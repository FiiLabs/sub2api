// APEXONE-EXT: 双边市场——观察期推进与下线排空的后台任务。
//
// 一个周期任务，做三件互不相干但都属于「供给账号的生命周期」的事：
//
//  1. 排空到期：draining 的号过了排空窗就转终态 retired。
//  2. 状态对齐：pending_review 但已经可调度的号，把 supply_state 补成 active。
//     这条规则是「管理员手工放行」这条路径的唯一实现——管理员在账号页把号设成
//     可调度，本任务下一轮就把接入状态对齐。自动入池关着时它也照常工作。
//  3. 观察期探测：按间隔对 pending_review 的号发一次真实的上游探测，记连续成功次数；
//     达标且观察窗跑满且总开关开着，才 promote 成 active。
//
// # 为什么是任务而不是挂在请求路径上
//
// 观察期的两个判据（时间跑满、连续探测成功）都不是任何用户请求的副产物：一个刚挂
// 上来的号在观察期里恰恰**没有流量**——它不可调度。没有后台任务，它就永远停在
// pending_review，除非它的主人正好去刷仪表盘。而排空到期更是纯粹的定时语义。
//
// # 探测花的是供给者的钱
//
// 每次探测是一个真实的推理请求，扣的是**供给者自己**的订阅额度。所以这里有三层节流：
// 单账号按 ProbeIntervalMinutes 间隔（下限 5 分钟，见 setting_supply_probation.go）、
// 单轮最多 supplierLifecycleMaxProbesPerRun 个账号、账号已经健康失败（Status 非 active）
// 时直接跳过——那种号探测必然失败，再戳只是白烧人家的额度。
package service

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// supplierLifecycleLeaderLockKey 多实例下只让一个实例推进生命周期。
	//
	// 这里的选主比解冻任务那边更要紧：解冻是幂等的搬钱，多跑几次只是浪费；
	// 而探测不是——N 个实例同时探同一个账号，供给者的额度就被烧 N 倍。
	supplierLifecycleLeaderLockKey = "supplier:lifecycle:leader"
	// supplierLifecycleLeaderLockTTL 必须大于单轮超时，否则锁会在跑到一半时过期，
	// 另一个实例接着进来重探一遍。
	supplierLifecycleLeaderLockTTL = 15 * time.Minute
	// supplierLifecycleRunTimeout 单轮总超时。探测是真实的上游请求，慢是常态。
	supplierLifecycleRunTimeout = 10 * time.Minute
	// supplierLifecycleProbeTimeout 单次探测超时。
	supplierLifecycleProbeTimeout = 90 * time.Second
	// SupplierLifecycleDefaultInterval 默认推进间隔。
	//
	// 观察窗以小时计、排空窗以分钟计，五分钟的粒度对两者都足够。取更小的值不会让
	// 观察期更快结束（单账号的探测间隔另有下限），只会让空转的那一轮更频繁。
	SupplierLifecycleDefaultInterval = 5 * time.Minute
	// supplierLifecycleScanLimit 单轮每个状态最多扫多少个账号。
	supplierLifecycleScanLimit = 200
	// supplierLifecycleMaxProbesPerRun 单轮最多真正发出多少次探测。
	//
	// 扫到的账号大多会被间隔节流掉，这个上限管的是「一批号同时到达探测时刻」的场景：
	// 那一轮不该变成一次对上游的突发。没轮到的下一轮再来，观察期本来就以小时计。
	supplierLifecycleMaxProbesPerRun = 20
)

// supplierLifecycleAccountStore 是生命周期任务用到的账号读写子集。
type supplierLifecycleAccountStore interface {
	GetByID(ctx context.Context, id int64) (*Account, error)
	UpdateExtra(ctx context.Context, id int64, updates map[string]any) error
	SetSchedulable(ctx context.Context, id int64, schedulable bool) error
	// SetError 把号打成错误态（status=error + 错误信息 + schedulable=false）。
	//
	// 只在探测拿到**认证类**失败时调用，见 probeOnce。加进这个窄接口之前值得知道：
	// 它是这份清单里唯一一个会让号「停下来」的方法，而它的对立面（ClearError）
	// 不在这里——把号放回去是供给者重新授权那条路径的事（supplier_reauth.go），
	// 不是后台任务能自作主张做的。
	SetError(ctx context.Context, id int64, errorMsg string) error
}

