//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ── platform-quota (3/4): checkSubjectPlatformQuotaEligibility 核心逻辑 ──
// 与 checkUserPlatformQuotaEligibility 同款（subject 维度），用相同 fake 验证拦截判定一致。
// fakeFullCache 未实现 BillingSubjectCache，故 subject 缓存包装回退到 user 缓存方法；
// DB 回源走 fakeQuotaRepo.GetBySubjectPlatform（返回 f.rec）。

func TestCheckSubjectPlatformQuotaEligibility_AllowsWhenUnderLimit(t *testing.T) {
	daily := 10.0
	repo := &fakeQuotaRepo{rec: &UserPlatformQuotaRecord{
		BillingSubjectID: 7, Platform: "anthropic", DailyLimitUSD: &daily,
	}}
	cache := &fakeFullCache{entry: &UserPlatformQuotaCacheEntry{
		DailyUsageUSD:    4.5,
		DailyLimitUSD:    &daily,
		DailyWindowStart: currentDayStart(),
		SchemaVersion:    UserPlatformQuotaCacheSchemaV1,
	}}
	s := newServiceForPreflight(t, repo, cache)
	if err := s.checkSubjectPlatformQuotaEligibility(context.Background(), 7, "anthropic", nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCheckSubjectPlatformQuotaEligibility_DailyExhausted(t *testing.T) {
	daily := 5.0
	repo := &fakeQuotaRepo{rec: &UserPlatformQuotaRecord{
		BillingSubjectID: 7, Platform: "anthropic", DailyLimitUSD: &daily,
	}}
	cache := &fakeFullCache{entry: &UserPlatformQuotaCacheEntry{
		DailyUsageUSD:    5.0,
		DailyLimitUSD:    &daily,
		DailyWindowStart: currentDayStart(),
		SchemaVersion:    UserPlatformQuotaCacheSchemaV1,
	}}
	s := newServiceForPreflight(t, repo, cache)
	err := s.checkSubjectPlatformQuotaEligibility(context.Background(), 7, "anthropic", nil)
	if !errors.Is(err, ErrUserPlatformDailyQuotaExhausted) {
		t.Errorf("expected ErrUserPlatformDailyQuotaExhausted, got %v", err)
	}
}

// DB 回源 + 无行 → sentinel/fail-open（allow）。
func TestCheckSubjectPlatformQuotaEligibility_NoRecordMeansUnlimited(t *testing.T) {
	repo := &fakeQuotaRepo{rec: nil}
	cache := &fakeFullCache{}
	s := newServiceForPreflight(t, repo, cache)
	if err := s.checkSubjectPlatformQuotaEligibility(context.Background(), 7, "anthropic", nil); err != nil {
		t.Errorf("no record = unlimited, got %v", err)
	}
}

func TestHasSubjectPlatformQuotaLimit(t *testing.T) {
	daily := 5.0
	tests := []struct {
		name  string
		setup func() *BillingCacheService
		want  bool
	}{
		{
			name: "has_limit",
			setup: func() *BillingCacheService {
				return newServiceForPreflight(t, &fakeQuotaRepo{}, &fakeFullCache{entry: &UserPlatformQuotaCacheEntry{DailyLimitUSD: &daily}})
			},
			want: true,
		},
		{
			name: "sentinel_no_limit",
			setup: func() *BillingCacheService {
				return newServiceForPreflight(t, &fakeQuotaRepo{}, &fakeFullCache{entry: &UserPlatformQuotaCacheEntry{}})
			},
			want: false,
		},
		{
			name: "cache_miss",
			setup: func() *BillingCacheService {
				return newServiceForPreflight(t, &fakeQuotaRepo{}, &fakeFullCache{})
			},
			want: true, // fail-safe
		},
		{
			name: "simple_mode",
			setup: func() *BillingCacheService {
				svc := newServiceForPreflight(t, &fakeQuotaRepo{}, &fakeFullCache{entry: &UserPlatformQuotaCacheEntry{DailyLimitUSD: &daily}})
				svc.cfg.RunMode = config.RunModeSimple
				return svc
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.setup().HasSubjectPlatformQuotaLimit(context.Background(), 7, "anthropic"); got != tt.want {
				t.Errorf("HasSubjectPlatformQuotaLimit() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── platform-quota (3/4): CheckBillingEligibility 灰度路由 ──
// 验证 QuotaSubjectScoped 开关把读路径在 subject / user 之间切换，
// 并在 subject 未解析时 fail-safe 兜底到 user 路径（不放行越限）。

// routeRecordingRepo 记录 GetByUserPlatform / GetBySubjectPlatform 各自是否被调用，
// 用以断言 preflight 走的是哪条路径。两者均返回 (nil,nil) → fail-open，便于聚焦"路由"。
type routeRecordingRepo struct {
	fakeQuotaRepo
	gotUser    bool
	gotSubject bool
}

func (r *routeRecordingRepo) GetByUserPlatform(_ context.Context, _ int64, _ string) (*UserPlatformQuotaRecord, error) {
	r.gotUser = true
	return nil, nil
}

func (r *routeRecordingRepo) GetBySubjectPlatform(_ context.Context, _ int64, _ string) (*UserPlatformQuotaRecord, error) {
	r.gotSubject = true
	return nil, nil
}

// balanceQuotaCache 让余额检查通过、平台 quota 缓存 MISS（强制走 DB → 命中 repo 记录）。
type balanceQuotaCache struct {
	BillingCache
}

func (balanceQuotaCache) GetUserBalance(_ context.Context, _ int64) (float64, error) { return 100, nil }
func (balanceQuotaCache) GetUserPlatformQuotaCache(_ context.Context, _ int64, _ string) (*UserPlatformQuotaCacheEntry, bool, error) {
	return nil, false, nil
}
func (balanceQuotaCache) SetUserPlatformQuotaCache(_ context.Context, _ int64, _ string, _ *UserPlatformQuotaCacheEntry, _ time.Duration) error {
	return nil
}

func newRoutingService(repo UserPlatformQuotaRepository, scoped bool) *BillingCacheService {
	cfg := &config.Config{}
	cfg.Billing.UserPlatformQuotaCacheTTLSeconds = 60
	cfg.Billing.QuotaSubjectScoped = scoped
	return &BillingCacheService{
		cache:                 &balanceQuotaCache{},
		cfg:                   cfg,
		userPlatformQuotaRepo: repo,
	}
}

func TestCheckBillingEligibility_RoutesToUserWhenFlagOff(t *testing.T) {
	repo := &routeRecordingRepo{}
	s := newRoutingService(repo, false)
	apiKey := &APIKey{BillingSubjectID: 77}
	_ = s.CheckBillingEligibility(context.Background(), &User{ID: 11}, apiKey, nil, nil, "anthropic")
	if !repo.gotUser || repo.gotSubject {
		t.Errorf("flag off 应走 user 路径: gotUser=%v gotSubject=%v", repo.gotUser, repo.gotSubject)
	}
}

func TestCheckBillingEligibility_RoutesToSubjectWhenFlagOn(t *testing.T) {
	repo := &routeRecordingRepo{}
	s := newRoutingService(repo, true)
	apiKey := &APIKey{BillingSubjectID: 77}
	_ = s.CheckBillingEligibility(context.Background(), &User{ID: 11}, apiKey, nil, nil, "anthropic")
	if !repo.gotSubject || repo.gotUser {
		t.Errorf("flag on + subject 应走 subject 路径: gotUser=%v gotSubject=%v", repo.gotUser, repo.gotSubject)
	}
}

func TestCheckBillingEligibility_FailSafeToUserWhenSubjectUnresolved(t *testing.T) {
	// flag on 但 apiKey.BillingSubjectID == 0 → 兜底 user 路径
	repo := &routeRecordingRepo{}
	s := newRoutingService(repo, true)
	_ = s.CheckBillingEligibility(context.Background(), &User{ID: 11}, &APIKey{BillingSubjectID: 0}, nil, nil, "anthropic")
	if !repo.gotUser || repo.gotSubject {
		t.Errorf("subjectID=0 应 fail-safe user 路径: gotUser=%v gotSubject=%v", repo.gotUser, repo.gotSubject)
	}

	// flag on 但 apiKey == nil → 兜底 user 路径
	repo2 := &routeRecordingRepo{}
	s2 := newRoutingService(repo2, true)
	_ = s2.CheckBillingEligibility(context.Background(), &User{ID: 11}, nil, nil, nil, "anthropic")
	if !repo2.gotUser || repo2.gotSubject {
		t.Errorf("apiKey=nil 应 fail-safe user 路径: gotUser=%v gotSubject=%v", repo2.gotUser, repo2.gotSubject)
	}
}

// platform-quota: IncrementSubjectPlatformQuotaUsage 入参守卫（subjectID<=0 / 空 platform / cost<=0 → noop）。
func TestIncrementSubjectPlatformQuotaUsage_Guards(t *testing.T) {
	fake := &fakeIncrCache{}
	cfg := &config.Config{}
	cfg.Billing.UserPlatformQuotaCacheTTLSeconds = 60
	s := &BillingCacheService{cache: fake, cfg: cfg}

	s.IncrementSubjectPlatformQuotaUsage(0, "anthropic", 1.0) // subjectID<=0 → noop
	s.IncrementSubjectPlatformQuotaUsage(7, "", 1.0)          // 空 platform → noop
	s.IncrementSubjectPlatformQuotaUsage(7, "anthropic", 0)   // cost<=0 → noop

	if len(fake.calls) != 0 {
		t.Errorf("expected 0 incr calls (all guarded), got %d", len(fake.calls))
	}
}
