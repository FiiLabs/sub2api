//go:build unit

// APEXONE-EXT: 双边市场——观察期推进与排空到期的单元测试。
//
// 这条流水线动的是两件不可逆的事：把一个陌生人的号推到付费流量前面（promote），
// 以及花供给者自己的额度做探测。所以测试的重心不是「功能能跑通」，而是
// **什么情况下它必须什么都不做**。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supplyProbationReaderStub 直接给一份配置，绕开 settings 缓存。
type supplyProbationReaderStub struct {
	settings *SupplyProbationSettings
	calls    int
}

func (s *supplyProbationReaderStub) GetSupplyProbationSettings(context.Context) *SupplyProbationSettings {
	s.calls++
	return s.settings
}

// supplierProberStub 记下每次探测的对象，并按预设结果作答。
type supplierProberStub struct {
	probed  []int64
	models  []string
	results map[int64]*ScheduledTestResult
	errs    map[int64]error
}

func newSupplierProberStub() *supplierProberStub {
	return &supplierProberStub{
		results: map[int64]*ScheduledTestResult{},
		errs:    map[int64]error{},
	}
}

func (p *supplierProberStub) RunTestBackground(_ context.Context, accountID int64, modelID string) (*ScheduledTestResult, error) {
	p.probed = append(p.probed, accountID)
	p.models = append(p.models, modelID)
	if err, ok := p.errs[accountID]; ok {
		return nil, err
	}
	if result, ok := p.results[accountID]; ok {
		return result, nil
	}
	return &ScheduledTestResult{Status: "success"}, nil
}

func newLifecycleService(
	repo *supplierOnboardingRepoStub,
	store *supplierAccountStoreStub,
	settings *SupplyProbationSettings,
	prober *supplierProberStub,
) *SupplierLifecycleService {
	svc := &SupplierLifecycleService{
		repo:        repo,
		accountRepo: store,
		settings:    &supplyProbationReaderStub{settings: settings},
		interval:    time.Minute,
		stopCh:      make(chan struct{}),
	}
	// nil 的 *supplierProberStub 装进接口不是 nil 接口——显式判空，否则
	// 「没有探测能力」的用例反而会走进探测分支。
	if prober != nil {
		svc.prober = prober
	}
	return svc
}

func autoPromoteSettings() *SupplyProbationSettings {
	return &SupplyProbationSettings{
		Enabled:               true,
		MinObservationMinutes: 60,
		RequiredSuccesses:     2,
		ProbeIntervalMinutes:  15,
		DrainWindowMinutes:    10,
	}
}

// ============================================================================
// 排空到期
// ============================================================================

func TestSweepDrainingRetiresExpiredAccounts(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Schedulable: false, Extra: map[string]any{
		SupplyStateExtraKey:      SupplyStateDraining,
		SupplyDrainUntilExtraKey: time.Now().Add(-time.Minute).Format(time.RFC3339),
		SupplyDrainFromExtraKey:  SupplyStateActive,
	}}
	repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
		SupplyStateDraining: {100},
	}}
	svc := newLifecycleService(repo, store, autoPromoteSettings(), nil)

	svc.sweepDraining(context.Background())

	updates := store.extraUpdates[100]
	assert.Equal(t, SupplyStateRetired, updates[SupplyStateExtraKey])
	assert.Empty(t, updates[SupplyDrainUntilExtraKey], "终态不该留着排空窗")
	assert.Empty(t, updates[SupplyDrainFromExtraKey])
	assert.False(t, store.schedulableSets[100])
}

// 窗口没到就一个字段都不动——这段时间正是供给者的反悔窗口。
func TestSweepDrainingLeavesUnexpiredAccountsAlone(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Extra: map[string]any{
		SupplyStateExtraKey:      SupplyStateDraining,
		SupplyDrainUntilExtraKey: time.Now().Add(time.Hour).Format(time.RFC3339),
	}}
	repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
		SupplyStateDraining: {100},
	}}
	svc := newLifecycleService(repo, store, autoPromoteSettings(), nil)

	svc.sweepDraining(context.Background())

	assert.Nil(t, store.extraUpdates[100])
	assert.Zero(t, store.schedulableCalls)
}