// supplierLifecycleStateLister 按接入状态列账号，外加一条按归属人可用性的扫描。
type supplierLifecycleStateLister interface {
	ListAccountIDsBySupplyState(ctx context.Context, state string, limit int) ([]int64, error)
	ListAccountIDsWithUnavailableOwner(ctx context.Context, limit int) ([]int64, error)
}

// supplierLifecycleProbationReader 读观察期参数。
type supplierLifecycleProbationReader interface {
	GetSupplyProbationSettings(ctx context.Context) *SupplyProbationSettings
}

// supplierLifecycleProber 发一次探测。
//
// 窄到只有一个方法，为的是不把 *AccountTestService（一个吃十几个依赖的大服务）
// 焊进本服务的构造签名里——测试要能用一个三行的假探针跑完整条观察期逻辑。
type supplierLifecycleProber interface {
	RunTestBackground(ctx context.Context, accountID int64, modelID string) (*ScheduledTestResult, error)
}

// supplierLifecycleIncidentSweeper 跑一轮失效事件的检测与通知。
//
// 窄到只有一个方法，理由与 supplierLifecycleProber 一样：本服务不该因为多做一件事
// 就长出一个新的存储依赖，而测试要能用一个计数器假件钉住「这一步被调了几次」。
type supplierLifecycleIncidentSweeper interface {
	Sweep(ctx context.Context)
}

// SupplierLifecycleService 周期推进供给账号的接入状态。
type SupplierLifecycleService struct {
	repo        supplierLifecycleStateLister
	accountRepo supplierLifecycleAccountStore
	settings    supplierLifecycleProbationReader
	prober      supplierLifecycleProber
	incidents   supplierLifecycleIncidentSweeper

	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string
}

// NewSupplierLifecycleService 构造生命周期任务。
//
// prober 允许为 nil：那样观察期只跑「时间到了没有」这一半，探测部分整体跳过。
// 这不是降级容错，是部署形态——没有配探测能力的部署，自动入池本来就不该开。
func NewSupplierLifecycleService(
	repo SupplierOnboardingRepository,
	accountRepo AccountRepository,
	settingService *SettingService,
	testService *AccountTestService,
	interval time.Duration,
) *SupplierLifecycleService {
	if interval <= 0 {
		interval = SupplierLifecycleDefaultInterval
	}
	svc := &SupplierLifecycleService{
		repo:        repo,
		accountRepo: accountRepo,
		settings:    settingService,
		interval:    interval,
		stopCh:      make(chan struct{}),
		instanceID:  uuid.NewString(),
	}
	// 显式判空再赋值：一个 nil 的 *AccountTestService 装进接口变量后不是 nil 接口，
	// 后面的 `s.prober == nil` 就永远为假，探测会打到一个空指针上。
	if testService != nil {
		svc.prober = testService
	}
	return svc
}

// SetIncidentSweeper 注入失效事件扫描。为 nil 时这一步整个跳过。
//
// 用 setter 而不是加进构造签名：它是可选的（没有配 SMTP、或者部署方不需要事件台账
// 时都可以不装），而构造函数的五个参数全部是这个任务跑起来的必要条件。
// 这个区分与 SetLeaderLock 是同一条——必需的进构造，可选的进 setter。
func (s *SupplierLifecycleService) SetIncidentSweeper(sweeper *SupplierIncidentService) {
	if s == nil || sweeper == nil {
		return
	}
	s.incidents = sweeper
}

