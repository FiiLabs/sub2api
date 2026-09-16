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
	// tickSlugs 记下每一轮传下去的「今天已经开始的活动」。
	// 它是「第二期活动不会把第一期的天数算进来」这条性质的唯一观测点。
	tickSlugs [][]string

	granted       map[string]int
	grantedByUser map[string]map[int64]int
	grantedErr    error

	candidates    map[string][]SupplyIncentiveCandidate
	candidatesErr error
	queries       []SupplyIncentiveCandidateQuery
}

func (r *incentiveRepoStub) TickActiveDays(_ context.Context, day string, startedSlugs []string) (int64, error) {
	r.tickDays = append(r.tickDays, day)
	r.tickSlugs = append(r.tickSlugs, startedSlugs)
	if r.tickErr != nil {
		return 0, r.tickErr
	}
	return r.tickCount, nil
}

func (r *incentiveRepoStub) GrantStats(_ context.Context, prefix string) (*SupplyIncentiveGrantStats, error) {
	if r.grantedErr != nil {
		return nil, r.grantedErr
	}
	stats := &SupplyIncentiveGrantStats{ByUser: map[int64]int{}}
	for userID, n := range r.grantedByUser[prefix] {
		stats.ByUser[userID] = n
		stats.Total += n
	}
	// granted 是只给总数、不关心是谁的那批用例用的简写。
	if total, ok := r.granted[prefix]; ok && stats.Total == 0 {
		stats.Total = total
	}
	return stats, nil
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
	worker      *SupplierIncentiveWorker
	repo        *incentiveRepoStub
	credit      *incentiveCreditStub
	settingRepo *incentiveSettingRepoStub
}