// 没有到期时刻（字段坏了/被手工删了）当作立刻到期：停在中间态比直接下线更糟，
// 供给者以为自己已经下线了，实际它既不接单也没退出。
func TestSweepDrainingRetiresAccountsWithMissingDeadline(t *testing.T) {
	for _, extra := range []map[string]any{
		{SupplyStateExtraKey: SupplyStateDraining},
		{SupplyStateExtraKey: SupplyStateDraining, SupplyDrainUntilExtraKey: ""},
		{SupplyStateExtraKey: SupplyStateDraining, SupplyDrainUntilExtraKey: "not-a-time"},
		{SupplyStateExtraKey: SupplyStateDraining, SupplyDrainUntilExtraKey: 12345},
	} {
		store := newSupplierAccountStoreStub()
		store.accounts[100] = &Account{ID: 100, Extra: extra}
		repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
			SupplyStateDraining: {100},
		}}
		svc := newLifecycleService(repo, store, autoPromoteSettings(), nil)

		svc.sweepDraining(context.Background())
		assert.Equal(t, SupplyStateRetired, store.extraUpdates[100][SupplyStateExtraKey])
	}
}

func TestSweepDrainingSurvivesListError(t *testing.T) {
	store := newSupplierAccountStoreStub()
	repo := &supplierOnboardingRepoStub{stateErr: errors.New("db down")}
	svc := newLifecycleService(repo, store, autoPromoteSettings(), nil)

	assert.NotPanics(t, func() { svc.sweepDraining(context.Background()) })
	assert.Empty(t, store.calls)
}

// ============================================================================
// 归属人失效
// ============================================================================
//
// 「谁进这个名单」由 SQL 决定（repository 层，对着真库测），这里测的是拿到名单之后
// 的动作：必须**停调度**，而且必须停在推进器之前跑。

func TestSweepUnavailableOwnersRetiresAccount(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Name: "orphan", Schedulable: true, Extra: map[string]any{
		SupplyStateExtraKey: SupplyStateActive,
	}}
	repo := &supplierOnboardingRepoStub{orphanIDs: []int64{100}}
	svc := newLifecycleService(repo, store, autoPromoteSettings(), nil)

	svc.sweepUnavailableOwners(context.Background())

	// 停调度是这条闸的**全部意义**：状态写成什么样都只是给人看的，
	// 真正让那个人的订阅不再被消耗的只有这一个布尔量。
	require.Contains(t, store.schedulableSets, int64(100))
	assert.False(t, store.schedulableSets[100])
	assert.Equal(t, SupplyStateRetired, store.extraUpdates[100][SupplyStateExtraKey])
}

// 已经产生的入账不在这条路径上——它是欠这个人的债，不是要清理的垃圾。
// 断言方式：整条 sweep 不碰任何钱包接口（本服务根本没有钱包依赖，
// 所以这里真正钉住的是「别有人日后往里加一个」）。
func TestSweepUnavailableOwnersSurvivesListError(t *testing.T) {
	store := newSupplierAccountStoreStub()
	repo := &supplierOnboardingRepoStub{orphanErr: errors.New("db down")}
	svc := newLifecycleService(repo, store, autoPromoteSettings(), nil)

	assert.NotPanics(t, func() { svc.sweepUnavailableOwners(context.Background()) })
	assert.Empty(t, store.calls, "读不到名单时一个号都不该被动")
}

// 顺序性质：孤儿扫描必须排在推进器之前。
//
// 反过来的话，同一轮里 sweepPendingReview 的「对齐」规则会把一个刚被停掉的号
// 当成"管理员手工放行"（它此刻 schedulable=false，不会被对齐）——真正的危险是
// 另一半：一个 owner 已失效但仍 schedulable 的号，会先被对齐规则推成 active，
// 再被孤儿扫描停掉，白白多一次状态翻转，且中间那一瞬它是"正式入池"的。
func TestRunOnceSweepsUnavailableOwnersFirst(t *testing.T) {
	store := newSupplierAccountStoreStub()
	repo := &supplierOnboardingRepoStub{}
	svc := newLifecycleService(repo, store, autoPromoteSettings(), nil)

	svc.runOnce()

	require.GreaterOrEqual(t, len(repo.calls), 3)
	assert.Equal(t, "ListAccountIDsWithUnavailableOwner", repo.calls[0])
}

// ============================================================================
// 状态对齐（管理员手工放行）
// ============================================================================