// SetLeaderLock 注入选主用的缓存与数据库。两者都为 nil 时不选主直接跑（单实例与测试）。
func (s *SupplierLifecycleService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

func (s *SupplierLifecycleService) Start() {
	if s == nil || s.repo == nil || s.accountRepo == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		// 启动即跑一次：进程重启后，停机期间到期的排空窗要立刻补上——一个号停在
		// draining 里不接单也不下线，供给者看着它干等。
		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *SupplierLifecycleService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *SupplierLifecycleService) runOnce() {
	if s == nil || s.repo == nil || s.accountRepo == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("[SupplierLifecycle] run panic", "recover", r)
		}
	}()

	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(lockCtx, s.lockCache, s.db,
		supplierLifecycleLeaderLockKey, s.instanceID, supplierLifecycleLeaderLockTTL)
	lockCancel()
	if !ok {
		return
	}
	defer release()

	// 独立于任何请求 context：后台任务不该被别人的取消牵连。
	runCtx, cancel := context.WithTimeout(context.Background(), supplierLifecycleRunTimeout)
	defer cancel()

	// 顺序有意义：孤儿扫描排在最前。它是一道安全闸（把不该再供货的号停掉），
	// 而后两步是推进器（可能把号推进 active、开启调度）。反过来的话，同一轮里
	// 刚被停掉的号可能又被对齐规则推回池子——两条规则打架，且症状取决于扫描顺序。
	s.sweepUnavailableOwners(runCtx)
	s.sweepDraining(runCtx)
	s.sweepPendingReview(runCtx)
	s.sweepIncidents(runCtx)
}

// sweepIncidents 记下坏掉的号、关掉恢复了的、给新事件的主人发信。
//
// # 为什么排在最后
//
// 前三步都会改变账号的状态（停掉孤儿号、把排空到期的转终态、把观察期跑完的推进
// active），而本步读的正是那个状态。排在前面的话，它看到的是**上一轮**留下的世界：
// 一个刚刚被 sweepUnavailableOwners 停掉的号要多等五分钟才会被记下来，
// 而一个刚被 promote 成 active 的号会带着一条本该在这一轮就关掉的事件多活一轮。
//
// # 为什么搭这趟车而不是自起一个任务
//
// 这一步与前三步共享的东西比看上去多：同一把选主锁（多实例下重复扫描的代价在这里
// 是**重复发信**）、同一个 5 分钟节奏（事件的时效性要求与排空到期一个量级）、
// 同一个 run 超时。再起一个任务意味着再写一遍选主、再配一个间隔、再多一个
// 会在部署时被忘记打开的开关。
func (s *SupplierLifecycleService) sweepIncidents(ctx context.Context) {
	if s.incidents == nil {
		return
	}
	s.incidents.Sweep(ctx)
}

// ============================================================================
// 归属人失效
// ============================================================================

