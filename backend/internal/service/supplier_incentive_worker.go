// APEXONE-EXT: 双边市场——挂号奖励的发放 worker。
//
// 一轮做三件事：给在役号的在线天数 +1 → 读规则算各档剩余名额 → 给够格且没拿过的
// 账号入账。天数先涨、奖励后发，所以刚满门槛的号**当轮就能拿到**，不用等下一轮。
//
// # 为什么是 1 小时 ticker 而不是「每天零点跑一次」
//
// 「每日发放」这件事靠的不是定时准点，是**日期键幂等**（见 repo 里那条 UPDATE 的
// WHERE 条件）。一小时跑一轮，一天里只有第一轮真正累加天数，其余 23 轮受影响行数为 0。
// 换来的是三个问题一起消失：进程重启不会漂移、重启在跨日点附近不会漏掉一天、
// 不必决定「几点跑」和用谁的时区。这个仓库里所有周期任务都是 ticker + 选主，
// 没有 cron 框架，这里也不为它引入一个。
//
// # 失败一律只记不重试
//
// 与 SupplierThawService 同一个脾气，而且理由更硬：两层幂等（天数的日期键、
// 入账的 (action, request_id) 唯一索引）让重跑永远安全，所以「等下一轮」总是
// 对的答案。worker 里绝不出现补偿逻辑——补偿逻辑本身才是重复发钱的常见来源。
//
// # 名额是唯一的成本闸门
//
// 门槛是「在线天数」而不是「累计产出」，意味着奖励不再被产出自付（详见
// setting_supply_incentive.go 文件头的取舍）。所以每一档的 Slots 都必须当真：
// 这里在入账前查一次已发数、按剩余名额裁剪候选列表。多实例下靠选主锁保证只有
// 一个实例在这段逻辑里，锁 TTL（5 分钟）刻意大于单轮超时（2 分钟）。
package service

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// supplierIncentiveLeaderLockKey 多实例下只让一个实例发奖。
	//
	// 与解冻那把锁不同，这里加锁不只是「省一次重复工作」：名额检查是
	// 查已发数 → 发放 两步，两个实例并发跑会各自读到同一个已发数，
	// 结果是名额被突破（不会重复发给同一个账号——那由唯一索引挡住——
	// 但会多发给几个不同的账号）。
	supplierIncentiveLeaderLockKey = "supplier:incentive:leader"
	// supplierIncentiveLeaderLockTTL 必须大于单轮超时，否则锁会在跑到一半时过期。
	supplierIncentiveLeaderLockTTL = 5 * time.Minute
	// supplierIncentiveRunTimeout 单轮超时。
	supplierIncentiveRunTimeout = 2 * time.Minute
	// SupplierIncentiveDefaultInterval 默认轮询间隔。
	//
	// 取 1 小时而不是 24 小时的理由见文件头。取 1 小时而不是 10 分钟，是因为
	// 这个任务在没有活动时也会扫一次账号表，而奖励晚几十分钟到账毫无影响。
	SupplierIncentiveDefaultInterval = time.Hour
	// supplierIncentiveDayFormat 在线天数的日期键格式（UTC）。
	//
	// 固定 UTC 而不是本地时区：时区一旦跟着部署环境走，换一次机器就可能出现
	// 某一天加两次或漏一天，而这两种错都不会报错，只会让天数悄悄不对。
	supplierIncentiveDayFormat = "2006-01-02"
)

// SupplierIncentiveWorker 周期性累加在线天数并发放挂号奖励。
type SupplierIncentiveWorker struct {
	repo           SupplierIncentiveRepository
	credit         SupplierCreditRepository
	settingService *SettingService

	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string
}

// NewSupplierIncentiveWorker 构造发放 worker。
func NewSupplierIncentiveWorker(
	repo SupplierIncentiveRepository,
	credit SupplierCreditRepository,
	settingService *SettingService,
	interval time.Duration,
) *SupplierIncentiveWorker {
	if interval <= 0 {
		interval = SupplierIncentiveDefaultInterval
	}
	return &SupplierIncentiveWorker{
		repo:           repo,
		credit:         credit,
		settingService: settingService,
		interval:       interval,
		stopCh:         make(chan struct{}),
		instanceID:     uuid.NewString(),
	}
}

// SetLeaderLock 注入选主用的缓存与数据库。两者都为 nil 时不选主直接跑
// （单实例部署与测试的行为）。
func (w *SupplierIncentiveWorker) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if w == nil {
		return
	}
	w.lockCache = lockCache
	w.db = db
}

