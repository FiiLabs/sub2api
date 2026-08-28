//go:build unit

// APEXONE-EXT: 供给者每日共享上限——判定逻辑与调度闸的单测。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func capAccount(cost float64, tokens int64) *Account {
	extra := map[string]any{}
	if cost > 0 {
		extra[SupplyDailyCostLimitExtraKey] = cost
	}
	if tokens > 0 {
		extra[SupplyDailyTokenLimitExtraKey] = tokens
	}
	return &Account{ID: 1, Extra: extra}
}

// 访问器要能扛住 extra 里的各种脏值。这些值全都真实存在过：JSON 往返把整数变成
// float64，人手改库会写进字符串，回滚脚本会留下负数。
func TestSupplyDailyCapAccessorsHandleDirtyValues(t *testing.T) {
	assert.Zero(t, (*Account)(nil).GetSupplyDailyCostLimit())
	assert.Zero(t, (&Account{}).GetSupplyDailyCostLimit())
	assert.Zero(t, (&Account{Extra: map[string]any{}}).GetSupplyDailyTokenLimit())

	// JSON 往返后整数是 float64——快照路径上这是常态而非例外。
	acc := &Account{Extra: map[string]any{
		SupplyDailyCostLimitExtraKey:  float64(20),
		SupplyDailyTokenLimitExtraKey: float64(500000),
	}}
	assert.Equal(t, 20.0, acc.GetSupplyDailyCostLimit())
	assert.Equal(t, int64(500000), acc.GetSupplyDailyTokenLimit())

	// 负数按「不限」处理，不是按 0 拦死：脏数据不该把一个号永久踢出池子。
	neg := &Account{Extra: map[string]any{
		SupplyDailyCostLimitExtraKey:  -5.0,
		SupplyDailyTokenLimitExtraKey: -1.0,
	}}
	assert.Zero(t, neg.GetSupplyDailyCostLimit())
	assert.Zero(t, neg.GetSupplyDailyTokenLimit())
	assert.False(t, neg.HasSupplyDailyCap())
}

// 边界是 >=：达到上限即停。这里 << 和 <= 的差别是一次静默的 off-by-one，
// 现象是「我设了 20，它花到 20.00 还在接单」。
func TestCheckSupplyDailyCapSchedulabilityBoundary(t *testing.T) {
	cases := []struct {
		name       string
		costLimit  float64
		tokenLimit int64
		cost       float64
		tokens     int64
		want       WindowCostSchedulability
	}{
		{"未设上限一律放行", 0, 0, 9999, 9999999, WindowCostSchedulable},
		{"金额未到上限", 20, 0, 19.99, 0, WindowCostSchedulable},
		{"金额刚好达到上限即停", 20, 0, 20, 0, WindowCostNotSchedulable},
		{"金额超过上限", 20, 0, 20.01, 0, WindowCostNotSchedulable},
		{"token 未到上限", 0, 1000, 0, 999, WindowCostSchedulable},
		{"token 刚好达到上限即停", 0, 1000, 0, 1000, WindowCostNotSchedulable},
		// 「先到先生效」：两个维度里任意一个满了就停，不要求两个都满。
		{"双上限-只有金额满", 20, 1000, 20, 1, WindowCostNotSchedulable},
		{"双上限-只有token满", 20, 1000, 0.01, 1000, WindowCostNotSchedulable},
		{"双上限-都没满", 20, 1000, 1, 1, WindowCostSchedulable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := capAccount(tc.costLimit, tc.tokenLimit).CheckSupplyDailyCapSchedulability(tc.cost, tc.tokens)
			assert.Equal(t, tc.want, got)
		})
	}
}

// 硬上限：永远不返回 StickyOnly。
//
// 这条守的是「有人觉得应该像 window_cost 那样留点余量」——那道闸的默认预留是
// **10 美元**，对一个填了 5 美元的供给者等于把他填的数字翻三倍。要加宽限只能按
// 比例，且必须先让这条测试变红。
func TestSupplyDailyCapNeverReturnsStickyOnly(t *testing.T) {
	acc := capAccount(10, 0)
	for _, cost := range []float64{9.99, 10, 10.01, 15, 100} {
		assert.NotEqual(t, WindowCostStickyOnly, acc.CheckSupplyDailyCapSchedulability(cost, 0),
			"cost=%v 返回了 StickyOnly；每日上限是硬上限", cost)
	}
}

// ---------------------------------------------------------------------------
// 调度闸
// ---------------------------------------------------------------------------

type dailyCapUsageRepoStub struct {
	UsageLogRepository
	stats     *usagestats.AccountStats
	err       error
	calls     int
	batchArgs []int64
	startTime time.Time
}

func (s *dailyCapUsageRepoStub) GetAccountWindowStats(_ context.Context, _ int64, start time.Time) (*usagestats.AccountStats, error) {
	s.calls++
	s.startTime = start
	return s.stats, s.err
}

func (s *dailyCapUsageRepoStub) GetAccountWindowStatsBatch(_ context.Context, ids []int64, start time.Time) (map[int64]*usagestats.AccountStats, error) {
	s.calls++
	s.batchArgs = ids
	s.startTime = start
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[int64]*usagestats.AccountStats, len(ids))
	for _, id := range ids {
		out[id] = s.stats
	}
	return out, nil
}