// sweepUnavailableOwners 把「主人已经不在了」的供给号停下来。
//
// # 为什么必须有这一步
//
// 用户注销走的是软删（`user_repo.deleteUser` 清掉认证身份后走 mixin 的软删），
// 所以 `accounts.owner_user_id` 上那条 `ON DELETE SET NULL` 一次也不会触发；
// 用户被停用更是完全不碰 accounts。两种情况下账号行**一个字节都没变**：
// 照常 schedulable、照常被调度、照常消耗那个人的上游订阅额度、照常给他的钱包
// 记账——而他已经登不进来，既看不到也停不掉。
//
// 这条闸只解决「继续供货」这一半。已经产生的入账**不撤**：那是他已经交付过的
// 服务，平台欠他的（§3.6 的名册也是按"注销用户仍可能是债主"设计的）。
//
// # 为什么是周期扫描而不是在封禁/注销那一刻同步做
//
// 同步做要侵入用户状态变更的每一条路径（管理员封禁、内容风控自动封禁、用户自助
// 注销），那是三处上游合并热区，且每加一条新路径都得记得补一次。周期扫描是**一处**，
// 并且对"漏改了某条路径"也免疫。代价是最长一个扫描周期（默认 5 分钟）的窗口——
// 相对"永远不停"，这个窗口是可接受的；相对三处 core 侵入，它更便宜。
//
// # 停下来之后
//
// 走的是与排空到期同一个 retire()：先停调度再写终态。用户如果只是被临时停用，
// 恢复后号停在 retired，需要他自己在供给页 Resume 一次——不自动放回去是有意的，
// 一个被封过的人的号重新进池应当重走观察期。
func (s *SupplierLifecycleService) sweepUnavailableOwners(ctx context.Context) {
	ids, err := s.repo.ListAccountIDsWithUnavailableOwner(ctx, supplierLifecycleScanLimit)
	if err != nil {
		slog.Error("[SupplierLifecycle] failed to list accounts with unavailable owner", "error", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	if len(ids) == supplierLifecycleScanLimit {
		slog.Warn("[SupplierLifecycle] orphan scan hit the batch limit, remainder deferred to next run",
			"limit", supplierLifecycleScanLimit)
	}

	stopped := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		account, err := s.accountRepo.GetByID(ctx, id)
		if err != nil || account == nil {
			continue
		}
		if err := s.retire(ctx, account); err != nil {
			slog.Warn("[SupplierLifecycle] failed to retire account with unavailable owner",
				"account_id", id, "error", err)
			continue
		}
		stopped++
		// 每个号单独一条 Info：这是一次「平台单方面把别人的号停了」的动作，
		// 事后有人问起时，聚合计数回答不了「到底停的是哪几个」。
		slog.Info("[SupplierLifecycle] retired supply account whose owner is no longer available",
			"account_id", id, "name", account.Name)
	}
	if stopped > 0 {
		slog.Warn("[SupplierLifecycle] supply accounts stopped because their owner was deleted or disabled",
			"count", stopped)
	}
}

// ============================================================================
// 排空到期
// ============================================================================

// sweepDraining 把排空窗跑完的号转终态。
//
// 到期判据只看 drain_until。没有这个时刻（配置被改过、或者这行 extra 被手工动过）
// 就当作立刻到期：一个进了 draining 却没有到期时刻的号，等下去也不会有人来救它，
// 停在中间态比直接下线更糟——供给者以为自己已经下线了，管理端看它既不接单也没退出。
func (s *SupplierLifecycleService) sweepDraining(ctx context.Context) {
	ids, err := s.repo.ListAccountIDsBySupplyState(ctx, SupplyStateDraining, supplierLifecycleScanLimit)
	if err != nil {
		slog.Error("[SupplierLifecycle] failed to list draining accounts", "error", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	if len(ids) == supplierLifecycleScanLimit {
		slog.Warn("[SupplierLifecycle] draining scan hit the batch limit, remainder deferred to next run",
			"limit", supplierLifecycleScanLimit)
	}

	now := time.Now()
	retired := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		account, err := s.accountRepo.GetByID(ctx, id)
		if err != nil || account == nil {
			continue
		}
		if until, ok := supplyExtraTime(account, SupplyDrainUntilExtraKey); ok && now.Before(until) {
			continue
		}
		if err := s.retire(ctx, account); err != nil {
			slog.Warn("[SupplierLifecycle] failed to retire drained account", "account_id", id, "error", err)
			continue
		}
		retired++
	}
	if retired > 0 {
		slog.Info("[SupplierLifecycle] retired drained supply accounts", "count", retired)
	}
}

// retire 把号推进终态。
//
// 先停调度再写状态：反过来的话，中间失败会留下一个 state=retired 却仍在接单的号——
// 供给者以为自己下线了，实际还在服务真实流量。这个顺序下的中间失败是
// 「已经停止接单，但状态还写着 draining」，下一轮扫描会重来一次。
func (s *SupplierLifecycleService) retire(ctx context.Context, account *Account) error {
	if err := s.accountRepo.SetSchedulable(ctx, account.ID, false); err != nil {
		return err
	}
	return s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
		SupplyStateExtraKey:       SupplyStateRetired,
		SupplyDrainUntilExtraKey:  "",
		SupplyDrainFromExtraKey:   "",
		SupplyProbePassesExtraKey: 0,
	})
}

// ============================================================================
// 观察期
// ============================================================================

