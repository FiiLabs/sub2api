//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supplierCreditRepoStub 记录调用，不做任何持久化。
// 未被本组测试用到的方法一律 panic：多余的调用是回归信号，不该被静默吞掉。
type supplierCreditRepoStub struct {
	mu sync.Mutex

	thawAllCalls  int
	thawAllLimits []int
	thawAllUsers  int
	thawAllTotal  float64
	thawAllErr    error

	thawUserCalls []int64
	thawUserErr   error

	ensureCalls  []int64
	ensureResult *SupplierCreditSummary
	ensureErr    error

	listFilter *SupplierCreditLedgerFilter
	listResult []SupplierCreditLedgerEntry
	listTotal  int64
	listErr    error

	clawbackCalls  []SupplierClawbackParams
	clawbackResult *SupplierClawbackResult
	clawbackErr    error
}

func (r *supplierCreditRepoStub) ThawAllMaturedUsers(_ context.Context, limit int) (int, float64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.thawAllCalls++
	r.thawAllLimits = append(r.thawAllLimits, limit)
	return r.thawAllUsers, r.thawAllTotal, r.thawAllErr
}

func (r *supplierCreditRepoStub) ThawMatured(_ context.Context, userID int64) (float64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.thawUserCalls = append(r.thawUserCalls, userID)
	return 0, r.thawUserErr
}

func (r *supplierCreditRepoStub) EnsureWallet(_ context.Context, userID int64) (*SupplierCreditSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureCalls = append(r.ensureCalls, userID)
	return r.ensureResult, r.ensureErr
}

func (r *supplierCreditRepoStub) ListLedger(_ context.Context, filter SupplierCreditLedgerFilter) ([]SupplierCreditLedgerEntry, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listFilter = &filter
	return r.listResult, r.listTotal, r.listErr
}

func (r *supplierCreditRepoStub) GetWallet(context.Context, int64) (*SupplierCreditSummary, error) {
	panic("unexpected GetWallet call")
}
func (r *supplierCreditRepoStub) Accrue(context.Context, SupplierAccrueParams) (bool, error) {
	panic("unexpected Accrue call")
}
func (r *supplierCreditRepoStub) Spend(context.Context, int64, float64, string) (bool, error) {
	panic("unexpected Spend call")
}

func (r *supplierCreditRepoStub) Clawback(_ context.Context, params SupplierClawbackParams) (*SupplierClawbackResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clawbackCalls = append(r.clawbackCalls, params)
	return r.clawbackResult, r.clawbackErr
}

func (r *supplierCreditRepoStub) snapshotClawbackCalls() []SupplierClawbackParams {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]SupplierClawbackParams(nil), r.clawbackCalls...)
}

func (r *supplierCreditRepoStub) snapshotThawAllCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.thawAllCalls
}

func (r *supplierCreditRepoStub) snapshotThawUserCalls() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int64(nil), r.thawUserCalls...)
}

// ---------------------------------------------------------------------------
// SupplierThawService
// ---------------------------------------------------------------------------

func TestNewSupplierThawServiceDefaultsInterval(t *testing.T) {
	assert.Equal(t, SupplierThawDefaultInterval, NewSupplierThawService(&supplierCreditRepoStub{}, 0).interval)
	assert.Equal(t, SupplierThawDefaultInterval, NewSupplierThawService(&supplierCreditRepoStub{}, -time.Second).interval)
	assert.Equal(t, time.Minute, NewSupplierThawService(&supplierCreditRepoStub{}, time.Minute).interval)
}

func TestSupplierThawRunOnceSweepsWithBatchLimit(t *testing.T) {
	repo := &supplierCreditRepoStub{thawAllUsers: 3, thawAllTotal: 12.5}
	svc := NewSupplierThawService(repo, time.Hour)

	svc.runOnce()

	assert.Equal(t, 1, repo.snapshotThawAllCalls())
	// 单轮必须带上限：补偿型任务不需要一次做完，需要的是永远压不垮库。
	assert.Equal(t, []int{supplierThawBatchLimit}, repo.thawAllLimits)
}

func TestSupplierThawRunOnceSwallowsRepoError(t *testing.T) {
	repo := &supplierCreditRepoStub{thawAllErr: errors.New("deadlock detected")}
	svc := NewSupplierThawService(repo, time.Hour)

	// 不 panic、不重试：下一轮 ticker 自然重来，且解冻幂等。
	assert.NotPanics(t, svc.runOnce)
	assert.Equal(t, 1, repo.snapshotThawAllCalls())
}

func TestSupplierThawRunOnceSkipsWhenLockHeldByPeer(t *testing.T) {
	repo := &supplierCreditRepoStub{}
	lock := &fakeLeaderLockCache{}
	// 另一个实例先占住这一轮。
	held, err := lock.TryAcquireLeaderLock(context.Background(), supplierThawLeaderLockKey, "peer-instance", time.Minute)
	require.NoError(t, err)
	require.True(t, held)

	svc := NewSupplierThawService(repo, time.Hour)
	svc.SetLeaderLock(lock, nil)
	svc.runOnce()

	assert.Zero(t, repo.snapshotThawAllCalls(), "没抢到锁的实例必须整轮跳过")
	assert.Equal(t, "peer-instance", lock.heldBy(supplierThawLeaderLockKey), "跳过的实例不能顺手把别人的锁删了")
}

