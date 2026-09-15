//go:build unit

// APEXONE-EXT: 新会话产出均衡的单元测试。
//
// 这一层改的是调度热路径上的排序，所以测试的重心有一半在**它什么时候不该生效**：
// 关掉时、查不到用量时、有账号数据缺失时，行为必须与改动前逐字一致。剩下一半
// 才是「今日产出最少的那一档优先」这件正事。
package service

import (
	"context"
	"testing"
	"time"

	"fmt"

	usagestats "github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// balanceSettingRepoStub 只认均衡这一个 key，读到别的就 panic。
type balanceSettingRepoStub struct{ value string }

func (r *balanceSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeySupplyBalance {
		panic("unexpected settings key: " + key)
	}
	return r.value, nil
}
func (r *balanceSettingRepoStub) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}
func (r *balanceSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}
func (r *balanceSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}
func (r *balanceSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}
func (r *balanceSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}
func (r *balanceSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

// balanceTestService 攒一台只开/只关均衡的 GatewayService。
//
// 开关来自 settings 表而不是 config.yaml——现网跑在 TEE 里，改 config 要重新
// 发 proof reference，那个代价配不上一个调优开关（见 setting_supply_balance.go）。
func balanceTestService(t *testing.T, enabled bool, band float64) *GatewayService {
	t.Helper()
	resetSchedulingBalanceCache()
	invalidateSupplyBalanceCache()
	t.Cleanup(func() {
		resetSchedulingBalanceCache()
		invalidateSupplyBalanceCache()
	})
	raw := fmt.Sprintf(`{"enabled":%t,"band_usd":%g}`, enabled, band)
	return &GatewayService{
		settingService: &SettingService{settingRepo: &balanceSettingRepoStub{value: raw}},
	}
}

func balanceCtx(costs map[int64]float64) context.Context {
	return context.WithValue(context.Background(), schedulingBalancePrefetchContextKey, costs)
}

// --- 什么时候不该生效 ---

// 关掉时必须原样返回：这一层是加在别人已经跑了两年的调度路径上的。
func TestFilterByBalanceBand_DisabledIsIdentity(t *testing.T) {
	s := balanceTestService(t, false, 5)
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 50, 0, nil, AccountTypeOAuth),
		makeAccWithLoad(2, 50, 0, nil, AccountTypeOAuth),
	}

	got := s.filterByBalanceBand(balanceCtx(map[int64]float64{1: 0, 2: 999}), accounts)
	assert.Len(t, got, 2)
}

// 查不出用量就不改变调度行为，与三道用量闸「失败开放」同向。
func TestFilterByBalanceBand_NoPrefetchIsIdentity(t *testing.T) {
	s := balanceTestService(t, true, 5)
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 50, 0, nil, AccountTypeOAuth),
		makeAccWithLoad(2, 50, 0, nil, AccountTypeOAuth),
	}

	assert.Len(t, s.filterByBalanceBand(context.Background(), accounts), 2)
	assert.Len(t, s.filterByBalanceBand(balanceCtx(map[int64]float64{}), accounts), 2)
}

// 有账号查不到产出时**全部放行**，而不是把它当成 0。
//
// 这一条最容易写反：当成 0 会让一个数据缺失的号永远排在最前面、吃下全部新会话，
// 也就是把这次修的那个毛病原样重现一遍，只是换了个原因。
func TestFilterByBalanceBand_MissingCostFallsBackToAllCandidates(t *testing.T) {
	s := balanceTestService(t, true, 5)
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 50, 0, nil, AccountTypeOAuth),
		makeAccWithLoad(2, 50, 0, nil, AccountTypeOAuth),
	}

	// 只有 1 号有数据，2 号缺失。
	got := s.filterByBalanceBand(balanceCtx(map[int64]float64{1: 0}), accounts)
	assert.Len(t, got, 2, "任何一个候选缺数据，整层就该让路")
}

func TestFilterByBalanceBand_SingleCandidateUntouched(t *testing.T) {
	s := balanceTestService(t, true, 5)
	accounts := []accountWithLoad{makeAccWithLoad(1, 50, 0, nil, AccountTypeOAuth)}
	assert.Len(t, s.filterByBalanceBand(balanceCtx(map[int64]float64{1: 999}), accounts), 1)
}