func (s *SupplierLifecycleService) sweepPendingReview(ctx context.Context) {
	ids, err := s.repo.ListAccountIDsBySupplyState(ctx, SupplyStatePendingReview, supplierLifecycleScanLimit)
	if err != nil {
		slog.Error("[SupplierLifecycle] failed to list pending review accounts", "error", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	if len(ids) == supplierLifecycleScanLimit {
		slog.Warn("[SupplierLifecycle] probation scan hit the batch limit, remainder deferred to next run",
			"limit", supplierLifecycleScanLimit)
	}

	settings := s.probationSettings(ctx)
	probes := 0
	skippedForBudget := 0

	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		account, err := s.accountRepo.GetByID(ctx, id)
		if err != nil || account == nil {
			continue
		}

		// 对齐：管理员手工把号设成可调度，就是一次人工放行。这条规则不受
		// Enabled 影响——它对齐的是一个已经发生的事实，不是自动做决定。
		if account.Schedulable {
			if err := s.promote(ctx, account, false); err != nil {
				slog.Warn("[SupplierLifecycle] failed to reconcile schedulable account to active",
					"account_id", id, "error", err)
			}
			continue
		}

		if !s.shouldProbe(account, settings) {
			continue
		}
		if probes >= supplierLifecycleMaxProbesPerRun {
			skippedForBudget++
			continue
		}
		probes++
		s.probeOnce(ctx, account, settings)
	}

	if skippedForBudget > 0 {
		slog.Info("[SupplierLifecycle] probe budget exhausted this run, remainder deferred",
			"probed", probes, "deferred", skippedForBudget)
	}
}

func (s *SupplierLifecycleService) probationSettings(ctx context.Context) *SupplyProbationSettings {
	if s.settings == nil {
		return DefaultSupplyProbationSettings()
	}
	settings := s.settings.GetSupplyProbationSettings(ctx)
	if settings == nil {
		return DefaultSupplyProbationSettings()
	}
	return settings
}

// shouldProbe 决定这一轮要不要探这个号。
func (s *SupplierLifecycleService) shouldProbe(account *Account, settings *SupplyProbationSettings) bool {
	if s.prober == nil {
		return false
	}
	// 号本身已经是错误态（凭证失效、被上游封）——探测必然失败，再戳只是白烧额度，
	// 而且会把一个已经写明原因的 ErrorMessage 覆盖成一句更含糊的探测失败。
	// 供给者重新授权后 Status 会回到 active，探测自然恢复。
	if account.Status != "" && account.Status != StatusActive {
		return false
	}
	last, ok := supplyExtraTime(account, SupplyProbeAtExtraKey)
	if !ok {
		return true
	}
	return time.Since(last) >= settings.ProbeInterval()
}

// probeOnce 探一次并把结果写回 extra，达标时 promote。
func (s *SupplierLifecycleService) probeOnce(ctx context.Context, account *Account, settings *SupplyProbationSettings) {
	probeCtx, cancel := context.WithTimeout(ctx, supplierLifecycleProbeTimeout)
	result, err := s.prober.RunTestBackground(probeCtx, account.ID, supplyResolveProbeModel(settings))
	cancel()

	now := time.Now()
	updates := map[string]any{
		SupplyProbeAtExtraKey: now.Format(time.RFC3339),
	}

	// 观察期起点缺失时就地补一个。历史账号（本功能上线前接入的）没有这个字段，
	// 不补的话 EligibleAt 算不出来，前端只能显示一个空白的进度。
	// 补的是「现在」而不是 CreatedAt：观察的是它接下来这段时间稳不稳。
	if _, ok := supplyExtraTime(account, SupplyProbationSinceExtraKey); !ok {
		updates[SupplyProbationSinceExtraKey] = now.Format(time.RFC3339)
	}

	if err != nil || result == nil || result.Status != "success" {
		message := supplyProbeErrorMessage(err, result)
		updates[SupplyProbePassesExtraKey] = 0
		updates[SupplyProbeErrorExtraKey] = message
		if updErr := s.accountRepo.UpdateExtra(ctx, account.ID, updates); updErr != nil {
			slog.Warn("[SupplierLifecycle] failed to record probe failure", "account_id", account.ID, "error", updErr)
		}
		// 认证类失败（401）与其它探测失败是两件不同的事。其它失败可能自己好——
		// 限流、网络抖动、上游 5xx；而 401 只有账号的主人能修（重新授权一次）。
		//
		// 把它抬成 status=error，是为了让它落进**已经存在**的失效事件与通知里
		// （SupplierIncidentService 的开事件谓词认的就是 status，见
		// repository/supplier_incident_repo.go）。不抬的话，一个凭证过期的号会每
		// 15 分钟失败一次、永远失败下去，而它的主人一封信都收不到、仪表盘上
		// 只有一行红字——那封信的正文其实早就写好了，只是没有任何东西会触发它。
		//
		// 放在这里而不是 account_test_service.go 里 403 那一处：那个函数同时服务
		// 管理端手工「测试连接」按钮和全部自营号，在那里加 401 会让管理员的一次
		// 诊断点击，把一个正在刷新 token 的自营号打成错误态——401 比 403 常见得多，
		// 也远更可能是瞬态的。这里则确知自己面对的是一个供给号
		// （sweepPendingReview 只扫 owner_user_id IS NOT NULL 的行）、且是一次后台探测。
		//
		// 这一步与 supplier_reauth.go 是绑死的：SetError 会一并把 schedulable 置 false，
		// 而 ClearError 不会还回来。没有那条重新授权路径，这里就是一扇只出不进的门。
		if supplyProbeAuthFailure(message) {
			if setErr := s.accountRepo.SetError(ctx, account.ID, message); setErr != nil {
				slog.Warn("[SupplierLifecycle] failed to mark supply account as errored after auth failure",
					"account_id", account.ID, "error", setErr)
			}
		}
		return
	}

	passes := supplyExtraInt(account, SupplyProbePassesExtraKey) + 1
	updates[SupplyProbePassesExtraKey] = passes
	updates[SupplyProbeErrorExtraKey] = ""
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		slog.Warn("[SupplierLifecycle] failed to record probe success", "account_id", account.ID, "error", err)
		return
	}

	if !s.eligible(account, settings, passes, now) {
		return
	}
	if err := s.promote(ctx, account, true); err != nil {
		slog.Warn("[SupplierLifecycle] failed to promote supply account", "account_id", account.ID, "error", err)
	}
}

