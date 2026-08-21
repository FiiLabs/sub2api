//go:build unit

// APEXONE-EXT: 双边市场——失效事件服务的单元测试。
//
// 这一层里没有 SQL。三件真正会出错的事全在这里：
// 扫描的**顺序**、发信与标记的**先后**、熔断的**方向**。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// 假仓储
// ============================================================================

type fakeIncidentRepo struct {
	// calls 按调用顺序记下方法名，这是 Sweep 顺序断言的唯一依据。
	calls []string

	openN, resolveN int64
	openErr         error
	resolveErr      error

	pending    []SupplierAccountIncident
	pendingErr error

	notified    []int64
	notifyErr   error
	recentCount int
	recentErr   error
	recentSince time.Time
	recentUser  int64
}

func (f *fakeIncidentRepo) OpenIncidents(_ context.Context, _ int) (int64, error) {
	f.calls = append(f.calls, "open")
	return f.openN, f.openErr
}

func (f *fakeIncidentRepo) ResolveIncidents(_ context.Context, _ int) (int64, error) {
	f.calls = append(f.calls, "resolve")
	return f.resolveN, f.resolveErr
}

func (f *fakeIncidentRepo) ListPendingNotice(_ context.Context, _ int) ([]SupplierAccountIncident, error) {
	f.calls = append(f.calls, "pending")
	return f.pending, f.pendingErr
}

func (f *fakeIncidentRepo) MarkNotified(_ context.Context, id int64) error {
	f.calls = append(f.calls, "mark")
	if f.notifyErr != nil {
		return f.notifyErr
	}
	f.notified = append(f.notified, id)
	return nil
}

func (f *fakeIncidentRepo) List(_ context.Context, filter SupplierIncidentFilter) ([]SupplierAccountIncident, int64, error) {
	f.calls = append(f.calls, "list")
	// 把夹过的分页参数原样带回去给断言看。
	return []SupplierAccountIncident{{ID: int64(filter.Page), AccountID: int64(filter.PageSize)}}, 0, nil
}

func (f *fakeIncidentRepo) Summary(_ context.Context, windowDays, topN int) (*SupplierIncidentSummary, error) {
	f.calls = append(f.calls, "summary")
	return &SupplierIncidentSummary{WindowDays: windowDays, Opened: int64(topN)}, nil
}

func (f *fakeIncidentRepo) CountRecentByUser(_ context.Context, userID int64, since time.Time) (int, error) {
	f.calls = append(f.calls, "count")
	f.recentUser = userID
	f.recentSince = since
	return f.recentCount, f.recentErr
}

// fakeIncidentSender 记下每一封被要求发出的信，并可以让其中任意一封失败。
type fakeIncidentSender struct {
	sent    []int64
	failOn  map[int64]bool
	callsIn []string
}

func (f *fakeIncidentSender) NotifyIncident(_ context.Context, incident *SupplierAccountIncident) error {
	f.callsIn = append(f.callsIn, "send")
	if f.failOn[incident.ID] {
		return errors.New("smtp down")
	}
	f.sent = append(f.sent, incident.ID)
	return nil
}

// newIncidentSvc 组一个带假通知器的服务。构造函数吃的是具体类型，
// 这里要塞接口实现，所以直接建结构体。
func newIncidentSvc(repo SupplierIncidentRepository, sender supplierIncidentNoticeSender) *SupplierIncidentService {
	return &SupplierIncidentService{repo: repo, notifier: sender}
}

// ============================================================================
// Sweep
// ============================================================================

// 先关后开，发信排最后——顺序本身就是设计的一部分（见 Sweep 的注释）。
func TestSupplierIncidentSweep_ResolvesBeforeOpening(t *testing.T) {
	repo := &fakeIncidentRepo{}
	newIncidentSvc(repo, nil).Sweep(context.Background())

	require.Equal(t, []string{"resolve", "open"}, repo.calls)
}

// 没有通知器时不发信，但检测照跑：那是一种部署形态（没配 SMTP），不是故障。
func TestSupplierIncidentSweep_WithoutNotifierStillDetects(t *testing.T) {
	repo := &fakeIncidentRepo{openN: 3, resolveN: 1}
	newIncidentSvc(repo, nil).Sweep(context.Background())

	assert.NotContains(t, repo.calls, "pending")
	assert.Contains(t, repo.calls, "open")
}

// 一步失败不许打断后面两步：一次数据库抖动不该让整轮什么都不做。
func TestSupplierIncidentSweep_ContinuesAfterEachStepFails(t *testing.T) {
	repo := &fakeIncidentRepo{
		resolveErr: errors.New("boom"),
		openErr:    errors.New("boom"),
		pending:    []SupplierAccountIncident{{ID: 7, UserID: 1}},
	}
	sender := &fakeIncidentSender{}
	newIncidentSvc(repo, sender).Sweep(context.Background())

	require.Equal(t, []string{"resolve", "open", "pending", "mark"}, repo.calls)
	assert.Equal(t, []int64{7}, sender.sent)
}

