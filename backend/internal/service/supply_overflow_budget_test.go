//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubOverflowCounter 记下每次调用，让测试能断言「配额到底有没有被消耗」——
// 只看放行/拒绝分不清「没消耗」和「消耗了但允许」。
type stubOverflowCounter struct {
	allowed    bool
	err        error
	usage      *SupplyOverflowUsage
	usageErr   error
	calls      []int // 每次 TryConsumeDailyOverflow 收到的 limit
	daysSeen   []string
	usageCalls int
}

func (s *stubOverflowCounter) TryConsumeDailyOverflow(_ context.Context, day time.Time, limit int) (bool, error) {
	s.calls = append(s.calls, limit)
	s.daysSeen = append(s.daysSeen, day.Format("2006-01-02"))
	return s.allowed, s.err
}

func (s *stubOverflowCounter) GetDailyOverflowUsage(_ context.Context, _ time.Time) (*SupplyOverflowUsage, error) {
	s.usageCalls++
	return s.usage, s.usageErr
}

// withOverflowCounter 装上计数器并保证测试结束后卸掉。
//
// 这是包级单例（见 supply_overflow_budget.go 里为什么），不清理的话下一个测试会
// 继承上一个的桩，失败现象还会随测试顺序变化。
func withOverflowCounter(t *testing.T, counter SupplyOverflowCounter) {
	t.Helper()
	SetSupplyOverflowCounter(counter)
	t.Cleanup(func() { SetSupplyOverflowCounter(nil) })
}

func TestAllowSupplyOverflow_NoCounterInstalledAllows(t *testing.T) {
	// 「本功能没装」不等于「配额已满」。当成满会静默地关掉溢出，比不装更难查。
	SetSupplyOverflowCounter(nil)
	assert.True(t, allowSupplyOverflow(context.Background(), 100))
}

func TestAllowSupplyOverflow_PassesLimitThroughAndHonorsVerdict(t *testing.T) {
	counter := &stubOverflowCounter{allowed: true}
	withOverflowCounter(t, counter)

	assert.True(t, allowSupplyOverflow(context.Background(), 42))
	require.Equal(t, []int{42}, counter.calls)
	// 「今天」按平台时区算，不是 UTC：否则中国部署的配额会在早上八点才重置。
	assert.Equal(t, []string{timezone.Now().Format("2006-01-02")}, counter.daysSeen)

	counter.allowed = false
	assert.False(t, allowSupplyOverflow(context.Background(), 42))
	require.Len(t, counter.calls, 2)
}

func TestAllowSupplyOverflow_FailsClosedOnCounterError(t *testing.T) {
	// 装了却读不出来 = 「不知道今天花了多少」。花平台的钱的决定不能建立在这个之上。
	withOverflowCounter(t, &stubOverflowCounter{allowed: true, err: errors.New("db down")})
	assert.False(t, allowSupplyOverflow(context.Background(), 100))
}

func TestGetSupplyOverflowUsage_ReturnsZeroValueInsteadOfError(t *testing.T) {
	svc := &SettingService{}

	// 没装计数器。
	SetSupplyOverflowCounter(nil)
	usage := svc.GetSupplyOverflowUsage(context.Background())
	require.NotNil(t, usage)
	assert.Equal(t, int64(0), usage.OverflowCount)
	assert.NotEmpty(t, usage.Day, "即使读不到计数，也要给出这是哪一天的读数")

	// 装了但报错：同样给零值——这是一块只读读数，不该让整个池配置页打不开。
	withOverflowCounter(t, &stubOverflowCounter{usageErr: errors.New("db down")})
	usage = svc.GetSupplyOverflowUsage(context.Background())
	require.NotNil(t, usage)
	assert.Equal(t, int64(0), usage.DeniedCount)
}

func TestGetSupplyOverflowUsage_PassesThroughRealReading(t *testing.T) {
	counter := &stubOverflowCounter{usage: &SupplyOverflowUsage{
		Day: "2026-08-18", OverflowCount: 17, DeniedCount: 3,
	}}
	withOverflowCounter(t, counter)

	usage := (&SettingService{}).GetSupplyOverflowUsage(context.Background())
	require.NotNil(t, usage)
	assert.Equal(t, int64(17), usage.OverflowCount)
	assert.Equal(t, int64(3), usage.DeniedCount)
	assert.Equal(t, 1, counter.usageCalls)
}

// ============================================================================
// 闸门装在调度上之后的端到端行为
// ============================================================================

// overflowLimitedJSON 是 overflowEnabledJSON 加上一个日配额。
func overflowLimitedJSON(limit int) string {
	return fmt.Sprintf(
		`{"enabled":true,"supply_group_id":10,"overflow_group_id":11,"daily_overflow_limit":%d}`, limit)
}

func TestSelectAccountWithLoadAwareness_BudgetExhaustedDoesNotOverflow(t *testing.T) {
	// 配额用完后请求拿回它原本就会拿到的 ErrNoAvailableAccounts，而且**没碰**自营池。
	// 后半句才是重点：只断言错误的话，一个「先溢出成功再报错」的实现也能过。
	repo := newGroupAwareAccountRepo(map[int64][]Account{
		testSupplyGroupID:    {},
		testFirstPartyGroupI: {supplyPoolAccount(1, testFirstPartyGroupI)},
	})
	svc, groupRepo := newOverflowGateway(t, repo, overflowLimitedJSON(5))
	counter := &stubOverflowCounter{allowed: false}
	withOverflowCounter(t, counter)

	groupID := testSupplyGroupID
	_, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	assert.NotContains(t, repo.listedGroups, testFirstPartyGroupI)
	assert.Equal(t, []int{5}, counter.calls, "配额上限要来自池配置的同一份快照")
}

func TestSelectAccountWithLoadAwareness_BudgetAvailableStillOverflows(t *testing.T) {
	repo := newGroupAwareAccountRepo(map[int64][]Account{
		testSupplyGroupID:    {},
		testFirstPartyGroupI: {supplyPoolAccount(1, testFirstPartyGroupI)},
	})
	svc, groupRepo := newOverflowGateway(t, repo, overflowLimitedJSON(5))
	withOverflowCounter(t, &stubOverflowCounter{allowed: true})

	groupID := testSupplyGroupID
	result, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, repo.listedGroups, testFirstPartyGroupI)
}

func TestSelectAccountWithLoadAwareness_BudgetNotConsumedWhenOverflowDisabled(t *testing.T) {
	// 溢出压根没开的分组耗尽时不能去消耗配额，否则任何一个空分组都能把预算吃光。
	repo := newGroupAwareAccountRepo(map[int64][]Account{testSupplyGroupID: {}})
	svc, groupRepo := newOverflowGateway(t, repo, "")
	counter := &stubOverflowCounter{allowed: true}
	withOverflowCounter(t, counter)

	groupID := testSupplyGroupID
	_, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	assert.Empty(t, counter.calls)
}

func TestSetSupplyPoolSettings_ClampsNegativeDailyLimitToUnlimited(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{getErr: ErrSettingNotFound}
	svc := newSupplyPoolSettingService(t, repo)

	settings := &SupplyPoolSettings{DailyOverflowLimit: -1}
	require.NoError(t, svc.SetSupplyPoolSettings(context.Background(), settings))
	assert.Equal(t, 0, settings.DailyOverflowLimit)
	assert.Contains(t, repo.setValue, `"daily_overflow_limit":0`)
}