// eligible 判定一个号是否可以入池。委托给包级的 supplyProbationEligible。
//
// 保留这个方法而不是让调用点直接调那个函数：它是本服务里「该不该 promote」
// 的唯一提问处，留一层薄壳能让这条链在调用图上仍然读得出来。
func (s *SupplierLifecycleService) eligible(account *Account, settings *SupplyProbationSettings, passes int, now time.Time) bool {
	return supplyProbationEligible(account, settings, passes, now)
}

// supplyProbationEligible 判定一个号是否可以入池。
//
// 三个条件是**并且**：总开关开着、连续成功次数达标、观察窗跑满。
// 观察起点缺失时按「刚补上」算（probeOnce 刚写了 now），也就是从这一刻重新计时——
// 一个没有起点的号不该因为查不到时间就被判为已经观察够久了。
//
// 做成包级函数是因为它有**两个**调用方：观察期任务（本文件）与自助接入时的
// 同步探测（supplier_onboarding_service.go 的 probeOnAttach）。两边各写一份判据，
// 漂移的那天症状是「接入时说不够格、十五分钟后又够格了」，而两处代码单独看都对。
func supplyProbationEligible(account *Account, settings *SupplyProbationSettings, passes int, now time.Time) bool {
	if settings == nil || !settings.Enabled {
		return false
	}
	if passes < settings.RequiredSuccesses {
		return false
	}
	since, ok := supplyExtraTime(account, SupplyProbationSinceExtraKey)
	if !ok {
		return false
	}
	return !now.Before(since.Add(settings.ObservationWindow()))
}

// promote 把号推进 active。委托给包级的 supplyPromoteToActive，理由同 eligible。
func (s *SupplierLifecycleService) promote(ctx context.Context, account *Account, setSchedulable bool) error {
	return supplyPromoteToActive(ctx, s.accountRepo, account, setSchedulable)
}

// supplyPromoteStore 入池要写的两样东西。
//
// 单独一个窄接口，是为了让 supplyPromoteToActive 同时被观察期任务的
// supplierLifecycleAccountStore 和自助接入的 supplierAccountStore 满足——
// 两者都有这两个方法，但它们是两个不同的接口，没有这层抽象就只能各抄一遍入池动作。
type supplyPromoteStore interface {
	SetSchedulable(ctx context.Context, id int64, schedulable bool) error
	UpdateExtra(ctx context.Context, id int64, updates map[string]any) error
}

// supplyPromoteToActive 把号推进 active。
//
// **先开调度再写状态**：反过来的话，中间失败会留下一个 state=active 却不可调度的号——
// 一个「看起来入池了但永远拿不到流量」的静默故障，供给者看不出问题在哪。这个顺序下
// 的中间失败是「已经在接单，状态还写着 pending_review」，下一轮的对齐规则会补上。
//
// setSchedulable=false 用于对齐路径：那时号已经是可调度的，不必再写一次。
func supplyPromoteToActive(ctx context.Context, store supplyPromoteStore, account *Account, setSchedulable bool) error {
	if setSchedulable {
		if err := store.SetSchedulable(ctx, account.ID, true); err != nil {
			return err
		}
		slog.Info("[SupplierLifecycle] supply account promoted to active",
			"account_id", account.ID, "name", account.Name)
	}
	return store.UpdateExtra(ctx, account.ID, map[string]any{
		SupplyStateExtraKey:      SupplyStateActive,
		SupplyProbeErrorExtraKey: "",
	})
}

