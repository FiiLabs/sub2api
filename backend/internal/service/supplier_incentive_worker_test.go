//go:build unit

// APEXONE-EXT: 双边市场——挂号奖励发放 worker 的单元测试。
//
// 这一组只钉 worker 自己的判断（发不发、发几个、发给谁、参数对不对）。
// 三件与 SQL 语义绑死的事（当天只加一次、断线不清零、候选排序）在
// repository 的真库测试里，替身表达不了那些性质。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 替身
// ---------------------------------------------------------------------------

type incentiveRepoStub struct {
	tickDays  []string
	tickErr   error
	tickCount int64

	granted    map[string]int
	grantedErr error

	candidates    map[string][]SupplyIncentiveCandidate
	candidatesErr error
	queries       []SupplyIncentiveCandidateQuery
}

func (r *incentiveRepoStub) TickActiveDays(_ context.Context, day string) (int64, error) {
	r.tickDays = append(r.tickDays, day)
	if r.tickErr != nil {
		return 0, r.tickErr
	}
	return r.tickCount, nil
}

func (r *incentiveRepoStub) CountGranted(_ context.Context, prefix string) (int, error) {
	if r.grantedErr != nil {
		return 0, r.grantedErr
	}
	return r.granted[prefix], nil
}

func (r *incentiveRepoStub) ListCandidates(_ context.Context, q SupplyIncentiveCandidateQuery) ([]SupplyIncentiveCandidate, error) {
	r.queries = append(r.queries, q)
	if r.candidatesErr != nil {
		return nil, r.candidatesErr
	}
	return r.candidates[q.RequestPrefix], nil
}

// incentiveCreditStub 只实现 Accrue，其余方法一律 panic——这个 worker 碰到
// 钱包的任何别的入口都是设计错误，让它当场炸掉比默默返回零值好。
type incentiveCreditStub struct {
	accrued []SupplierAccrueParams
	err     error
	// notApplied 里的 request_id 返回 (false, nil)，模拟幂等命中。
	notApplied map[string]bool
}

func (c *incentiveCreditStub) Accrue(_ context.Context, params SupplierAccrueParams) (bool, error) {
	c.accrued = append(c.accrued, params)
	if c.err != nil {
		return false, c.err
	}
	if c.notApplied[params.RequestID] {
		return false, nil
	}
	return true, nil
}

func (c *incentiveCreditStub) EnsureWallet(context.Context, int64) (*SupplierCreditSummary, error) {
	panic("unexpected EnsureWallet call")
}
func (c *incentiveCreditStub) GetWallet(context.Context, int64) (*SupplierCreditSummary, error) {
	panic("unexpected GetWallet call")
}
func (c *incentiveCreditStub) Spend(context.Context, int64, float64, string) (bool, error) {
	panic("unexpected Spend call")
}
func (c *incentiveCreditStub) Clawback(context.Context, SupplierClawbackParams) (*SupplierClawbackResult, error) {
	panic("unexpected Clawback call")
}
func (c *incentiveCreditStub) ThawMatured(context.Context, int64) (float64, error) {
	panic("unexpected ThawMatured call")
}
func (c *incentiveCreditStub) ThawAllMaturedUsers(context.Context, int) (int, float64, error) {
	panic("unexpected ThawAllMaturedUsers call")
}
func (c *incentiveCreditStub) ListLedger(context.Context, SupplierCreditLedgerFilter) ([]SupplierCreditLedgerEntry, int64, error) {
	panic("unexpected ListLedger call")
}

// incentiveWorkerHarness 攒出一台可跑的 worker。
type incentiveWorkerHarness struct {
	worker *SupplierIncentiveWorker
	repo   *incentiveRepoStub
	credit *incentiveCreditStub
}

func newIncentiveWorkerHarness(t *testing.T, incentiveJSON, settlementJSON string) *incentiveWorkerHarness {
	t.Helper()
	invalidateSupplyIncentiveCache()
	invalidateSupplierSettlementCache()
	t.Cleanup(func() {
		invalidateSupplyIncentiveCache()
		invalidateSupplierSettlementCache()
	})

	settingRepo := &incentiveSettingRepoStub{
		values: map[string]string{
			SettingKeySupplyIncentive:    incentiveJSON,
			SettingKeySupplierSettlement: settlementJSON,
		},
	}
	repo := &incentiveRepoStub{
		granted:    map[string]int{},
		candidates: map[string][]SupplyIncentiveCandidate{},
	}
	credit := &incentiveCreditStub{notApplied: map[string]bool{}}

	return &incentiveWorkerHarness{
		worker: NewSupplierIncentiveWorker(repo, credit, &SettingService{settingRepo: settingRepo}, time.Hour),
		repo:   repo,
		credit: credit,
	}
}

// incentiveSettingRepoStub 认两个 key（激励规则 + 结算参数），读到别的就 panic。
type incentiveSettingRepoStub struct {
	values map[string]string
}

