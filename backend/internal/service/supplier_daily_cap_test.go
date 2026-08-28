//go:build unit

// APEXONE-EXT: 每日共享上限写路径的单测。
//
// 这里守的是三件事：别人的号改不了、脏值写不进去、只改一项时不会把另一项冲掉。
// 第三件最容易在重构里丢——把 UpdateExtra 换成整体替换、或者把两个指针换成值类型，
// 都会让「我只想调 token 上限」顺手把金额上限清零，而供给者不会立刻发现。
package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dailyCapAccountStoreStub struct {
	supplierAccountStore
	account      *Account
	lastUpdates  map[string]any
	updateCalled int
}

func (s *dailyCapAccountStoreStub) GetByID(_ context.Context, id int64) (*Account, error) {
	if s.account == nil || s.account.ID != id {
		return nil, errors.New("not found")
	}
	return s.account, nil
}

func (s *dailyCapAccountStoreStub) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	s.updateCalled++
	s.lastUpdates = updates
	// 模拟 JSONB 合并语义：只覆盖传进来的键，其余原样保留。
	if s.account != nil {
		if s.account.Extra == nil {
			s.account.Extra = map[string]any{}
		}
		for k, v := range updates {
			s.account.Extra[k] = v
		}
	}
	return nil
}

type dailyCapOwnerRepoStub struct {
	SupplierOnboardingRepository
	owner int64
}

func (s *dailyCapOwnerRepoStub) GetAccountOwner(_ context.Context, _ int64) (int64, error) {
	return s.owner, nil
}

func newDailyCapService(owner int64, account *Account) (*SupplierOnboardingService, *dailyCapAccountStoreStub) {
	store := &dailyCapAccountStoreStub{account: account}
	svc := &SupplierOnboardingService{
		repo:        &dailyCapOwnerRepoStub{owner: owner},
		accountRepo: store,
	}
	return svc, store
}

// 别人的号、以及平台自营的号（owner == 0），一律「找不到」——不能区分，
// 「这个号存在但不归你」本身就是不该泄漏的信息。
func TestSetDailyCapRejectsForeignAccounts(t *testing.T) {
	for _, owner := range []int64{0, 999} {
		svc, store := newDailyCapService(owner, &Account{ID: 1})
		_, err := svc.SetDailyCap(context.Background(), 42, 1, floatPtr(10), nil)
		assert.ErrorIs(t, err, ErrSupplierAccountNotFound)
		assert.Zero(t, store.updateCalled, "归属校验没过就不该写库")
	}
}

func TestSetDailyCapRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name   string
		cost   *float64
		tokens *int64
	}{
		{"金额为负", floatPtr(-1), nil},
		{"金额 NaN", floatPtr(math.NaN()), nil},
		{"金额 +Inf", floatPtr(math.Inf(1)), nil},
		{"金额超上界", floatPtr(SupplyDailyCostLimitMaxUSD + 1), nil},
		{"token 为负", nil, int64Ptr(-1)},
		{"token 超上界", nil, int64Ptr(SupplyDailyTokenLimitMax + 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := newDailyCapService(42, &Account{ID: 1})
			_, err := svc.SetDailyCap(context.Background(), 42, 1, tc.cost, tc.tokens)
			require.Error(t, err)
			assert.Zero(t, store.updateCalled, "值不合法时一个字节都不该写进去")
		})
	}
}

// 只传一项时，落到 UpdateExtra 的 map 里必须**只有那一个键**。
//
// 多写一个键就等于把另一项也覆盖掉。这条在「把两个指针改成值类型」那种看起来
// 无害的重构下会立刻变红，而线上的现象只是某个供给者的另一个上限莫名归零。
func TestSetDailyCapPartialUpdateTouchesOnlyGivenField(t *testing.T) {
	svc, store := newDailyCapService(42, &Account{
		ID:    1,
		Extra: map[string]any{SupplyDailyCostLimitExtraKey: 20.0, SupplyDailyTokenLimitExtraKey: int64(500)},
	})

	_, err := svc.SetDailyCap(context.Background(), 42, 1, nil, int64Ptr(999))
	require.NoError(t, err)
	require.Len(t, store.lastUpdates, 1)
	assert.Contains(t, store.lastUpdates, SupplyDailyTokenLimitExtraKey)
	assert.NotContains(t, store.lastUpdates, SupplyDailyCostLimitExtraKey,
		"只改 token 上限时不该把金额上限一起写")
	// 金额上限仍在。
	assert.Equal(t, 20.0, store.account.GetSupplyDailyCostLimit())
}

