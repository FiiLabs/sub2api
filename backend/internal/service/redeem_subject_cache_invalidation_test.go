//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// trackingSubjectInvalidateCache 实现 BillingCache + BillingSubjectCache，
// 记录 InvalidateSubjectBalance 和 InvalidateUserBalance 调用次数及参数。
type trackingSubjectInvalidateCache struct {
	BillingCache // 嵌入接口；未覆写方法不应在本测试中被调用

	invalidateSubjectCount atomic.Int64
	lastSubjectID          atomic.Int64

	invalidateUserCount atomic.Int64
	lastUserID         atomic.Int64
}

// BillingSubjectCache 方法 — 使 BillingCacheService.InvalidateSubjectBalance
// 走到专用键路径而非回退到 InvalidateUserBalance。
func (t *trackingSubjectInvalidateCache) GetSubjectBalance(_ context.Context, _ int64) (float64, error) {
	return 0, nil
}
func (t *trackingSubjectInvalidateCache) SetSubjectBalance(_ context.Context, _ int64, _ float64) error {
	return nil
}
func (t *trackingSubjectInvalidateCache) DeductSubjectBalance(_ context.Context, _ int64, _ float64) error {
	return nil
}
func (t *trackingSubjectInvalidateCache) InvalidateSubjectBalance(_ context.Context, subjectID int64) error {
	t.lastSubjectID.Store(subjectID)
	t.invalidateSubjectCount.Add(1)
	return nil
}
func (t *trackingSubjectInvalidateCache) GetSubjectPlatformQuotaCache(_ context.Context, _ int64, _ string) (*UserPlatformQuotaCacheEntry, bool, error) {
	return nil, false, nil
}
func (t *trackingSubjectInvalidateCache) SetSubjectPlatformQuotaCache(_ context.Context, _ int64, _ string, _ *UserPlatformQuotaCacheEntry, _ time.Duration) error {
	return nil
}
func (t *trackingSubjectInvalidateCache) IncrSubjectPlatformQuotaUsageCache(_ context.Context, _ int64, _ string, _ float64, _ time.Duration, _ bool) error {
	return nil
}

// BillingCache 方法（仅覆写测试路径需要的方法）
func (t *trackingSubjectInvalidateCache) InvalidateUserBalance(_ context.Context, userID int64) error {
	t.lastUserID.Store(userID)
	t.invalidateUserCount.Add(1)
	return nil
}

// newTrackingBillingCacheService 创建使用 trackingSubjectInvalidateCache 的 BillingCacheService。
// 直接构造结构体以避免启动 worker goroutine（测试无需缓存写入池）。
func newTrackingBillingCacheService(mock *trackingSubjectInvalidateCache) *BillingCacheService {
	return &BillingCacheService{cache: mock}
}

// TestInvalidateRedeemCaches_TeamRecharge_InvalidatesSubjectBalance 验证
// 团队兑换（billingSubjectID > 0）触发 InvalidateSubjectBalance，且
// InvalidateUserBalance 仍被调用（回归防护）。
func TestInvalidateRedeemCaches_TeamRecharge_InvalidatesSubjectBalance(t *testing.T) {
	t.Parallel()

	mock := &trackingSubjectInvalidateCache{}
	billingCacheSvc := newTrackingBillingCacheService(mock)

	svc := &RedeemService{billingCacheService: billingCacheSvc}

	const userID int64 = 42
	const subjectID int64 = 900

	svc.invalidateRedeemCaches(context.Background(), userID, subjectID, &RedeemCode{Type: RedeemTypeBalance})

	// InvalidateSubjectBalance 在 goroutine 中异步调用：用 Eventually 等待。
	require.Eventually(t, func() bool {
		return mock.invalidateSubjectCount.Load() == 1
	}, 2*time.Second, 10*time.Millisecond, "应异步调用 InvalidateSubjectBalance")

	require.Equal(t, subjectID, mock.lastSubjectID.Load(), "InvalidateSubjectBalance 应使用 billingSubjectID=900")

	// 回归防护：InvalidateUserBalance 仍须被调用。
	require.Eventually(t, func() bool {
		return mock.invalidateUserCount.Load() == 1
	}, 2*time.Second, 10*time.Millisecond, "应异步调用 InvalidateUserBalance（回归防护）")

	require.Equal(t, userID, mock.lastUserID.Load(), "InvalidateUserBalance 应使用 userID=42")
}

// TestInvalidateRedeemCaches_PersonalRecharge_DoesNotInvalidateSubjectBalance 验证
// 个人兑换（billingSubjectID == 0）不触发 InvalidateSubjectBalance，只失效用户余额。
func TestInvalidateRedeemCaches_PersonalRecharge_DoesNotInvalidateSubjectBalance(t *testing.T) {
	t.Parallel()

	mock := &trackingSubjectInvalidateCache{}
	billingCacheSvc := newTrackingBillingCacheService(mock)

	svc := &RedeemService{billingCacheService: billingCacheSvc}

	const userID int64 = 7
	const subjectID int64 = 0 // 个人兑换，无独立计费主体

	svc.invalidateRedeemCaches(context.Background(), userID, subjectID, &RedeemCode{Type: RedeemTypeBalance})

	// InvalidateUserBalance 应被调用。
	require.Eventually(t, func() bool {
		return mock.invalidateUserCount.Load() == 1
	}, 2*time.Second, 10*time.Millisecond, "个人兑换应调用 InvalidateUserBalance")

	// InvalidateSubjectBalance 不应被调用（短暂等待后断言）。
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, int64(0), mock.invalidateSubjectCount.Load(), "个人兑换不应调用 InvalidateSubjectBalance")
}