func (w *SupplierIncentiveWorker) Start() {
	if w == nil || w.repo == nil || w.credit == nil || w.interval <= 0 {
		return
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		// 启动即跑一次：进程重启后不必等满一个 interval 才补上停机期间的天数。
		// 当天已经加过就是零行受影响，重启多少次都不会多加。
		w.runOnce()
		for {
			select {
			case <-ticker.C:
				w.runOnce()
			case <-w.stopCh:
				return
			}
		}
	}()
}

func (w *SupplierIncentiveWorker) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		close(w.stopCh)
	})
	w.wg.Wait()
}

func (w *SupplierIncentiveWorker) runOnce() {
	if w == nil || w.repo == nil || w.credit == nil {
		return
	}

	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(lockCtx, w.lockCache, w.db,
		supplierIncentiveLeaderLockKey, w.instanceID, supplierIncentiveLeaderLockTTL)
	lockCancel()
	if !ok {
		return
	}
	defer release()

	// 独立于任何请求 context：这是后台任务，不该被别人的取消牵连。
	runCtx, cancel := context.WithTimeout(context.Background(), supplierIncentiveRunTimeout)
	defer cancel()

	w.RunOnce(runCtx)
}

// RunOnce 处理一轮。导出给测试直接调（绕过 ticker 与选主）。
func (w *SupplierIncentiveWorker) RunOnce(ctx context.Context) {
	if w == nil || w.repo == nil || w.credit == nil {
		return
	}

	// 天数**无条件**累加，与活动开没开无关：它是账号的客观属性。活动一开一关就让
	// 所有人的天数从头再来，那奖励门槛就变成了「运营手抖的次数」。
	day := time.Now().UTC().Format(supplierIncentiveDayFormat)
	if ticked, err := w.repo.TickActiveDays(ctx, day); err != nil {
		slog.Error("[SupplierIncentive] failed to tick supply active days", "error", err, "day", day)
		// 不 return：这一轮的天数没加上，下一轮会补；但已经够格的账号不该被连累。
	} else if ticked > 0 {
		slog.Info("[SupplierIncentive] ticked supply active days", "accounts", ticked, "day", day)
	}

	settings := w.settings(ctx)
	if !settings.Active() {
		return
	}
	// 分成关掉时不发奖：钱包的可见性与提现都挂在结算总开关上，往一个用户看不见、
	// 也提不出来的钱包里打钱，比不打更糟——他不会知道自己拿到了什么。
	settlement := w.settlement(ctx)
	if settlement == nil || !settlement.Enabled {
		slog.Warn("[SupplierIncentive] settlement disabled, skipping reward grants")
		return
	}

	perUserCap := w.perUserGrantCap(ctx)
	for _, program := range settings.Programs {
		for i, tier := range program.Tiers {
			if ctx.Err() != nil {
				return
			}
			w.grantTier(ctx, program, i, tier, settlement.FreezeHours, perUserCap)
		}
	}
}

// perUserGrantCap 同一个人在同一档最多能拿几份。
//
// # 为什么需要这个上限
//
// 名额按**账号**计（一个人挂两个真号就该拿两份），但「一个账号」并不等于
// 「一份订阅」：上游订阅查重（rejectDuplicateSubscription）只看 deleted_at IS NULL
// 的行，而解绑会软删该行、并把 credentials 抹成 {}。于是同一份订阅解绑之后可以
// 再挂一次，拿到一个**新的 account id**，也就是一个全新的幂等键。
//
// 单看收益，这样刷并不划算——尾重的档位设计让老实挂满 90 天拿 $100，而反复刷
// 10 天档 90 天只能拿 $45。但它会把名额从真实用户手里挤走，所以还是要挡。
//
// # 为什么复用接入的每人号数上限
//
// 「你允许一个人挂几个号，他最多就拿几份」——这条对应关系不需要第八个配置字段，
// 而且两个数天然同向：运营调紧接入上限时，一定也想调紧奖励份数。
//
// 上限配成 0（不限）时取 1，而不是跟着不限。理由是方向：0 表达的是「挂号数量
// 不设限」这条**供给侧**策略，它不可能是一个有意为之的**奖励**策略——没有人会
// 刻意配置「一个人可以无限次领同一档」。读不出明确意图时取最严的那个，
// 与这个子系统其余各处的 fail-closed 同向。
func (w *SupplierIncentiveWorker) perUserGrantCap(ctx context.Context) int {
	if w.settingService == nil {
		return 1
	}
	onboarding := w.settingService.GetSupplyOnboardingSettings(ctx)
	if onboarding == nil || onboarding.MaxAccountsPerUser <= 0 {
		return 1
	}
	return onboarding.MaxAccountsPerUser
}