func TestSupplierThawRunOnceReleasesLockAfterSweep(t *testing.T) {
	repo := &supplierCreditRepoStub{}
	lock := &fakeLeaderLockCache{}
	svc := NewSupplierThawService(repo, time.Hour)
	svc.SetLeaderLock(lock, nil)

	svc.runOnce()

	assert.Equal(t, 1, repo.snapshotThawAllCalls())
	// 每轮结束就放锁：领导权每轮重新竞争，不该钉死在一个实例上。
	assert.Empty(t, lock.heldBy(supplierThawLeaderLockKey))
}

func TestSupplierThawStartRunsImmediatelyThenStops(t *testing.T) {
	repo := &supplierCreditRepoStub{}
	// interval 取得足够长，确保观察到的那次调用来自「启动即跑」而不是 ticker。
	svc := NewSupplierThawService(repo, time.Hour)

	svc.Start()
	require.Eventually(t, func() bool { return repo.snapshotThawAllCalls() >= 1 },
		2*time.Second, 5*time.Millisecond, "进程重启后不该等满一个 interval 才补上停机期间到期的额度")

	svc.Stop()
	svc.Stop() // Stop 必须幂等，重复关闭不能 panic
}

func TestSupplierThawGuardsAgainstNil(t *testing.T) {
	var svc *SupplierThawService
	assert.NotPanics(t, func() {
		svc.SetLeaderLock(nil, nil)
		svc.Start()
		svc.Stop()
		svc.runOnce()
	})

	// repo 为 nil 时任务干脆不启动，而不是每轮空转一次选主。
	noRepo := NewSupplierThawService(nil, time.Millisecond)
	assert.NotPanics(t, func() {
		noRepo.Start()
		noRepo.Stop()
	})
}

// ---------------------------------------------------------------------------
// SupplierCreditService
// ---------------------------------------------------------------------------

func TestSupplierCreditServiceGetWalletThawsBeforeReading(t *testing.T) {
	repo := &supplierCreditRepoStub{ensureResult: &SupplierCreditSummary{UserID: 42, AvailableCredit: 8}}
	svc := NewSupplierCreditService(repo, nil)

	got, err := svc.GetWallet(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, float64(8), got.AvailableCredit)
	// 懒解冻：供给者点开页面时看到的划分就是此刻的真实划分，不必等后台那一轮。
	assert.Equal(t, []int64{42}, repo.snapshotThawUserCalls())
	assert.Equal(t, []int64{42}, repo.ensureCalls)
}

func TestSupplierCreditServiceGetWalletIgnoresThawFailure(t *testing.T) {
	repo := &supplierCreditRepoStub{
		thawUserErr:  errors.New("lock timeout"),
		ensureResult: &SupplierCreditSummary{UserID: 42},
	}
	svc := NewSupplierCreditService(repo, nil)

	// 解冻只影响两个数字之间的划分，不影响总额，没有理由让读失败。
	got, err := svc.GetWallet(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), got.UserID)
}

func TestSupplierCreditServiceGetWalletRejectsBadInput(t *testing.T) {
	repo := &supplierCreditRepoStub{}
	svc := NewSupplierCreditService(repo, nil)

	_, err := svc.GetWallet(context.Background(), 0)
	assert.Error(t, err)
	assert.Empty(t, repo.snapshotThawUserCalls())

	var nilSvc *SupplierCreditService
	_, err = nilSvc.GetWallet(context.Background(), 1)
	assert.Error(t, err)

	_, err = NewSupplierCreditService(nil, nil).GetWallet(context.Background(), 1)
	assert.Error(t, err)
}

func TestSupplierCreditServiceListLedgerClampsPaging(t *testing.T) {
	cases := []struct {
		name         string
		page         int
		pageSize     int
		wantPage     int
		wantPageSize int
	}{
		{"zero values get defaults", 0, 0, 1, supplierLedgerDefaultPageSize},
		{"negative values get defaults", -3, -7, 1, supplierLedgerDefaultPageSize},
		{"oversized page size is capped", 2, 10000, 2, supplierLedgerMaxPageSize},
		{"legal values pass through", 3, 50, 3, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplierCreditRepoStub{}
			svc := NewSupplierCreditService(repo, nil)

			_, _, err := svc.ListLedger(context.Background(), SupplierCreditLedgerFilter{
				UserID: 42, Page: tc.page, PageSize: tc.pageSize,
			})
			require.NoError(t, err)
			require.NotNil(t, repo.listFilter)
			assert.Equal(t, tc.wantPage, repo.listFilter.Page)
			assert.Equal(t, tc.wantPageSize, repo.listFilter.PageSize)
		})
	}
}

func TestSupplierCreditServiceListLedgerRejectsBadInput(t *testing.T) {
	repo := &supplierCreditRepoStub{}
	svc := NewSupplierCreditService(repo, nil)

	_, _, err := svc.ListLedger(context.Background(), SupplierCreditLedgerFilter{UserID: 0})
	assert.Error(t, err)
	assert.Nil(t, repo.listFilter)

	var nilSvc *SupplierCreditService
	_, _, err = nilSvc.ListLedger(context.Background(), SupplierCreditLedgerFilter{UserID: 1})
	assert.Error(t, err)
}

func TestSupplierCreditServiceIsEnabledFollowsSettings(t *testing.T) {
	assert.False(t, (*SupplierCreditService)(nil).IsEnabled(context.Background()))
	assert.False(t, NewSupplierCreditService(&supplierCreditRepoStub{}, nil).IsEnabled(context.Background()))

	repo := &supplierSettingRepoStub{value: `{"enabled":true,"share_ratio":0.6,"freeze_hours":48}`}
	svc := NewSupplierCreditService(&supplierCreditRepoStub{}, newSupplierSettingService(t, repo))
	assert.True(t, svc.IsEnabled(context.Background()))
}