// 没设上限的号一次查询都不该发。
//
// 这是「存量号完全不受影响」这条产品承诺的机器可读版本：断言的是 calls == 0，
// 而不是肉眼看代码里有没有早返回。
func TestSupplyDailyCapGateSkipsUncappedAccountsEntirely(t *testing.T) {
	repo := &dailyCapUsageRepoStub{stats: &usagestats.AccountStats{StandardCost: 999}}
	svc := &GatewayService{usageLogRepo: repo}

	assert.True(t, svc.isAccountSchedulableForSupplyDailyCap(context.Background(), &Account{ID: 7}, false))
	assert.Equal(t, 0, repo.calls, "没设上限的号不该产生任何用量查询")
}

// 中转接入的号必须同样被拦。
//
// 这条是类型判断陷阱的回归测试：window_cost 那道闸开头是
// IsAnthropicOAuthOrSetupToken()，照抄会静默跳过每一个中转号——因为中转接入建的是
// AccountTypeAPIKey，而 OAuth 接入建的是 AccountTypeSetupToken。
func TestSupplyDailyCapGateAppliesToRelayAccounts(t *testing.T) {
	repo := &dailyCapUsageRepoStub{stats: &usagestats.AccountStats{StandardCost: 50}}
	svc := &GatewayService{usageLogRepo: repo}

	for _, typ := range []string{AccountTypeAPIKey, AccountTypeSetupToken} {
		acc := capAccount(10, 0)
		acc.Type = typ
		assert.Falsef(t, svc.isAccountSchedulableForSupplyDailyCap(context.Background(), acc, false),
			"type=%s 的供给号没有被每日上限拦住", typ)
	}
}

// 查不出用量时放行，与另外两道闸同向：一次数据库抖动不该把所有设了上限的号
// 一起踢出池子。
func TestSupplyDailyCapGateFailsOpen(t *testing.T) {
	svc := &GatewayService{usageLogRepo: &dailyCapUsageRepoStub{err: errors.New("db down")}}
	assert.True(t, svc.isAccountSchedulableForSupplyDailyCap(context.Background(), capAccount(1, 0), false))

	// repo 完全没装配时同理。
	assert.True(t, (&GatewayService{}).isAccountSchedulableForSupplyDailyCap(context.Background(), capAccount(1, 0), false))
}

// 窗口起点必须是 UTC 零点，且**不跟随平台时区设置**。
//
// 供给者被告知的是「UTC 零点重置」。若这里误用 timezone.Today()，运营改一次
// 平台时区就会把所有人的重置点悄悄挪走，而没有任何地方会报错。
func TestSupplyDailyCapUsesUTCMidnightNotPlatformTimezone(t *testing.T) {
	repo := &dailyCapUsageRepoStub{stats: &usagestats.AccountStats{}}
	svc := &GatewayService{usageLogRepo: repo}

	svc.isAccountSchedulableForSupplyDailyCap(context.Background(), capAccount(10, 0), false)

	require.Equal(t, 1, repo.calls)
	got := repo.startTime
	assert.Equal(t, time.UTC, got.Location(), "窗口起点必须是 UTC")
	assert.Zero(t, got.Hour())
	assert.Zero(t, got.Minute())
	assert.Zero(t, got.Second())
	assert.Equal(t, time.Now().UTC().Truncate(24*time.Hour), got)
}

// 预取：N 个设了上限的号只发一次批量查询；一个都没有时一次也不发。
func TestSupplyDailyCapPrefetchBatchesAndShortCircuits(t *testing.T) {
	repo := &dailyCapUsageRepoStub{stats: &usagestats.AccountStats{StandardCost: 3, Tokens: 30}}
	svc := &GatewayService{usageLogRepo: repo}

	accounts := []Account{
		*capAccount(10, 0), // 设了上限
		{ID: 2},            // 没设
		{ID: 3, Extra: map[string]any{SupplyDailyTokenLimitExtraKey: float64(100)}},
	}
	accounts[0].ID = 1

	ctx := svc.withSupplyDailyCapPrefetch(context.Background(), accounts)
	assert.Equal(t, 1, repo.calls, "应当只发一次批量查询")
	assert.ElementsMatch(t, []int64{1, 3}, repo.batchArgs, "只预取设了上限的号")

	// 预取命中后，闸门不再产生新查询。
	usage, ok := supplyDailyUsageFromPrefetchContext(ctx, 1)
	require.True(t, ok)
	assert.Equal(t, 3.0, usage.Cost)
	assert.Equal(t, int64(30), usage.Tokens)

	callsAfterPrefetch := repo.calls
	svc.isAccountSchedulableForSupplyDailyCap(ctx, &accounts[0], false)
	assert.Equal(t, callsAfterPrefetch, repo.calls, "预取命中时闸门不该再查一次")

	// 没有任何号设上限时，一次查询也不发。
	repo2 := &dailyCapUsageRepoStub{stats: &usagestats.AccountStats{}}
	svc2 := &GatewayService{usageLogRepo: repo2}
	svc2.withSupplyDailyCapPrefetch(context.Background(), []Account{{ID: 9}, {ID: 10}})
	assert.Equal(t, 0, repo2.calls)
}