// 发信成功才 MarkNotified。发失败的那条一个字都不许记，否则通知被永久吞掉。
func TestSupplierIncidentSweep_MarksOnlyWhatWasActuallySent(t *testing.T) {
	repo := &fakeIncidentRepo{pending: []SupplierAccountIncident{
		{ID: 1, UserID: 10},
		{ID: 2, UserID: 11},
		{ID: 3, UserID: 12},
	}}
	sender := &fakeIncidentSender{failOn: map[int64]bool{2: true}}

	newIncidentSvc(repo, sender).Sweep(context.Background())

	assert.Equal(t, []int64{1, 3}, sender.sent)
	assert.Equal(t, []int64{1, 3}, repo.notified, "发失败的那条不许被标记为已通知")
}

// 发信在标记之前——顺序反了的话 SMTP 一挂通知就没了。
// 用调用序列钉死：sender 与 repo 各自记账，交叉比对。
func TestSupplierIncidentSweep_SendsBeforeMarking(t *testing.T) {
	repo := &fakeIncidentRepo{pending: []SupplierAccountIncident{{ID: 1, UserID: 10}}}
	sender := &fakeIncidentSender{}

	// mark 时 sender 必须已经发过：借 notifyErr 之外的手段观察不到先后，
	// 于是让 MarkNotified 断言当时的发送计数。
	repo.notifyErr = nil
	newIncidentSvc(repo, &orderedSender{inner: sender, t: t, repo: repo}).Sweep(context.Background())

	assert.Equal(t, []int64{1}, repo.notified)
}

// orderedSender 在每次发信时断言这条事件**还没**被标记过。
type orderedSender struct {
	inner *fakeIncidentSender
	repo  *fakeIncidentRepo
	t     *testing.T
}

func (o *orderedSender) NotifyIncident(ctx context.Context, incident *SupplierAccountIncident) error {
	assert.NotContains(o.t, o.repo.notified, incident.ID, "标记必须发生在发信之后")
	return o.inner.NotifyIncident(ctx, incident)
}

// 列不出待发列表就整段放弃。
//
// 假仓储刻意**同时**返回一批行和一个错误：真实仓储出错时确实只返回 nil，
// 于是"出错就 return"和"出错只记日志然后继续"在那种输入下是同一个行为，
// 测不出差别。这里钉的是接口契约本身——err != nil 时那批行一个都不许用，
// 无论它是不是空的。哪天仓储改成"扫到一半出错、返回已扫出的部分"，
// 这条断言就是那次改动唯一的拦路者。
func TestSupplierIncidentSweep_PendingQueryFailureSendsNothing(t *testing.T) {
	repo := &fakeIncidentRepo{
		pendingErr: errors.New("boom"),
		pending:    []SupplierAccountIncident{{ID: 1, UserID: 10}},
	}
	sender := &fakeIncidentSender{}
	newIncidentSvc(repo, sender).Sweep(context.Background())

	assert.Empty(t, sender.sent)
	assert.NotContains(t, repo.calls, "mark")
}

