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
}

// supplierLifecycleStateLister 按接入状态列账号。
type supplierLifecycleStateLister interface {
	ListAccountIDsBySupplyState(ctx context.Context, state string, limit int) ([]int64, error)
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

// SupplierLifecycleService 周期推进供给账号的接入状态。
type SupplierLifecycleService struct {
	repo        supplierLifecycleStateLister
	accountRepo supplierLifecycleAccountStore
	settings    supplierLifecycleProbationReader
	prober      supplierLifecycleProber

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

	s.sweepDraining(runCtx)
	s.sweepPendingReview(runCtx)
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
	result, err := s.prober.RunTestBackground(probeCtx, account.ID, settings.ProbeModel)
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

// eligible 判定一个号是否可以入池。
//
// 三个条件是**并且**：总开关开着、连续成功次数达标、观察窗跑满。
// 观察起点缺失时按「刚补上」算（probeOnce 刚写了 now），也就是从这一刻重新计时——
// 一个没有起点的号不该因为查不到时间就被判为已经观察够久了。
func (s *SupplierLifecycleService) eligible(account *Account, settings *SupplyProbationSettings, passes int, now time.Time) bool {
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

// promote 把号推进 active。
//
// 先开调度再写状态：反过来的话，中间失败会留下一个 state=active 却不可调度的号——
// 一个「看起来入池了但永远拿不到流量」的静默故障，供给者看不出问题在哪。这个顺序下
// 的中间失败是「已经在接单，状态还写着 pending_review」，下一轮的对齐规则会补上。
//
// setSchedulable=false 用于对齐路径：那时号已经是可调度的，不必再写一次。
func (s *SupplierLifecycleService) promote(ctx context.Context, account *Account, setSchedulable bool) error {
	if setSchedulable {
		if err := s.accountRepo.SetSchedulable(ctx, account.ID, true); err != nil {
			return err
		}
		slog.Info("[SupplierLifecycle] supply account promoted to active",
			"account_id", account.ID, "name", account.Name)
	}
	return s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
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