func (r *incentiveSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	v, ok := r.values[key]
	if !ok {
		panic("unexpected settings key: " + key)
	}
	return v, nil
}
func (r *incentiveSettingRepoStub) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}
func (r *incentiveSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}
func (r *incentiveSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}
func (r *incentiveSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}
func (r *incentiveSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}
func (r *incentiveSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

const (
	incentiveTwoTierJSON = `{"enabled":true,"programs":[{"slug":"bind26q4","tiers":[
		{"min_active_days":10,"amount_usd":5,"slots":60},
		{"min_active_days":30,"amount_usd":20,"slots":5}]}]}`
	settlementOnJSON  = `{"enabled":true,"share_ratio":0.5,"freeze_hours":72}`
	settlementOffJSON = `{"enabled":false,"share_ratio":0.5,"freeze_hours":72}`
)

// ---------------------------------------------------------------------------
// 天数累加
// ---------------------------------------------------------------------------

// 天数与「活动开没开」无关：一开一关就让所有人的天数从头再来的话，
// 门槛就变成了「运营手抖的次数」。
func TestIncentiveWorkerTicksDaysEvenWhenDisabled(t *testing.T) {
	h := newIncentiveWorkerHarness(t, `{"enabled":false}`, settlementOnJSON)
	h.worker.RunOnce(context.Background())

	require.Len(t, h.repo.tickDays, 1)
	assert.Equal(t, time.Now().UTC().Format("2006-01-02"), h.repo.tickDays[0],
		"日期键必须是 UTC：跟着部署环境走会出现某天加两次或漏一天")
	assert.Empty(t, h.credit.accrued)
}

// 天数加不上不该连累已经够格的人——那些人的天数是前几轮攒下的。
func TestIncentiveWorkerStillGrantsWhenTickFails(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOnJSON)
	h.repo.tickErr = errors.New("db hiccup")
	h.repo.candidates[SupplyIncentiveRequestPrefix("bind26q4", 0)] = []SupplyIncentiveCandidate{
		{AccountID: 7, OwnerUserID: 42, ActiveDays: 12},
	}

	h.worker.RunOnce(context.Background())
	require.Len(t, h.credit.accrued, 1)
	assert.Equal(t, int64(42), h.credit.accrued[0].SupplierUserID)
}

// ---------------------------------------------------------------------------
// 发不发
// ---------------------------------------------------------------------------

// 分成关掉时不发奖：钱包的可见性与提现都挂在结算总开关上，
// 往一个用户看不见也提不出来的钱包里打钱，比不打更糟。
func TestIncentiveWorkerSkipsGrantsWhenSettlementDisabled(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOffJSON)
	h.repo.candidates[SupplyIncentiveRequestPrefix("bind26q4", 0)] = []SupplyIncentiveCandidate{
		{AccountID: 7, OwnerUserID: 42, ActiveDays: 12},
	}

	h.worker.RunOnce(context.Background())
	assert.Len(t, h.repo.tickDays, 1, "天数照常累加")
	assert.Empty(t, h.credit.accrued)
}

// 入账参数是这个功能对外的全部承诺，逐项钉死。
func TestIncentiveWorkerAccrueParams(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOnJSON)
	h.repo.candidates[SupplyIncentiveRequestPrefix("bind26q4", 1)] = []SupplyIncentiveCandidate{
		{AccountID: 99, OwnerUserID: 5, ActiveDays: 31},
	}

	h.worker.RunOnce(context.Background())
	require.Len(t, h.credit.accrued, 1)
	got := h.credit.accrued[0]

	assert.Equal(t, int64(5), got.SupplierUserID)
	assert.Equal(t, "cmp:bind26q4:t1:a99", got.RequestID)
	require.NotNil(t, got.AccountID)
	assert.Equal(t, int64(99), *got.AccountID)
	assert.Nil(t, got.ConsumerUserID,
		"活动奖励没有消费方——留 nil 才能让消费者退款的 clawback 碰不到它")
	assert.InDelta(t, 20.0, got.BasisAmount, 1e-9)
	assert.InDelta(t, 1.0, got.ShareRatio, 1e-9,
		"恒为 1.0，让流水里「基数 × 比例 = 金额」自洽")
	assert.Equal(t, 72, got.FreezeHours, "冻结窗复用结算参数，不单独配")
}

// ---------------------------------------------------------------------------
// 名额
// ---------------------------------------------------------------------------

// 名额是这个设计里唯一的成本闸门（门槛换成在线天数之后奖励不再自付）。
func TestIncentiveWorkerLimitsCandidatesToRemainingSlots(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOnJSON)
	prefix := SupplyIncentiveRequestPrefix("bind26q4", 1)
	h.repo.granted[prefix] = 3 // 5 个名额已用 3

	h.worker.RunOnce(context.Background())

	var tierQuery *SupplyIncentiveCandidateQuery
	for i := range h.repo.queries {
		if h.repo.queries[i].RequestPrefix == prefix {
			tierQuery = &h.repo.queries[i]
		}
	}
	require.NotNil(t, tierQuery)
	assert.Equal(t, 2, tierQuery.Limit, "只应再取 5-3=2 个")
	assert.Equal(t, 30, tierQuery.MinActiveDays)
}