func newIncentiveWorkerHarness(t *testing.T, incentiveJSON, settlementJSON string) *incentiveWorkerHarness {
	t.Helper()
	invalidateSupplyIncentiveCache()
	invalidateSupplierSettlementCache()
	invalidateSupplyOnboardingCache()
	t.Cleanup(func() {
		invalidateSupplyIncentiveCache()
		invalidateSupplierSettlementCache()
		invalidateSupplyOnboardingCache()
	})

	settingRepo := &incentiveSettingRepoStub{
		values: map[string]string{
			SettingKeySupplyIncentive:    incentiveJSON,
			SettingKeySupplierSettlement: settlementJSON,
			// 每人每档的发放上限复用接入的每人号数上限，所以 worker 会读它。
			SettingKeySupplyOnboarding: `{"max_accounts_per_user":2}`,
		},
	}
	repo := &incentiveRepoStub{
		granted:       map[string]int{},
		grantedByUser: map[string]map[int64]int{},
		candidates:    map[string][]SupplyIncentiveCandidate{},
	}
	credit := &incentiveCreditStub{notApplied: map[string]bool{}}

	return &incentiveWorkerHarness{
		worker:      NewSupplierIncentiveWorker(repo, credit, &SettingService{settingRepo: settingRepo}, time.Hour),
		repo:        repo,
		credit:      credit,
		settingRepo: settingRepo,
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
	incentiveTwoTierJSON = `{"enabled":true,"programs":[{"slug":"bind26q4","start_at":"2020-01-01","tiers":[
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
func TestIncentiveWorkerUnlimitedTierPassesNoLimit(t *testing.T) {
	h := newIncentiveWorkerHarness(t,
		`{"enabled":true,"programs":[{"slug":"bind26q4","start_at":"2020-01-01","tiers":[
			{"min_active_days":10,"amount_usd":5,"slots":0}]}]}`, settlementOnJSON)

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
			{"slug":"cl26q4","start_at":"2020-01-01","platform":"anthropic","tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]},
			{"slug":"any26q4","start_at":"2020-01-01","tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]}]}`,
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

// ---------------------------------------------------------------------------
// 每人每档上限（挡解绑重挂）
// ---------------------------------------------------------------------------

// 上游订阅查重只看未删除的行，而解绑会软删该行并抹掉 credentials——于是同一份
// 订阅解绑后能再挂一次，拿到一个新的 account id，也就是一个全新的幂等键。
// 按账号计的名额挡不住它，只有把拿满的人整个排除掉才挡得住。
func TestIncentiveWorkerExcludesUsersAtPerUserCap(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOnJSON)
	prefix := SupplyIncentiveRequestPrefix("bind26q4", 0)
	// 上限是 2（harness 里的 max_accounts_per_user）：7 号拿满了，9 号还差一份。
	h.repo.grantedByUser[prefix] = map[int64]int{7: 2, 9: 1}

	h.worker.RunOnce(context.Background())

	var q *SupplyIncentiveCandidateQuery
	for i := range h.repo.queries {
		if h.repo.queries[i].RequestPrefix == prefix {
			q = &h.repo.queries[i]
		}
	}
	require.NotNil(t, q)
	assert.Equal(t, []int64{7}, q.ExcludedUserIDs, "只排除已经拿满的那个人")
	assert.Equal(t, 57, q.Limit, "已发 3 份，60 个名额还剩 57")
}

// 同一轮之内也要数。候选查询排除的是**查询那一刻**拿满的人，而一个人名下两个
// 够格的号会在同一批候选里一起出现——不在循环里累计的话，上限为 1 时他会一次拿两份。
func TestIncentiveWorkerCapsWithinASingleRound(t *testing.T) {
	h := newIncentiveWorkerHarness(t,
		`{"enabled":true,"programs":[{"slug":"bind26q4","start_at":"2020-01-01","tiers":[
			{"min_active_days":10,"amount_usd":5,"slots":60}]}]}`, settlementOnJSON)
	// 上限压到 1。
	h.settingRepo.values[SettingKeySupplyOnboarding] = `{"max_accounts_per_user":1}`
	invalidateSupplyOnboardingCache()

	prefix := SupplyIncentiveRequestPrefix("bind26q4", 0)
	h.repo.candidates[prefix] = []SupplyIncentiveCandidate{
		{AccountID: 1, OwnerUserID: 42, ActiveDays: 30},
		{AccountID: 2, OwnerUserID: 42, ActiveDays: 20}, // 同一个人的第二个号
		{AccountID: 3, OwnerUserID: 43, ActiveDays: 15},
	}

	h.worker.RunOnce(context.Background())

	require.Len(t, h.credit.accrued, 2, "42 只该拿一份，43 拿一份")
	assert.Equal(t, "cmp:bind26q4:t0:a1", h.credit.accrued[0].RequestID, "同一人里天数多的先拿")
	assert.Equal(t, "cmp:bind26q4:t0:a3", h.credit.accrued[1].RequestID)
}

// 上限为 2 时，一个人名下两个**真号**该拿两份——防刷不能把正常的多号共享也挡掉。
func TestIncentiveWorkerAllowsTwoAccountsUnderCapTwo(t *testing.T) {
	h := newIncentiveWorkerHarness(t,
		`{"enabled":true,"programs":[{"slug":"bind26q4","start_at":"2020-01-01","tiers":[
			{"min_active_days":10,"amount_usd":5,"slots":60}]}]}`, settlementOnJSON)

	prefix := SupplyIncentiveRequestPrefix("bind26q4", 0)
	h.repo.candidates[prefix] = []SupplyIncentiveCandidate{
		{AccountID: 1, OwnerUserID: 42, ActiveDays: 30},
		{AccountID: 2, OwnerUserID: 42, ActiveDays: 20},
	}

	h.worker.RunOnce(context.Background())
	assert.Len(t, h.credit.accrued, 2)
}

// 接入上限配成 0（不限）时，发放上限取 1 而不是跟着不限。
//
// 0 表达的是「挂号数量不设限」这条供给侧策略，不可能是一个有意为之的奖励策略——
// 没有人会刻意配置「一个人可以无限次领同一档」。读不出明确意图时取最严的那个。
func TestIncentiveWorkerPerUserCapDefaultsToOneWhenOnboardingUnlimited(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOnJSON)
	h.settingRepo.values[SettingKeySupplyOnboarding] = `{"max_accounts_per_user":0}`
	invalidateSupplyOnboardingCache()

	assert.Equal(t, 1, h.worker.perUserGrantCap(context.Background()))
}

func TestIncentiveWorkerPerUserCapFollowsOnboardingLimit(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveTwoTierJSON, settlementOnJSON)
	h.settingRepo.values[SettingKeySupplyOnboarding] = `{"max_accounts_per_user":3}`
	invalidateSupplyOnboardingCache()

	assert.Equal(t, 3, h.worker.perUserGrantCap(context.Background()))
}

// ---------------------------------------------------------------------------
// 分期与新用户
// ---------------------------------------------------------------------------

func incentiveProgramJSON(slug, startAt string, newUsersOnly bool) string {
	return `{"enabled":true,"programs":[{"slug":"` + slug + `","start_at":"` + startAt + `"` +
		`,"new_users_only":` + map[bool]string{true: "true", false: "false"}[newUsersOnly] +
		`,"tiers":[{"min_active_days":10,"amount_usd":5,"slots":60}]}]}`
}

func incentiveDay(offsetDays int) string {
	return time.Now().UTC().AddDate(0, 0, offsetDays).Format(SupplyIncentiveDayFormat)
}