// 管理员在账号页把号设成可调度 = 一次人工放行。这条规则不受 Enabled 影响：
// 它对齐的是一个已经发生的事实，不是替谁做决定。
func TestSweepPendingReviewReconcilesSchedulableAccountEvenWhenAutoPromotionOff(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Schedulable: true, Status: StatusActive, Extra: map[string]any{
		SupplyStateExtraKey: SupplyStatePendingReview,
	}}
	repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
		SupplyStatePendingReview: {100},
	}}
	prober := newSupplierProberStub()
	svc := newLifecycleService(repo, store, DefaultSupplyProbationSettings(), prober)

	svc.sweepPendingReview(context.Background())

	assert.Equal(t, SupplyStateActive, store.extraUpdates[100][SupplyStateExtraKey])
	assert.Empty(t, prober.probed, "已经在接单的号不用再探测")
	assert.Zero(t, store.schedulableCalls, "它本来就是可调度的，不必再写一次")
}

// ============================================================================
// 探测节流（花的是供给者的额度）
// ============================================================================

func TestProbeSkippedWithinInterval(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Status: StatusActive, Extra: map[string]any{
		SupplyStateExtraKey:   SupplyStatePendingReview,
		SupplyProbeAtExtraKey: time.Now().Add(-time.Minute).Format(time.RFC3339),
	}}
	repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
		SupplyStatePendingReview: {100},
	}}
	prober := newSupplierProberStub()
	svc := newLifecycleService(repo, store, autoPromoteSettings(), prober)

	svc.sweepPendingReview(context.Background())
	assert.Empty(t, prober.probed, "间隔没到就不许再花人家的额度")
}

// 号已经是错误态时探测必然失败，再戳只是白烧额度，还会把一条写明原因的
// ErrorMessage 盖成一句更含糊的探测失败。
func TestProbeSkippedForUnhealthyAccount(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Status: StatusError, Extra: map[string]any{
		SupplyStateExtraKey: SupplyStatePendingReview,
	}}
	repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
		SupplyStatePendingReview: {100},
	}}
	prober := newSupplierProberStub()
	svc := newLifecycleService(repo, store, autoPromoteSettings(), prober)

	svc.sweepPendingReview(context.Background())
	assert.Empty(t, prober.probed)
}

func TestProbeSkippedWhenNoProberConfigured(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Status: StatusActive, Extra: map[string]any{
		SupplyStateExtraKey: SupplyStatePendingReview,
	}}
	repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
		SupplyStatePendingReview: {100},
	}}
	svc := newLifecycleService(repo, store, autoPromoteSettings(), nil)

	assert.NotPanics(t, func() { svc.sweepPendingReview(context.Background()) })
	assert.Nil(t, store.extraUpdates[100])
}

// 一批号同时到达探测时刻时，这一轮不该变成一次对上游的突发。
func TestProbeBudgetCapsProbesPerRun(t *testing.T) {
	store := newSupplierAccountStoreStub()
	ids := make([]int64, 0, supplierLifecycleMaxProbesPerRun+5)
	for i := int64(1); i <= int64(supplierLifecycleMaxProbesPerRun)+5; i++ {
		store.accounts[i] = &Account{ID: i, Status: StatusActive, Extra: map[string]any{
			SupplyStateExtraKey: SupplyStatePendingReview,
		}}
		ids = append(ids, i)
	}
	repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
		SupplyStatePendingReview: ids,
	}}
	prober := newSupplierProberStub()
	svc := newLifecycleService(repo, store, autoPromoteSettings(), prober)

	svc.sweepPendingReview(context.Background())
	assert.Len(t, prober.probed, supplierLifecycleMaxProbesPerRun)
}

func TestProbeUsesConfiguredModel(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Status: StatusActive, Extra: map[string]any{
		SupplyStateExtraKey: SupplyStatePendingReview,
	}}
	repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
		SupplyStatePendingReview: {100},
	}}
	prober := newSupplierProberStub()
	settings := autoPromoteSettings()
	settings.ProbeModel = "claude-probe-model"
	svc := newLifecycleService(repo, store, settings, prober)

	svc.sweepPendingReview(context.Background())
	require.Len(t, prober.models, 1)
	assert.Equal(t, "claude-probe-model", prober.models[0])
}

