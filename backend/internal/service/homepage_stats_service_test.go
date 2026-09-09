//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
)

type fakeHomepageSettings struct{ s *HomepageStatsSettings }

func (f fakeHomepageSettings) GetHomepageStatsSettings(_ context.Context) *HomepageStatsSettings {
	if f.s == nil {
		return DefaultHomepageStatsSettings()
	}
	clone := *f.s
	return &clone
}

type fakeEarnings struct {
	total float64
	err   error
}

func (f fakeEarnings) SumContributorEarnings(_ context.Context) (float64, error) {
	return f.total, f.err
}

func TestHomepageStatsDisabled(t *testing.T) {
	svc := NewHomepageStatsService(
		fakeHomepageSettings{s: &HomepageStatsSettings{Enabled: false, ActiveUsersOffset: 1000}},
		fakeDemandReader{active: 5, tr: 5},
		fakeSupplyCounter{counts: map[string]int{"anthropic": 3}},
		fakeEarnings{total: 100},
	)
	got := svc.GetPublicStats(context.Background())
	if got.Enabled {
		t.Fatalf("disabled gate should return Enabled=false")
	}
	if got.ActiveUsers != 0 || got.SharedAccounts != 0 {
		t.Fatalf("disabled should not expose numbers, got %+v", got)
	}
}

func TestHomepageStatsRealPlusOffset(t *testing.T) {
	svc := NewHomepageStatsService(
		fakeHomepageSettings{s: &HomepageStatsSettings{
			Enabled:                   true,
			SharedAccountsOffset:      100,
			ActiveUsersOffset:         500,
			TotalRequestsOffset:       9000,
			ContributorEarningsOffset: 250.5,
		}},
		fakeDemandReader{active: 7, tr: 42},
		fakeSupplyCounter{counts: map[string]int{"anthropic": 3, "openai": 2}},
		fakeEarnings{total: 12.5},
	)
	got := svc.GetPublicStats(context.Background())
	if !got.Enabled {
		t.Fatalf("want enabled")
	}
	if got.SharedAccounts != 105 { // 3+2 + 100
		t.Fatalf("SharedAccounts = %d want 105", got.SharedAccounts)
	}
	if got.ActiveUsers != 507 { // 7 + 500
		t.Fatalf("ActiveUsers = %d want 507", got.ActiveUsers)
	}
	if got.TotalRequests != 9042 { // 42 + 9000
		t.Fatalf("TotalRequests = %d want 9042", got.TotalRequests)
	}
	if got.ContributorEarningsUSDT != 263.0 { // 12.5 + 250.5
		t.Fatalf("ContributorEarningsUSDT = %v want 263.0", got.ContributorEarningsUSDT)
	}
	// 供给分解是真实 map（不叠偏移），环形图占比用。
	if got.SupplyByPlatform["anthropic"] != 3 || got.SupplyByPlatform["openai"] != 2 {
		t.Fatalf("SupplyByPlatform = %+v want {anthropic:3, openai:2}", got.SupplyByPlatform)
	}
}

func TestHomepageStatsFailSoft(t *testing.T) {
	// 真实读数全部报错，仍应展示「偏移」部分，不 panic、不整段消失。
	svc := NewHomepageStatsService(
		fakeHomepageSettings{s: &HomepageStatsSettings{
			Enabled:              true,
			SharedAccountsOffset: 100,
			ActiveUsersOffset:    500,
			TotalRequestsOffset:  9000,
		}},
		fakeDemandReader{err: errors.New("db down")},
		fakeSupplyCounter{err: errors.New("db down")},
		fakeEarnings{err: errors.New("db down")},
	)
	got := svc.GetPublicStats(context.Background())
	if !got.Enabled || got.SharedAccounts != 100 || got.ActiveUsers != 500 || got.TotalRequests != 9000 {
		t.Fatalf("fail-soft should show offsets only, got %+v", got)
	}
}