// 桶只推进**已经开始**的活动。这是「第二期不会把第一期的天数算进来」的机制本身：
// 一期还没到起算日的活动，它的桶今天一天都不该涨。
func TestIncentiveWorkerTicksOnlyStartedPrograms(t *testing.T) {
	h := newIncentiveWorkerHarness(t,
		`{"enabled":true,"programs":[
			{"slug":"past","start_at":"`+incentiveDay(-10)+`","tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]},
			{"slug":"today","start_at":"`+incentiveDay(0)+`","tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]},
			{"slug":"future","start_at":"`+incentiveDay(7)+`","tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]}]}`,
		settlementOnJSON)

	h.worker.RunOnce(context.Background())

	require.Len(t, h.repo.tickSlugs, 1)
	assert.ElementsMatch(t, []string{"past", "today"}, h.repo.tickSlugs[0],
		"起算日当天就该开始计数，还没到的一天都不该涨")
}

// 桶的推进与 Enabled 无关：可以先配好起算日让天数跑起来，等文案定稿再开开关，
// 届时此前累计的天数照数。这正是「开关只管发不发钱」那条分工。
func TestIncentiveWorkerTicksProgramBucketsWhileDisabled(t *testing.T) {
	h := newIncentiveWorkerHarness(t,
		`{"enabled":false,"programs":[{"slug":"bind26q4","start_at":"`+incentiveDay(-1)+
			`","tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]}]}`,
		settlementOnJSON)

	h.worker.RunOnce(context.Background())

	require.Len(t, h.repo.tickSlugs, 1)
	assert.Equal(t, []string{"bind26q4"}, h.repo.tickSlugs[0])
	assert.Empty(t, h.credit.accrued, "开关关着不发钱")
}

// 还没到起算日的活动整期跳过：一次候选查询都不该发出去。
func TestIncentiveWorkerSkipsProgramsBeforeStart(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveProgramJSON("soon", incentiveDay(3), false), settlementOnJSON)
	h.repo.candidates[SupplyIncentiveRequestPrefix("soon", 0)] = []SupplyIncentiveCandidate{
		{AccountID: 7, OwnerUserID: 42, ActiveDays: 99},
	}

	h.worker.RunOnce(context.Background())

	assert.Empty(t, h.repo.queries, "活动还没开始就不该去查候选")
	assert.Empty(t, h.credit.accrued)
}

// 三个新字段都要原样传到 SQL 层：slug 决定读哪个桶，另外两个决定「新用户」怎么判。
func TestIncentiveWorkerPassesProgramScopeToQuery(t *testing.T) {
	start := incentiveDay(-5)
	h := newIncentiveWorkerHarness(t, incentiveProgramJSON("bind26q4", start, true), settlementOnJSON)
	h.worker.RunOnce(context.Background())

	require.Len(t, h.repo.queries, 1)
	q := h.repo.queries[0]
	assert.Equal(t, "bind26q4", q.ProgramSlug, "天数门槛要读这期的桶，不是账号总计")
	assert.True(t, q.NewUsersOnly)
	assert.Equal(t, start, q.StartAt.Format(SupplyIncentiveDayFormat),
		"新用户的分界线就是这期的起算日")
}

// 不限新老的活动，StartAt 仍然要带上（SQL 侧靠 NewUsersOnly 决定用不用它），
// 但 NewUsersOnly 必须是 false——否则一期普惠活动会静默变成拉新活动。
func TestIncentiveWorkerDefaultsToAllUsers(t *testing.T) {
	h := newIncentiveWorkerHarness(t, incentiveProgramJSON("bind26q4", incentiveDay(-5), false), settlementOnJSON)
	h.worker.RunOnce(context.Background())

	require.Len(t, h.repo.queries, 1)
	assert.False(t, h.repo.queries[0].NewUsersOnly)
}

func TestStartedProgramSlugs(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	settings := &SupplyIncentiveSettings{Programs: []SupplyIncentiveProgram{
		{Slug: "a", StartAt: "2026-10-01"},
		{Slug: "b", StartAt: "2026-10-10"},
		{Slug: "c", StartAt: "2026-10-11"},
		{Slug: "d", StartAt: "garbage"},
	}}
	assert.Equal(t, []string{"a", "b"}, startedProgramSlugs(settings, now))

	// 没有活动时返回 nil 而不是空切片：repo 那侧据此走「只推进总计」那条路。
	assert.Nil(t, startedProgramSlugs(&SupplyIncentiveSettings{}, now))
	assert.Nil(t, startedProgramSlugs(nil, now))
}
