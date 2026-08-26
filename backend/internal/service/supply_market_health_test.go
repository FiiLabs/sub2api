//go:build unit

// APEXONE-EXT: 双边市场——定价健康度的派生量。
//
// 这个文件测的全是**除法和排序**，看着像在测标准库。值得测的理由是它们的
// 失效方式都不报错：
//
//   - 分母为零时给 NaN，JSON 序列化会当场炸掉，于是「今天还没有流水」这个
//     完全正常的状态让整个面板返回 500
//   - 中位数写成平均值，「大多数供给者其实赚不到钱」这件事就被一两个重度账号
//     藏起来了——而那正是供给流失的前兆
//   - 算中位数时就地排序入参，前端那张按产出降序的榜会变成随机序
package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type healthRepoStub struct {
	health     *SupplyMarketHealth
	err        error
	windowSeen []int
}

func (s *healthRepoStub) Aggregate(_ context.Context, windowDays int) (*SupplyMarketHealth, error) {
	s.windowSeen = append(s.windowSeen, windowDays)
	return s.health, s.err
}

func healthService(repo SupplyMarketHealthRepository) *SupplyMarketHealthService {
	return &SupplyMarketHealthService{repo: repo}
}

// ============================================================================
// 窗口
// ============================================================================

func TestSupplyHealthWindowClampsInsteadOfErroring(t *testing.T) {
	// 越界夹取而不是报错：这个参数来自界面上的窗口切换器，
	// 一个手敲坏了的查询串要的是「给我看默认那档」，不是一页错误。
	for _, tc := range []struct{ in, want int }{
		{0, supplyHealthDefaultWindowDays},
		{-7, supplyHealthDefaultWindowDays},
		{7, 7},
		{90, 90},
		{9999, supplyHealthMaxWindowDays},
	} {
		repo := &healthRepoStub{health: &SupplyMarketHealth{}}
		got, err := healthService(repo).Get(context.Background(), tc.in)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got.WindowDays)
		assert.Equal(t, []int{tc.want}, repo.windowSeen,
			"夹过的窗口没传给仓储——面板显示的天数和实际统计的天数会对不上")
	}
}

// ============================================================================
// 除零：所有比率的分母都可能是零
// ============================================================================

func TestSupplyHealthRatiosSurviveAnEmptyWindow(t *testing.T) {
	// 全新部署、或者切到一个还没有流量的短窗口。这是最常见的第一次打开面板的状态。
	repo := &healthRepoStub{health: &SupplyMarketHealth{}}

	got, err := healthService(repo).Get(context.Background(), 30)
	require.NoError(t, err)
	assert.Zero(t, got.EffectiveMultiplier)
	assert.Zero(t, got.OverflowShare)
	assert.Zero(t, got.MedianMonthlyOutput)

	// 真正的回归点：NaN 会让 encoding/json 报错，于是整个面板 500。
	encoded, err := json.Marshal(got)
	require.NoError(t, err, "比率算出了 NaN——面板会返回 500 而不是一屏零")
	assert.Contains(t, string(encoded), `"effective_multiplier":0`)
}

func TestSupplyHealthDerivesMarginAndRatiosFromMoney(t *testing.T) {
	repo := &healthRepoStub{health: &SupplyMarketHealth{
		ListValue:         1000,
		Revenue:           180,
		SupplierPayout:    153,
		OverflowListValue: 250,
	}}

	got, err := healthService(repo).Get(context.Background(), 30)
	require.NoError(t, err)
	assert.InDelta(t, 27.0, got.GrossMargin, 1e-9, "毛利 = 营收 − 分成，不减固定成本")
	assert.InDelta(t, 0.18, got.EffectiveMultiplier, 1e-9)
	assert.InDelta(t, 0.25, got.OverflowShare, 1e-9)
}

// ============================================================================
// 中位数
// ============================================================================

func TestSupplyHealthUsesMedianNotMean(t *testing.T) {
	// 一个重度账号 + 四个几乎没产出的账号。平均值 $620 会让人以为供给侧健康，
	// 中位数 $50 才说出真相：五个供给者里有四个赚不到钱。
	repo := &healthRepoStub{health: &SupplyMarketHealth{
		SupplyAccounts: []SupplyAccountOutput{
			{MonthlyOutput: 3000},
			{MonthlyOutput: 60},
			{MonthlyOutput: 50},
			{MonthlyOutput: 40},
			{MonthlyOutput: 30},
		},
	}}

	got, err := healthService(repo).Get(context.Background(), 30)
	require.NoError(t, err)
	assert.InDelta(t, 50.0, got.MedianMonthlyOutput, 1e-9,
		"用了平均值——一两个重度账号会把「大多数供给者赚不到钱」整个藏起来")
}