// ============================================================================
// 计分与入池
// ============================================================================

func TestProbeSuccessIncrementsPasses(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Status: StatusActive, Extra: map[string]any{
		SupplyStateExtraKey: SupplyStatePendingReview,
		// JSONB 读回来是 float64，计数逻辑必须认这个类型。
		SupplyProbePassesExtraKey:    float64(1),
		SupplyProbationSinceExtraKey: time.Now().Add(-time.Minute).Format(time.RFC3339),
	}}
	repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
		SupplyStatePendingReview: {100},
	}}
	prober := newSupplierProberStub()
	svc := newLifecycleService(repo, store, autoPromoteSettings(), prober)

	svc.sweepPendingReview(context.Background())

	updates := store.extraUpdates[100]
	assert.Equal(t, 2, updates[SupplyProbePassesExtraKey])
	assert.Empty(t, updates[SupplyProbeErrorExtraKey])
	assert.NotEmpty(t, updates[SupplyProbeAtExtraKey])
	// 观察窗还没跑满（刚才才开始），不许入池。
	assert.NotEqual(t, SupplyStateActive, updates[SupplyStateExtraKey])
	assert.Zero(t, store.schedulableCalls)
}

// 失败一次清零，并把原因留给供给者看——只有他自己能修（重新授权）。
func TestProbeFailureResetsPassesAndRecordsReason(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Status: StatusActive, Extra: map[string]any{
		SupplyStateExtraKey:       SupplyStatePendingReview,
		SupplyProbePassesExtraKey: float64(5),
	}}
	repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
		SupplyStatePendingReview: {100},
	}}
	prober := newSupplierProberStub()
	prober.results[100] = &ScheduledTestResult{Status: "failed", ErrorMessage: "invalid refresh token"}
	svc := newLifecycleService(repo, store, autoPromoteSettings(), prober)

	svc.sweepPendingReview(context.Background())

	updates := store.extraUpdates[100]
	assert.Equal(t, 0, updates[SupplyProbePassesExtraKey])
	assert.Equal(t, "invalid refresh token", updates[SupplyProbeErrorExtraKey])
	assert.Zero(t, store.schedulableCalls, "探测失败绝不入池")
}

func TestProbeErrorMessageIsTruncated(t *testing.T) {
	long := make([]byte, supplyProbeErrorMaxLen*2)
	for i := range long {
		long[i] = 'x'
	}
	message := supplyProbeErrorMessage(nil, &ScheduledTestResult{Status: "failed", ErrorMessage: string(long)})
	assert.Len(t, message, supplyProbeErrorMaxLen)

	assert.Equal(t, "boom", supplyProbeErrorMessage(errors.New("boom"), nil))
	assert.NotEmpty(t, supplyProbeErrorMessage(nil, nil), "失败总要有个说法")
}

// 达标 + 观察窗跑满 + 开关开着 = 入池。顺序是先可调度再写状态：
// 反过来的中间失败会留下一个「看起来入池了但永远拿不到流量」的静默故障。
func TestProbePromotesWhenAllConditionsMet(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Status: StatusActive, Extra: map[string]any{
		SupplyStateExtraKey:          SupplyStatePendingReview,
		SupplyProbePassesExtraKey:    float64(1),
		SupplyProbationSinceExtraKey: time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
	}}
	repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
		SupplyStatePendingReview: {100},
	}}
	prober := newSupplierProberStub()
	svc := newLifecycleService(repo, store, autoPromoteSettings(), prober)

	svc.sweepPendingReview(context.Background())

	assert.True(t, store.schedulableSets[100])
	assert.Equal(t, SupplyStateActive, store.extraUpdates[100][SupplyStateExtraKey])
}

