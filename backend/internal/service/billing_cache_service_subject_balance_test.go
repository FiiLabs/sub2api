//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// fakeSubjectBalanceCache 实现 BillingCache + BillingSubjectCache 的最小子集。
type fakeSubjectBalanceCache struct {
	BillingCache
	getErr   error   // GetSubjectBalance 返回的错误（模拟 MISS）
	cached   float64 // GetSubjectBalance 命中时返回的值
	setCalls int
	lastSet  float64
}

func (f *fakeSubjectBalanceCache) GetSubjectBalance(_ context.Context, _ int64) (float64, error) {
	if f.getErr != nil {
		return 0, f.getErr
	}
	return f.cached, nil
}
func (f *fakeSubjectBalanceCache) SetSubjectBalance(_ context.Context, _ int64, b float64) error {
	f.setCalls++
	f.lastSet = b
	return nil
}
func (f *fakeSubjectBalanceCache) DeductSubjectBalance(_ context.Context, _ int64, _ float64) error {
	return nil
}
func (f *fakeSubjectBalanceCache) InvalidateSubjectBalance(_ context.Context, _ int64) error {
	return nil
}
func (f *fakeSubjectBalanceCache) GetSubjectPlatformQuotaCache(_ context.Context, _ int64, _ string) (*UserPlatformQuotaCacheEntry, bool, error) {
	return nil, false, nil
}
func (f *fakeSubjectBalanceCache) SetSubjectPlatformQuotaCache(_ context.Context, _ int64, _ string, _ *UserPlatformQuotaCacheEntry, _ time.Duration) error {
	return nil
}
func (f *fakeSubjectBalanceCache) IncrSubjectPlatformQuotaUsageCache(_ context.Context, _ int64, _ string, _ float64, _ time.Duration, _ bool) error {
	return nil
}

// fakeSubjectRepo 实现 BillingSubjectRepository 最小子集（仅 GetByID）。
type fakeSubjectRepo struct {
	byID map[int64]*BillingSubject
}

func (f *fakeSubjectRepo) GetByID(_ context.Context, id int64) (*BillingSubject, error) {
	if s, ok := f.byID[id]; ok {
		return s, nil
	}
	return nil, ErrUserNotFound
}
func (f *fakeSubjectRepo) GetPersonalByUserID(_ context.Context, userID int64) (*BillingSubject, error) {
	for _, s := range f.byID {
		if s.UserID != nil && *s.UserID == userID {
			return s, nil
		}
	}
	return nil, ErrUserNotFound
}
func (f *fakeSubjectRepo) EnsurePersonalForUser(_ context.Context, _ *User) (*BillingSubject, error) {
	return nil, nil
}
func (f *fakeSubjectRepo) CreateTeamSubject(_ context.Context, _ int64, _ BillingSubject) (*BillingSubject, error) {
	return nil, nil
}
func (f *fakeSubjectRepo) UpdateBalance(_ context.Context, _ int64, _ float64) error { return nil }
func (f *fakeSubjectRepo) DeductBalance(_ context.Context, _ int64, _ float64) error { return nil }

func newSubjectBalanceService(cache BillingCache, repo BillingSubjectRepository) *BillingCacheService {
	cfg := &config.Config{}
	cfg.Billing.QuotaSubjectScoped = true
	return &BillingCacheService{
		cache:              cache,
		cfg:                cfg,
		billingSubjectRepo: repo,
	}
}

func TestGetSubjectBalance_CacheHit(t *testing.T) {
	cache := &fakeSubjectBalanceCache{cached: 42}
	s := newSubjectBalanceService(cache, &fakeSubjectRepo{})
	got, err := s.GetSubjectBalance(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != 42 {
		t.Errorf("balance = %v, want 42", got)
	}
}

func TestGetSubjectBalance_CacheMiss_FallsBackToDB(t *testing.T) {
	bal := 88.5
	cache := &fakeSubjectBalanceCache{getErr: errors.New("redis miss")}
	repo := &fakeSubjectRepo{byID: map[int64]*BillingSubject{7: {ID: 7, Balance: bal}}}
	s := newSubjectBalanceService(cache, repo)
	got, err := s.GetSubjectBalance(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != bal {
		t.Errorf("balance = %v, want %v", got, bal)
	}
	if cache.setCalls != 1 || cache.lastSet != bal {
		t.Errorf("expected cache repopulation with %v, got setCalls=%d lastSet=%v", bal, cache.setCalls, cache.lastSet)
	}
}

func TestCheckSubjectBalanceEligibility_AllowsWhenFunded(t *testing.T) {
	cache := &fakeSubjectBalanceCache{cached: 10}
	s := newSubjectBalanceService(cache, &fakeSubjectRepo{})
	if err := s.checkSubjectBalanceEligibility(context.Background(), 7, nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCheckSubjectBalanceEligibility_BlocksWhenZero(t *testing.T) {
	cache := &fakeSubjectBalanceCache{cached: 0}
	s := newSubjectBalanceService(cache, &fakeSubjectRepo{})
	err := s.checkSubjectBalanceEligibility(context.Background(), 7, nil)
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Errorf("expected ErrInsufficientBalance, got %v", err)
	}
}