// --- 正事 ---

// 今日零产出的号优先——这正是「挂了一个月颗粒无收」那批号该被补偿的地方。
func TestFilterByBalanceBand_PrefersLowestBand(t *testing.T) {
	s := balanceTestService(t, true, 5)
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 50, 0, nil, AccountTypeOAuth), // 今天吃了 $120
		makeAccWithLoad(2, 50, 0, nil, AccountTypeOAuth), // 今天 $0
		makeAccWithLoad(3, 50, 0, nil, AccountTypeOAuth), // 今天 $30
	}

	got := s.filterByBalanceBand(balanceCtx(map[int64]float64{1: 120, 2: 0, 3: 30}), accounts)
	require.Len(t, got, 1)
	assert.Equal(t, int64(2), got[0].account.ID)
}

// 同一档内全部保留，交回给既有的 LRU 打散。
//
// 这就是「分档而不是严格选最小」的意义：判据带 60 秒缓存，严格最小会让缓存刷新前
// 的并发新会话全部涌向同一个号——把一种不均衡换成另一种。
func TestFilterByBalanceBand_KeepsWholeBandForLRU(t *testing.T) {
	s := balanceTestService(t, true, 5)
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 50, 0, nil, AccountTypeOAuth), // $1.0  → 档 0
		makeAccWithLoad(2, 50, 0, nil, AccountTypeOAuth), // $4.9  → 档 0
		makeAccWithLoad(3, 50, 0, nil, AccountTypeOAuth), // $5.1  → 档 1
	}

	got := s.filterByBalanceBand(balanceCtx(map[int64]float64{1: 1.0, 2: 4.9, 3: 5.1}), accounts)
	require.Len(t, got, 2)
	ids := []int64{got[0].account.ID, got[1].account.ID}
	assert.ElementsMatch(t, []int64{1, 2}, ids)
}

// 带宽可配：把它调大，本来分开的两档会合成一档。
func TestFilterByBalanceBand_BandWidthIsConfigurable(t *testing.T) {
	costs := map[int64]float64{1: 1, 2: 40}
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 50, 0, nil, AccountTypeOAuth),
		makeAccWithLoad(2, 50, 0, nil, AccountTypeOAuth),
	}

	narrow := balanceTestService(t, true, 5)
	assert.Len(t, narrow.filterByBalanceBand(balanceCtx(costs), accounts), 1)

	wide := balanceTestService(t, true, 100)
	assert.Len(t, wide.filterByBalanceBand(balanceCtx(costs), accounts), 2)
}

// 0 / 负数 / NaN 的带宽夹回默认值。
//
// 0 尤其要夹：floor(cost/0) 是 ±Inf，分档会退化成「严格选最小」——正是这层
// 刻意避开的那个形态，而且不会有任何报错。
func TestSchedulingBalanceBandClampsBadConfig(t *testing.T) {
	ctx := context.Background()
	for _, band := range []float64{0, -1} {
		s := balanceTestService(t, true, band)
		assert.InDelta(t, SupplyBalanceBandUSDDefault, s.balanceSettings(ctx).Band(), 1e-9)
	}
	s := balanceTestService(t, true, 12.5)
	assert.InDelta(t, 12.5, s.balanceSettings(ctx).Band(), 1e-9)
	// 上限也夹：填成 5000 而不是 500 会让所有号落进同一档，均衡静默退化成纯 LRU。
	tooWide := balanceTestService(t, true, 99999)
	assert.InDelta(t, float64(SupplyBalanceBandUSDMax), tooWide.balanceSettings(ctx).Band(), 1e-9)
}

// 负的产出（理论上不该有，但聚合口径变过就可能）按 0 算，不该翻成负档号。
func TestFilterByBalanceBand_NegativeCostTreatedAsZero(t *testing.T) {
	s := balanceTestService(t, true, 5)
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 50, 0, nil, AccountTypeOAuth),
		makeAccWithLoad(2, 50, 0, nil, AccountTypeOAuth),
	}

	got := s.filterByBalanceBand(balanceCtx(map[int64]float64{1: -10, 2: 0}), accounts)
	assert.Len(t, got, 2, "-10 与 0 都该落在档 0")
}