// 三个条件缺一不可。
func TestProbeDoesNotPromoteWhenAnyConditionMissing(t *testing.T) {
	cases := []struct {
		name     string
		settings *SupplyProbationSettings
		since    time.Time
		passes   float64
	}{
		{
			name:     "自动入池关着",
			settings: &SupplyProbationSettings{Enabled: false, MinObservationMinutes: 60, RequiredSuccesses: 2, ProbeIntervalMinutes: 15},
			since:    time.Now().Add(-2 * time.Hour),
			passes:   5,
		},
		{
			name:     "观察窗没跑满",
			settings: autoPromoteSettings(),
			since:    time.Now().Add(-time.Minute),
			passes:   5,
		},
		{
			name:     "成功次数不够",
			settings: &SupplyProbationSettings{Enabled: true, MinObservationMinutes: 60, RequiredSuccesses: 10, ProbeIntervalMinutes: 15},
			since:    time.Now().Add(-2 * time.Hour),
			passes:   0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newSupplierAccountStoreStub()
			store.accounts[100] = &Account{ID: 100, Status: StatusActive, Extra: map[string]any{
				SupplyStateExtraKey:          SupplyStatePendingReview,
				SupplyProbePassesExtraKey:    tc.passes,
				SupplyProbationSinceExtraKey: tc.since.Format(time.RFC3339),
			}}
			repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
				SupplyStatePendingReview: {100},
			}}
			prober := newSupplierProberStub()
			svc := newLifecycleService(repo, store, tc.settings, prober)

			svc.sweepPendingReview(context.Background())

			assert.Zero(t, store.schedulableCalls, "条件不全就不许入池")
			assert.NotEqual(t, SupplyStateActive, store.extraUpdates[100][SupplyStateExtraKey])
		})
	}
}

// 观察起点缺失的历史账号：就地补一个起点，并且**不因为查不到时间就判为已观察够久**。
func TestProbeStampsMissingProbationStartAndDoesNotPromote(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Status: StatusActive, Extra: map[string]any{
		SupplyStateExtraKey:       SupplyStatePendingReview,
		SupplyProbePassesExtraKey: float64(99),
	}}
	repo := &supplierOnboardingRepoStub{idsByState: map[string][]int64{
		SupplyStatePendingReview: {100},
	}}
	prober := newSupplierProberStub()
	svc := newLifecycleService(repo, store, autoPromoteSettings(), prober)

	svc.sweepPendingReview(context.Background())

	assert.NotEmpty(t, store.extraUpdates[100][SupplyProbationSinceExtraKey])
	assert.Zero(t, store.schedulableCalls, "没有观察起点就不算观察够久")
}

// ============================================================================
// 生命周期钩子
// ============================================================================

func TestSupplierLifecycleServiceIsNilSafe(t *testing.T) {
	var svc *SupplierLifecycleService
	assert.NotPanics(t, func() {
		svc.SetLeaderLock(nil, nil)
		svc.Start()
		svc.Stop()
		svc.runOnce()
	})
}

func TestSupplierLifecycleServiceStartStop(t *testing.T) {
	store := newSupplierAccountStoreStub()
	repo := &supplierOnboardingRepoStub{}
	svc := newLifecycleService(repo, store, autoPromoteSettings(), nil)
	svc.interval = time.Hour // 只跑启动那一次

	svc.Start()
	svc.Stop()
	// 重复 Stop 不能 panic（关掉一个已关的 channel 会）。
	assert.NotPanics(t, func() { svc.Stop() })
}

func TestSupplyExtraHelpers(t *testing.T) {
	account := &Account{Extra: map[string]any{
		"t":   time.Unix(1700000000, 0).UTC().Format(time.RFC3339),
		"bad": "not-a-time",
		"i":   float64(7),
		"i64": int64(8),
		"s":   "  hi  ",
	}}

	parsed, ok := supplyExtraTime(account, "t")
	require.True(t, ok)
	assert.Equal(t, int64(1700000000), parsed.Unix())

	// 坏掉的时间戳当作「没有」而不是零值——零值会让所有「到期了吗」瞬间为真。
	_, ok = supplyExtraTime(account, "bad")
	assert.False(t, ok)
	_, ok = supplyExtraTime(account, "missing")
	assert.False(t, ok)
	_, ok = supplyExtraTime(nil, "t")
	assert.False(t, ok)

	assert.Equal(t, 7, supplyExtraInt(account, "i"))
	assert.Equal(t, 8, supplyExtraInt(account, "i64"))
	assert.Zero(t, supplyExtraInt(account, "missing"))
	assert.Zero(t, supplyExtraInt(nil, "i"))

	assert.Equal(t, "hi", supplyExtraString(account, "s"))
	assert.Empty(t, supplyExtraString(account, "i"))
	assert.Empty(t, supplyExtraString(nil, "s"))
}