// ctx 已经取消时不再往下发信：这一轮跑在生命周期任务的总超时里。
func TestSupplierIncidentSweep_StopsOnCancelledContext(t *testing.T) {
	repo := &fakeIncidentRepo{pending: []SupplierAccountIncident{{ID: 1}, {ID: 2}}}
	sender := &fakeIncidentSender{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	newIncidentSvc(repo, sender).notifyPending(ctx)
	assert.Empty(t, sender.sent)
}

// 没装配起来的服务被扫到时不许 panic。
func TestSupplierIncidentSweep_NilRepoIsInert(t *testing.T) {
	assert.NotPanics(t, func() {
		newIncidentSvc(nil, nil).Sweep(context.Background())
		var s *SupplierIncidentService
		s.Sweep(context.Background())
	})
}

// ============================================================================
// List / Summary
// ============================================================================

// 分页参数必须在进仓储之前被夹回合法区间。
func TestSupplierIncidentList_ClampsPaging(t *testing.T) {
	repo := &fakeIncidentRepo{}
	rows, _, err := newIncidentSvc(repo, nil).List(context.Background(), SupplierIncidentFilter{Page: -3, PageSize: 100000})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	assert.Positive(t, rows[0].ID, "page 必须被夹成正数")
	assert.LessOrEqual(t, rows[0].AccountID, int64(200), "page_size 必须有上限")
}

// 窗口值的夹取：0 走默认，超界夹到上限。
func TestSupplierIncidentSummary_ClampsWindow(t *testing.T) {
	svc := newIncidentSvc(&fakeIncidentRepo{}, nil)

	got, err := svc.Summary(context.Background(), 0, 5)
	require.NoError(t, err)
	assert.Equal(t, supplierIncidentDefaultWindowDays, got.WindowDays)

	got, err = svc.Summary(context.Background(), 99999, 5)
	require.NoError(t, err)
	assert.Equal(t, supplierIncidentMaxWindowDays, got.WindowDays)

	got, err = svc.Summary(context.Background(), 7, 5)
	require.NoError(t, err)
	assert.Equal(t, 7, got.WindowDays, "合法窗口原样透传")
}

// 没装配起来时报 503 而不是返回一个空列表——空列表和"最近没坏号"长得一样。
func TestSupplierIncidentReads_UnavailableWhenNotWired(t *testing.T) {
	svc := newIncidentSvc(nil, nil)

	_, _, err := svc.List(context.Background(), SupplierIncidentFilter{})
	require.Error(t, err)
	_, err = svc.Summary(context.Background(), 30, 10)
	require.Error(t, err)
}

// ============================================================================
// GuardOnboarding
// ============================================================================

// 闸默认是关的：连查询都不该发出去。
func TestSupplierIncidentGuard_DisabledByDefault(t *testing.T) {
	repo := &fakeIncidentRepo{recentCount: 9999}
	limits := DefaultSupplyOnboardingSettings()

	require.NoError(t, newIncidentSvc(repo, nil).GuardOnboarding(context.Background(), 42, limits))
	assert.NotContains(t, repo.calls, "count", "闸关着时一次查询都不该发")
}

// 开着且没到线 → 放行；到线 → 拦。边界是 >=。
func TestSupplierIncidentGuard_BlocksAtThreshold(t *testing.T) {
	limits := DefaultSupplyOnboardingSettings()
	limits.MaxIncidentsPerUser = 3

	for _, tc := range []struct {
		count   int
		blocked bool
	}{{0, false}, {2, false}, {3, true}, {4, true}} {
		repo := &fakeIncidentRepo{recentCount: tc.count}
		err := newIncidentSvc(repo, nil).GuardOnboarding(context.Background(), 42, limits)
		if tc.blocked {
			require.ErrorIs(t, err, ErrSupplierIncidentRateExceeded, "count=%d", tc.count)
		} else {
			require.NoError(t, err, "count=%d", tc.count)
		}
	}
}

// 查询失败**放行**。这是与其它两道闸相反的方向，理由写在 GuardOnboarding 的注释里。
func TestSupplierIncidentGuard_FailsOpenOnQueryError(t *testing.T) {
	repo := &fakeIncidentRepo{recentErr: errors.New("boom"), recentCount: 9999}
	limits := DefaultSupplyOnboardingSettings()
	limits.MaxIncidentsPerUser = 1

	assert.NoError(t, newIncidentSvc(repo, nil).GuardOnboarding(context.Background(), 42, limits))
}

// 窗口配置真的被用上了：往回数的起点必须落在配置的小时数上。
func TestSupplierIncidentGuard_UsesConfiguredWindow(t *testing.T) {
	repo := &fakeIncidentRepo{}
	limits := DefaultSupplyOnboardingSettings()
	limits.MaxIncidentsPerUser = 1
	limits.IncidentWindowHours = 48

	before := time.Now()
	require.NoError(t, newIncidentSvc(repo, nil).GuardOnboarding(context.Background(), 42, limits))

	assert.Equal(t, int64(42), repo.recentUser)
	elapsed := before.Sub(repo.recentSince)
	assert.InDelta(t, 48*time.Hour, elapsed, float64(time.Minute))
}

// 无效 userID 不查库：那是调用方的 bug，不是一个要被熔断的人。
func TestSupplierIncidentGuard_IgnoresInvalidUser(t *testing.T) {
	repo := &fakeIncidentRepo{recentCount: 9999}
	limits := DefaultSupplyOnboardingSettings()
	limits.MaxIncidentsPerUser = 1

	require.NoError(t, newIncidentSvc(repo, nil).GuardOnboarding(context.Background(), 0, limits))
	assert.NotContains(t, repo.calls, "count")
}

// limits 为 nil 时放行而不是 panic——接入路径上任何一次空指针都是一次 500。
func TestSupplierIncidentGuard_NilLimitsIsInert(t *testing.T) {
	assert.NotPanics(t, func() {
		require.NoError(t, newIncidentSvc(&fakeIncidentRepo{}, nil).GuardOnboarding(context.Background(), 42, nil))
	})
}