func TestSupplyHealthMedianAveragesTheMiddlePairWhenEven(t *testing.T) {
	repo := &healthRepoStub{health: &SupplyMarketHealth{
		SupplyAccounts: []SupplyAccountOutput{
			{MonthlyOutput: 100}, {MonthlyOutput: 40},
			{MonthlyOutput: 20}, {MonthlyOutput: 10},
		},
	}}

	got, err := healthService(repo).Get(context.Background(), 30)
	require.NoError(t, err)
	assert.InDelta(t, 30.0, got.MedianMonthlyOutput, 1e-9)
}

func TestSupplyHealthMedianDoesNotReorderTheLeaderboard(t *testing.T) {
	// 榜单按产出降序发给前端。算中位数时就地排序入参的话，那张表会变成升序——
	// 一个不报错、只是"看起来怪"的回归，而没人会想到是中位数干的。
	repo := &healthRepoStub{health: &SupplyMarketHealth{
		SupplyAccounts: []SupplyAccountOutput{
			{AccountID: 1, MonthlyOutput: 300},
			{AccountID: 2, MonthlyOutput: 200},
			{AccountID: 3, MonthlyOutput: 100},
		},
	}}

	got, err := healthService(repo).Get(context.Background(), 30)
	require.NoError(t, err)
	assert.InDelta(t, 200.0, got.MedianMonthlyOutput, 1e-9)
	require.Len(t, got.SupplyAccounts, 3)
	assert.Equal(t, int64(1), got.SupplyAccounts[0].AccountID, "中位数把榜单顺序打乱了")
	assert.Equal(t, int64(3), got.SupplyAccounts[2].AccountID)
}

// ============================================================================
// fail-open：配置读不到不该让面板打不开
// ============================================================================

func TestSupplyHealthWorksWithoutSettings(t *testing.T) {
	// settings/groups 都没注入。对照项留零，聚合本身照常返回——
	// 这是一块给人看的经营读数，让它因为一次设置读失败而整页打不开是不成比例的。
	repo := &healthRepoStub{health: &SupplyMarketHealth{ListValue: 100, Revenue: 18}}

	got, err := healthService(repo).Get(context.Background(), 30)
	require.NoError(t, err)
	assert.Zero(t, got.ConfiguredMultiplier)
	assert.Zero(t, got.ConfiguredShare)
	assert.InDelta(t, 0.18, got.EffectiveMultiplier, 1e-9, "对照项缺失不该影响实测值")
}

func TestSupplyHealthPropagatesAggregateFailure(t *testing.T) {
	// 聚合本身失败是真失败：这时面板上一个数都没有，画一屏零会让人
	// 以为「今天没流水」——那和「查不出来」是两回事。
	_, err := healthService(&healthRepoStub{err: errors.New("db down")}).
		Get(context.Background(), 30)
	require.Error(t, err)
}

func TestSupplyHealthHandlesNilAggregate(t *testing.T) {
	// 仓储回 (nil, nil)：不该 panic。
	got, err := healthService(&healthRepoStub{}).Get(context.Background(), 30)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, supplyHealthDefaultWindowDays, got.WindowDays)
}

func TestSupplyHealthUnavailableWithoutRepo(t *testing.T) {
	_, err := healthService(nil).Get(context.Background(), 30)
	require.Error(t, err)

	var nilService *SupplyMarketHealthService
	_, err = nilService.Get(context.Background(), 30)
	require.Error(t, err)
}

// ============================================================================
// 线上形状
// ============================================================================

// 键的全集钉死。与提现那两个 DTO 同一条理由：这份读数里有每个供给账号的
// 归属人 id 与收益，多发一个字段就是多漏一份经营数据。
func TestSupplyHealthWireShapeIsSnakeCase(t *testing.T) {
	assert.Equal(t,
		[]string{
			"configured_multiplier", "configured_share", "effective_multiplier",
			"effective_share", "exhausted_today", "gross_margin", "list_value",
			"median_monthly_output", "overflow_list_value", "overflow_share",
			"revenue", "supplier_count", "supplier_payout", "supply_accounts",
			"window_days",
		},
		jsonKeys(t, &SupplyMarketHealth{}))

	assert.Equal(t,
		[]string{
			"account_id", "list_value", "monthly_output", "name",
			"owner_user_id", "requests", "supplier_earned",
		},
		jsonKeys(t, SupplyAccountOutput{}))
}