func TestIncentiveWorkerSkipsExhaustedTier(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOnJSON)
	prefix := SupplyIncentiveRequestPrefix("bind26q4", 1)
	h.repo.granted[prefix] = 5

	h.worker.RunOnce(context.Background())
	for _, q := range h.repo.queries {
		assert.NotEqual(t, prefix, q.RequestPrefix, "名额用尽的档不该再查候选")
	}
}

// 数不出已发数就不发：读失败时放行等于把唯一的成本闸门打开。
func TestIncentiveWorkerFailsClosedWhenGrantedCountUnavailable(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOnJSON)
	h.repo.grantedErr = errors.New("db down")
	h.repo.candidates[SupplyIncentiveRequestPrefix("bind26q4", 0)] = []SupplyIncentiveCandidate{
		{AccountID: 7, OwnerUserID: 42, ActiveDays: 12},
	}

	h.worker.RunOnce(context.Background())
	assert.Empty(t, h.credit.accrued)
	assert.Empty(t, h.repo.queries)
}

// Slots=0（不限）时不该去数已发数——那次查询没有任何用处，
// 而它在 ledger 长大之后是一次全表扫描。
func TestIncentiveWorkerSkipsCountForUnlimitedTier(t *testing.T) {
	h := newIncentiveWorkerHarness(t,
		`{"enabled":true,"programs":[{"slug":"bind26q4","tiers":[
			{"min_active_days":10,"amount_usd":5,"slots":0}]}]}`, settlementOnJSON)
	h.repo.grantedErr = errors.New("CountGranted 不该被调用")

	h.worker.RunOnce(context.Background())
	require.Len(t, h.repo.queries, 1)
	assert.Zero(t, h.repo.queries[0].Limit, "0 = 交给 repo 用它自己的单轮上限兜住")
}

// ---------------------------------------------------------------------------
// 容错
// ---------------------------------------------------------------------------

// 一个人入账失败不该拖垮同一档里的其他人；失败的那个下一轮会重来。
func TestIncentiveWorkerContinuesAfterAccrueError(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOnJSON)
	h.repo.candidates[SupplyIncentiveRequestPrefix("bind26q4", 0)] = []SupplyIncentiveCandidate{
		{AccountID: 1, OwnerUserID: 11, ActiveDays: 12},
		{AccountID: 2, OwnerUserID: 22, ActiveDays: 11},
	}
	h.credit.err = errors.New("write conflict")

	h.worker.RunOnce(context.Background())
	assert.Len(t, h.credit.accrued, 2, "两个都要试过")
}

// 幂等命中（候选查询与入账之间的窗口）不是错误，不该打断这一档。
func TestIncentiveWorkerTreatsIdempotentHitAsSuccess(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOnJSON)
	h.repo.candidates[SupplyIncentiveRequestPrefix("bind26q4", 0)] = []SupplyIncentiveCandidate{
		{AccountID: 1, OwnerUserID: 11, ActiveDays: 12},
		{AccountID: 2, OwnerUserID: 22, ActiveDays: 11},
	}
	h.credit.notApplied["cmp:bind26q4:t0:a1"] = true

	h.worker.RunOnce(context.Background())
	assert.Len(t, h.credit.accrued, 2)
}

// 候选查不出来只跳过这一档，不影响别的档。
func TestIncentiveWorkerSkipsTierWhenCandidateQueryFails(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOnJSON)
	h.repo.candidatesErr = errors.New("db down")

	h.worker.RunOnce(context.Background())
	assert.Len(t, h.repo.queries, 2, "两档都试过")
	assert.Empty(t, h.credit.accrued)
}

// ---------------------------------------------------------------------------
// 平台过滤
// ---------------------------------------------------------------------------

// 空 platform = 全平台共用一个名额池（当前上线的形态）。
func TestIncentiveWorkerPassesPlatformFilter(t *testing.T) {
	h := newIncentiveWorkerHarness(t,
		`{"enabled":true,"programs":[
			{"slug":"cl26q4","platform":"anthropic","tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]},
			{"slug":"any26q4","tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]}]}`,
		settlementOnJSON)

	h.worker.RunOnce(context.Background())
	require.Len(t, h.repo.queries, 2)
	assert.Equal(t, PlatformAnthropic, h.repo.queries[0].Platform)
	assert.Empty(t, h.repo.queries[1].Platform, "空 = 不限平台")
}

// 停机信号必须能在档位之间生效，否则一轮扫到一半的 Stop() 要等到全部发完。
func TestIncentiveWorkerStopsOnContextCancel(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOnJSON)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	h.worker.RunOnce(ctx)
	assert.Empty(t, h.credit.accrued)
}