// 0 是「取消这一项的上限」，是一个必须能写进去的合法值。
func TestSetDailyCapZeroClearsLimit(t *testing.T) {
	svc, store := newDailyCapService(42, &Account{
		ID:    1,
		Extra: map[string]any{SupplyDailyCostLimitExtraKey: 20.0},
	})

	view, err := svc.SetDailyCap(context.Background(), 42, 1, floatPtr(0), nil)
	require.NoError(t, err)
	require.NotNil(t, view)
	assert.Equal(t, 1, store.updateCalled)
	assert.Zero(t, store.account.GetSupplyDailyCostLimit())
	assert.Zero(t, view.DailyCostLimitUSD)
}

// 金额截到分：存 0.006 没有意义，且会让界面显示的数和实际生效的数对不上。
func TestSetDailyCapRoundsCostToCents(t *testing.T) {
	svc, store := newDailyCapService(42, &Account{ID: 1})
	_, err := svc.SetDailyCap(context.Background(), 42, 1, floatPtr(19.999), nil)
	require.NoError(t, err)
	assert.Equal(t, 20.0, store.lastUpdates[SupplyDailyCostLimitExtraKey])
}

// ---------------------------------------------------------------------------
// 视图
// ---------------------------------------------------------------------------

type dailyCapUsageReaderStub struct {
	stats map[int64]*usagestats.AccountStats
	err   error
	calls int
	ids   []int64
}

func (s *dailyCapUsageReaderStub) GetAccountWindowStatsBatch(_ context.Context, ids []int64, _ time.Time) (map[int64]*usagestats.AccountStats, error) {
	s.calls++
	s.ids = ids
	return s.stats, s.err
}

// 触顶判定必须与调度闸走同一个函数，否则界面会说「还能接单」而实际早就不接了。
func TestApplyDailyCapUsageMarksReached(t *testing.T) {
	acc := &Account{ID: 1, Extra: map[string]any{SupplyDailyCostLimitExtraKey: 10.0}}
	reader := &dailyCapUsageReaderStub{stats: map[int64]*usagestats.AccountStats{
		1: {StandardCost: 10, Tokens: 123},
	}}
	svc := &SupplierOnboardingService{dailyUsageReader: reader}

	views := []SupplierAccountView{{ID: 1, DailyCostLimitUSD: 10}}
	svc.applyDailyCapUsage(context.Background(), views, map[int64]*Account{1: acc})

	assert.Equal(t, 10.0, views[0].DailyCostUsedUSD)
	assert.Equal(t, int64(123), views[0].DailyTokensUsed)
	assert.True(t, views[0].DailyCapReached, "已用 == 上限，应判为触顶（边界是 >=）")
}

// 没有任何号设过上限时一次查询都不发；查询失败时保持零值、不影响上限显示。
func TestApplyDailyCapUsageShortCircuitsAndFailsSoft(t *testing.T) {
	reader := &dailyCapUsageReaderStub{}
	svc := &SupplierOnboardingService{dailyUsageReader: reader}
	svc.applyDailyCapUsage(context.Background(), []SupplierAccountView{{ID: 1}}, nil)
	assert.Zero(t, reader.calls, "没设上限的号不该触发用量查询")

	failing := &dailyCapUsageReaderStub{err: errors.New("db down")}
	svc2 := &SupplierOnboardingService{dailyUsageReader: failing}
	views := []SupplierAccountView{{ID: 1, DailyCostLimitUSD: 10}}
	svc2.applyDailyCapUsage(context.Background(), views, nil)
	assert.Zero(t, views[0].DailyCostUsedUSD)
	assert.False(t, views[0].DailyCapReached, "查不到用量时不该武断判成触顶")
	assert.Equal(t, 10.0, views[0].DailyCostLimitUSD, "上限本身照常显示")
}