// --- 预取 ---

type balanceStatsStub struct {
	UsageLogRepository
	calls     int
	requested [][]int64
	stats     map[int64]*usagestats.AccountStats
	err       error
}

func (s *balanceStatsStub) GetAccountWindowStatsBatch(
	_ context.Context, accountIDs []int64, _ time.Time,
) (map[int64]*usagestats.AccountStats, error) {
	s.calls++
	ids := make([]int64, len(accountIDs))
	copy(ids, accountIDs)
	s.requested = append(s.requested, ids)
	if s.err != nil {
		return nil, s.err
	}
	return s.stats, nil
}

// 关掉时**一次查询也不发**。这一层加在每一次调度上，「没开启就是零成本」
// 与供给者每日上限那道闸是同一个约定。
func TestWithSchedulingBalancePrefetch_DisabledIssuesNoQuery(t *testing.T) {
	s := balanceTestService(t, false, 5)
	stub := &balanceStatsStub{stats: map[int64]*usagestats.AccountStats{}}
	s.usageLogRepo = stub

	ctx := s.withSchedulingBalancePrefetch(context.Background(), []Account{{ID: 1}, {ID: 2}})
	assert.Zero(t, stub.calls)
	assert.Nil(t, schedulingBalanceCosts(ctx))
}

func TestWithSchedulingBalancePrefetch_FetchesAndCaches(t *testing.T) {
	s := balanceTestService(t, true, 5)
	stub := &balanceStatsStub{stats: map[int64]*usagestats.AccountStats{
		1: {StandardCost: 12.5},
		// 2 号没有记录——批量接口对未命中返回空，应当被当成「今天还没开张」的 0，
		// 而不是「数据缺失」。这与 filterByBalanceBand 里那条缺失判断不冲突：
		// 缺的是**这一轮没查过**的号，不是查了但今天没用过的号。
	}}
	s.usageLogRepo = stub

	ctx := s.withSchedulingBalancePrefetch(context.Background(), []Account{{ID: 1}, {ID: 2}})
	costs := schedulingBalanceCosts(ctx)
	require.NotNil(t, costs)
	assert.InDelta(t, 12.5, costs[1], 1e-9)
	assert.InDelta(t, 0, costs[2], 1e-9)
	require.Equal(t, 1, stub.calls)

	// 第二轮应当全部命中缓存，不再查库。
	ctx2 := s.withSchedulingBalancePrefetch(context.Background(), []Account{{ID: 1}, {ID: 2}})
	assert.Equal(t, 1, stub.calls, "60 秒内的第二次调度不该再发查询")
	assert.InDelta(t, 12.5, schedulingBalanceCosts(ctx2)[1], 1e-9)
}

// 只查缓存没覆盖到的那些号。
func TestWithSchedulingBalancePrefetch_OnlyFetchesMisses(t *testing.T) {
	s := balanceTestService(t, true, 5)
	stub := &balanceStatsStub{stats: map[int64]*usagestats.AccountStats{1: {StandardCost: 1}}}
	s.usageLogRepo = stub

	s.withSchedulingBalancePrefetch(context.Background(), []Account{{ID: 1}})
	require.Equal(t, 1, stub.calls)

	s.withSchedulingBalancePrefetch(context.Background(), []Account{{ID: 1}, {ID: 2}})
	require.Equal(t, 2, stub.calls)
	assert.Equal(t, []int64{2}, stub.requested[1], "1 号已在缓存里，不该重复查")
}

// 查询失败不塞 context——下游 filterByBalanceBand 看到空表就整层让路。
func TestWithSchedulingBalancePrefetch_ErrorLeavesContextClean(t *testing.T) {
	s := balanceTestService(t, true, 5)
	s.usageLogRepo = &balanceStatsStub{err: assert.AnError}

	ctx := s.withSchedulingBalancePrefetch(context.Background(), []Account{{ID: 1}})
	assert.Nil(t, schedulingBalanceCosts(ctx))
}
