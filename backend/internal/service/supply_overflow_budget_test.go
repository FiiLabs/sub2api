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

	// exhaustedErr 让「统计写失败」这条路可测——它必须不改变任何行为。
	exhaustedErr error
	// exhaustedCalls 记 RecordOverflowExhausted 被调了几次。
	// 只看返回值分不清「没调」和「调了但吞了错」，而这两者差着一个丢失的经营信号。
	exhaustedCalls int
	// exhaustedDays 记每次收到的日期，钉住「用的是平台时区的今天」。
	exhaustedDays []string
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

func (s *stubOverflowCounter) RecordOverflowExhausted(_ context.Context, day time.Time) error {
	s.exhaustedCalls++
	s.exhaustedDays = append(s.exhaustedDays, day.Format("2006-01-02"))
	return s.exhaustedErr
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

// ============================================================================
// 兜底池也耗尽的计数（迁移 236）
// ============================================================================
//
// 这三条盯的是同一件事的三个面：它**数得到**、它**只在该数的时候数**、
// 以及它**数不进去也不能改变任何行为**。
//
// 为什么值得单独测：这个计数是「该不该加兜底账号」的唯一依据（见定价方案 §9.5）。
// 它漏数一次，没有任何症状——面板上少一个数，而运营据此以为兜底容量够用。

func TestSelectAccountWithLoadAwareness_CountsWhenOverflowPoolIsAlsoEmpty(t *testing.T) {
	// 两个池都空：请求注定失败，但这一刻正是要被数下来的那一刻。
	repo := newGroupAwareAccountRepo(map[int64][]Account{
		testSupplyGroupID:    {},
		testFirstPartyGroupI: {},
	})
	svc, groupRepo := newOverflowGateway(t, repo, overflowLimitedJSON(5))
	counter := &stubOverflowCounter{allowed: true}
	withOverflowCounter(t, counter)

	groupID := testSupplyGroupID
	_, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	assert.Contains(t, repo.listedGroups, testFirstPartyGroupI, "得真的去兜底池找过，才谈得上兜底池也空了")
	assert.Equal(t, 1, counter.exhaustedCalls, "消费者拿到了错误却没被数下来——加兜底账号的依据就此丢失")
	require.Len(t, counter.exhaustedDays, 1)
	assert.Equal(t, timezone.Now().Format("2006-01-02"), counter.exhaustedDays[0],
		"日期不是平台时区的今天，跨时区部署的日报会错位")
}

func TestSelectAccountWithLoadAwareness_DoesNotCountWhenOverflowSucceeds(t *testing.T) {
	// 溢出成功 = 保险生效了，用户什么都没感觉到。数进去会让「保险不够赔」
	// 这个信号被「保险正常工作」的次数淹掉，而两者要的处置完全相反。
	repo := newGroupAwareAccountRepo(map[int64][]Account{
		testSupplyGroupID:    {},
		testFirstPartyGroupI: {supplyPoolAccount(1, testFirstPartyGroupI)},
	})
	svc, groupRepo := newOverflowGateway(t, repo, overflowLimitedJSON(5))
	counter := &stubOverflowCounter{allowed: true}
	withOverflowCounter(t, counter)

	groupID := testSupplyGroupID
	_, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	require.NoError(t, err)
	assert.Zero(t, counter.exhaustedCalls)
}

func TestSelectAccountWithLoadAwareness_DoesNotCountWhenBudgetBlockedTheOverflow(t *testing.T) {
	// 配额挡住 ≠ 兜底池空了。前者已经有 denied_count 在数，且处置是「调预算」；
	// 后者的处置是「加账号」。混进同一个计数里，面板就再也分不出该做哪件事。
	repo := newGroupAwareAccountRepo(map[int64][]Account{
		testSupplyGroupID:    {},
		testFirstPartyGroupI: {},
	})
	svc, groupRepo := newOverflowGateway(t, repo, overflowLimitedJSON(5))
	counter := &stubOverflowCounter{allowed: false}
	withOverflowCounter(t, counter)

	groupID := testSupplyGroupID
	_, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	assert.Zero(t, counter.exhaustedCalls, "配额拦下的那次被记成了兜底耗尽——两种处置会被混为一谈")
}

func TestRecordSupplyOverflowExhausted_FailureChangesNothing(t *testing.T) {
	// 统计写失败必须是完全无声的：这一步发生在请求已经注定失败之后，
	// 为一次统计写失败再叠一层错误，没有任何人受益。
	repo := newGroupAwareAccountRepo(map[int64][]Account{
		testSupplyGroupID:    {},
		testFirstPartyGroupI: {},
	})
	svc, groupRepo := newOverflowGateway(t, repo, overflowLimitedJSON(5))
	counter := &stubOverflowCounter{allowed: true, exhaustedErr: errors.New("db down")}
	withOverflowCounter(t, counter)

	groupID := testSupplyGroupID
	_, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	// 拿回的仍然是原本那个错误，不是数据库的错误。
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	assert.NotContains(t, err.Error(), "db down")
	assert.Equal(t, 1, counter.exhaustedCalls)
}

func TestRecordSupplyOverflowExhausted_NoCounterIsNotAFailure(t *testing.T) {
	// 计数器没装（单实例部署没接上 provider、单测里）不该让请求路径出任何岔子。
	repo := newGroupAwareAccountRepo(map[int64][]Account{
		testSupplyGroupID:    {},
		testFirstPartyGroupI: {},
	})
	svc, groupRepo := newOverflowGateway(t, repo, overflowLimitedJSON(0))
	SetSupplyOverflowCounter(nil)

	groupID := testSupplyGroupID
	_, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	assert.ErrorIs(t, err, ErrNoAvailableAccounts)
}
