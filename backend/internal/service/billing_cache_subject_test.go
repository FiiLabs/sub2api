package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type billingCacheSubjectStub struct {
	BillingCache  // platform-quota: 嵌入接口自动满足新增方法（未覆写者调用即 panic，本测试不触发）
	balances      map[int64]float64
	lastSubjectID int64
}

func (b *billingCacheSubjectStub) GetSubjectBalance(_ context.Context, subjectID int64) (float64, error) {
	b.lastSubjectID = subjectID
	return b.balances[subjectID], nil
}

func (b *billingCacheSubjectStub) SetSubjectBalance(_ context.Context, subjectID int64, balance float64) error {
	b.balances[subjectID] = balance
	return nil
}

func (b *billingCacheSubjectStub) DeductSubjectBalance(_ context.Context, subjectID int64, amount float64) error {
	b.balances[subjectID] -= amount
	return nil
}

func (b *billingCacheSubjectStub) InvalidateSubjectBalance(_ context.Context, subjectID int64) error {
	delete(b.balances, subjectID)
	return nil
}

func (b *billingCacheSubjectStub) GetUserBalance(_ context.Context, userID int64) (float64, error) {
	return b.balances[userID], nil
}

func (b *billingCacheSubjectStub) SetUserBalance(_ context.Context, userID int64, balance float64) error {
	b.balances[userID] = balance
	return nil
}

func (b *billingCacheSubjectStub) DeductUserBalance(_ context.Context, userID int64, amount float64) error {
	b.balances[userID] -= amount
	return nil
}

func (b *billingCacheSubjectStub) InvalidateUserBalance(_ context.Context, userID int64) error {
	delete(b.balances, userID)
	return nil
}

func (b *billingCacheSubjectStub) GetSubscriptionCache(context.Context, int64, int64) (*SubscriptionCacheData, error) {
	return nil, nil
}

func (b *billingCacheSubjectStub) SetSubscriptionCache(context.Context, int64, int64, *SubscriptionCacheData) error {
	return nil
}

func (b *billingCacheSubjectStub) UpdateSubscriptionUsage(context.Context, int64, int64, float64) error {
	return nil
}

func (b *billingCacheSubjectStub) InvalidateSubscriptionCache(context.Context, int64, int64) error {
	return nil
}

func (b *billingCacheSubjectStub) GetAPIKeyRateLimit(context.Context, int64) (*APIKeyRateLimitCacheData, error) {
	return nil, nil
}

func (b *billingCacheSubjectStub) SetAPIKeyRateLimit(context.Context, int64, *APIKeyRateLimitCacheData) error {
	return nil
}

func (b *billingCacheSubjectStub) UpdateAPIKeyRateLimitUsage(context.Context, int64, float64) error {
	return nil
}

func (b *billingCacheSubjectStub) InvalidateAPIKeyRateLimit(context.Context, int64) error {
	return nil
}

func (b *billingCacheSubjectStub) GetUserPlatformQuotaCache(context.Context, int64, string) (*UserPlatformQuotaCacheEntry, bool, error) {
	return nil, false, nil
}

func (b *billingCacheSubjectStub) SetUserPlatformQuotaCache(context.Context, int64, string, *UserPlatformQuotaCacheEntry, time.Duration) error {
	return nil
}

func (b *billingCacheSubjectStub) DeleteUserPlatformQuotaCache(context.Context, int64, string) error {
	return nil
}

func (b *billingCacheSubjectStub) IncrUserPlatformQuotaUsageCache(context.Context, int64, string, float64, time.Duration, bool) error {
	return nil
}

func (b *billingCacheSubjectStub) PopDirtyUserPlatformQuotaKeys(context.Context, int) ([]UserPlatformQuotaKey, error) {
	return nil, nil
}

func (b *billingCacheSubjectStub) ReaddDirtyUserPlatformQuotaKeys(context.Context, []UserPlatformQuotaKey) error {
	return nil
}

func (b *billingCacheSubjectStub) BatchGetUserPlatformQuotaCache(context.Context, []UserPlatformQuotaKey) ([]*UserPlatformQuotaCacheEntry, error) {
	return nil, nil
}

func (b *billingCacheSubjectStub) GetSubjectPlatformQuotaCache(context.Context, int64, string) (*UserPlatformQuotaCacheEntry, bool, error) {
	return nil, false, nil
}

func (b *billingCacheSubjectStub) SetSubjectPlatformQuotaCache(context.Context, int64, string, *UserPlatformQuotaCacheEntry, time.Duration) error {
	return nil
}

func (b *billingCacheSubjectStub) IncrSubjectPlatformQuotaUsageCache(context.Context, int64, string, float64, time.Duration, bool) error {
	return nil
}

func TestBillingCacheUsesSubjectBalanceWhenSubjectIDProvided(t *testing.T) {
	cache := &billingCacheSubjectStub{balances: map[int64]float64{900: 12.5}}
	svc := &BillingCacheService{cache: cache}

	balance, err := svc.GetSubjectBalance(context.Background(), 900)
	require.NoError(t, err)
	require.Equal(t, 12.5, balance)
	require.Equal(t, int64(900), cache.lastSubjectID)
}