// grantTier 发放一个档位。
func (w *SupplierIncentiveWorker) grantTier(
	ctx context.Context,
	program SupplyIncentiveProgram,
	tierIndex int,
	tier SupplyIncentiveTier,
	freezeHours int,
	perUserCap int,
) {
	prefix := SupplyIncentiveRequestPrefix(program.Slug, tierIndex)

	// 已发明细这一查无论名额限不限都要做：Total 用来算剩余名额（只在限量时有用），
	// ByUser 用来挡解绑重挂（任何时候都要挡）。
	stats, err := w.repo.GrantStats(ctx, prefix)
	if err != nil || stats == nil {
		// 数不出已发数就**不发**：名额是这个设计里唯一的成本闸门，
		// 读失败时放行等于把闸门打开。下一轮会重来。
		slog.Error("[SupplierIncentive] failed to read grant stats, skipping tier",
			"error", err, "slug", program.Slug, "tier", tierIndex)
		return
	}

	limit := 0 // 0 = 不限，交给 repo 用它自己的单轮上限兜住
	if tier.Slots > 0 {
		remaining := tier.Slots - stats.Total
		if remaining <= 0 {
			return
		}
		limit = remaining
	}

	var excludedUsers []int64
	for userID, n := range stats.ByUser {
		if n >= perUserCap {
			excludedUsers = append(excludedUsers, userID)
		}
	}

	candidates, err := w.repo.ListCandidates(ctx, SupplyIncentiveCandidateQuery{
		MinActiveDays:   tier.MinActiveDays,
		Platform:        program.Platform,
		RequestPrefix:   prefix,
		ExcludedUserIDs: excludedUsers,
		Limit:           limit,
	})
	if err != nil {
		slog.Error("[SupplierIncentive] failed to list candidates",
			"error", err, "slug", program.Slug, "tier", tierIndex)
		return
	}
	if len(candidates) == 0 {
		return
	}

	// 同一轮之内也要数：候选查询排除的是**查询那一刻**已经拿满的人，而一个人
	// 名下两个够格的号会在同一批候选里一起出现。不在这里累计的话，上限为 1 时
	// 他会在一轮里拿到两份。
	grantedThisRound := make(map[int64]int, len(candidates))

	paid, total := 0, 0.0
	for _, c := range candidates {
		if ctx.Err() != nil {
			break
		}
		if stats.ByUser[c.OwnerUserID]+grantedThisRound[c.OwnerUserID] >= perUserCap {
			continue
		}
		accountID := c.AccountID
		applied, err := w.credit.Accrue(ctx, SupplierAccrueParams{
			SupplierUserID: c.OwnerUserID,
			RequestID:      SupplyIncentiveRequestID(program.Slug, tierIndex, accountID),
			AccountID:      &accountID,
			// 无消费方：活动奖励不是谁的用量换来的。留 nil 还有一层作用——
			// 消费者退款的 clawback 只沿 source_user_id 追回，碰不到这些行。
			ConsumerUserID: nil,
			BasisAmount:    tier.AmountUSD,
			// 恒为 1.0：让流水里「基数 × 比例 = 金额」三要素自洽，共享者拿快照
			// 就能自行核对，与分成入账是同一套读法。
			ShareRatio:  1.0,
			FreezeHours: freezeHours,
		})
		if err != nil {
			// 只记不重试：下一轮 ticker 自然会重来，幂等键保证不会重发。
			slog.Error("[SupplierIncentive] failed to grant reward",
				"error", err, "slug", program.Slug, "tier", tierIndex,
				"account_id", accountID, "user_id", c.OwnerUserID)
			continue
		}
		if !applied {
			// 幂等命中：上一轮已经发过（候选查询与入账之间有窗口）。不是错误。
			continue
		}
		grantedThisRound[c.OwnerUserID]++
		paid++
		total += tier.AmountUSD
	}

	if paid > 0 {
		slog.Info("[SupplierIncentive] granted rewards",
			"slug", program.Slug, "tier", tierIndex, "min_active_days", tier.MinActiveDays,
			"accounts", paid, "amount", total)
	}
}

func (w *SupplierIncentiveWorker) settings(ctx context.Context) *SupplyIncentiveSettings {
	if w.settingService == nil {
		return DefaultSupplyIncentiveSettings()
	}
	return w.settingService.GetSupplyIncentiveSettings(ctx)
}

func (w *SupplierIncentiveWorker) settlement(ctx context.Context) *SupplierSettlementSettings {
	if w.settingService == nil {
		return nil
	}
	return w.settingService.GetSupplierSettlementSettings(ctx)
}