// supplyProbeErrorMessage 把探测失败压成一句给供给者看的话。
//
// 截断是必要的：上游的错误体可能是一整段 JSON，原样存进 extra 会让账号行膨胀，
// 而供给者需要的只是「是不是我的号出问题了」这个判断。
func supplyProbeErrorMessage(err error, result *ScheduledTestResult) string {
	message := ""
	switch {
	case err != nil:
		message = err.Error()
	case result != nil && result.ErrorMessage != "":
		message = result.ErrorMessage
	default:
		message = "probe failed"
	}
	message = strings.TrimSpace(message)
	if len(message) > supplyProbeErrorMaxLen {
		message = message[:supplyProbeErrorMaxLen]
	}
	return message
}

// supplyProbeErrorMaxLen 探测失败原因的存储上限。
const supplyProbeErrorMaxLen = 300

// supplyProbeAuthFailure 判断一句探测失败是不是「上游不认这份凭证了」。
//
// 判据是字符串，因为探测结果（ScheduledTestResult）只带回一句话、不带状态码。
// 它匹配的是 account_test_service.go 里那句
//
//	errMsg := fmt.Sprintf("API returned %d: %s", resp.StatusCode, string(body))
//
// 的产物——改那句话会让这里**静默**失效（不会编译错，只会让徽章、事件和邮件
// 一起消失），所以有一条单测把这个前缀原样钉住。
//
// 只匹配这一个精确前缀，不放宽成「含 401」：上游的错误体本身可能提到 401
// （比如一段解释重试策略的文档链接），那会把一次限流误判成凭证失效，
// 进而把一个健康的号打成错误态。
//
// supplyProbeErrorMessage 会把消息截到 300 字符，但这个前缀在最开头，
// 截断不影响匹配。
func supplyProbeAuthFailure(message string) bool {
	return strings.Contains(message, "API returned 401")
}

// supplyProbeDefaultModel 是供给号探测的默认模型。ops 没在 supply-probation
// 里显式设 probe_model 时用它。
//
// 刻意用 Fable 而不是全局的 DefaultTestModel（sonnet）：探测的目的不是「这个号
// 能不能回一个响应」，是「这个号能不能服务我们实际卖的东西」。免费号或没订阅的号
// 打 Fable 会被上游以 429 + credits_required 拒掉（见 supplyProbeNoQuota），而
// 打 sonnet 可能照样成功——那样一个接不了单的号会通过探测、白占观察期。
//
// 不改 DefaultTestModel：那个常量同时服务管理端「测试连接」按钮和全部自营号，
// 在那里换成 Fable 会波及一大片与供给无关的路径。
const supplyProbeDefaultModel = "claude-fable-5-1"

// supplyProbeNoQuota 判定探测失败是不是「这个订阅没有 Fable 额度」。
//
// 判据是两段上游原文，取自生产 ops_error_logs 里免费/无额度号打 Fable 的真实返回：
//   1. body 里的 error_code":"credits_required"（out_of_credits / org_level_disabled 都带它）
//   2. 明文 "Usage credits are required for this model"
//
// 只认这两个精确信号，**不**把笼统的 429 当无额度——普通限流（"rate limit"）也是
// 429，但它是瞬态、会自己好，把一个只是这一刻超速的付费号误判成「免费号」拒之门外
// 比放进一个免费号严重得多。supplyProbeErrorMessage 截断 300 字符，而这两个信号都
// 出现在上游 body 的前 ~120 字符，截断安全。
func supplyProbeNoQuota(message string) bool {
	return strings.Contains(message, "credits_required") ||
		strings.Contains(message, "Usage credits are required")
}

// supplyResolveProbeModel 决定这次探测用哪个模型：ops 显式配了就用配的，否则 Fable。
func supplyResolveProbeModel(settings *SupplyProbationSettings) string {
	if settings != nil && strings.TrimSpace(settings.ProbeModel) != "" {
		return settings.ProbeModel
	}
	return supplyProbeDefaultModel
}
