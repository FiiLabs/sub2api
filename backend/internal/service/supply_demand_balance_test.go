//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

type fakeGateReader struct{ s *SupplyDemandGateSettings }

func (f fakeGateReader) GetSupplyDemandGateSettings(_ context.Context) *SupplyDemandGateSettings {
	if f.s == nil {
		return DefaultSupplyDemandGateSettings()
	}
	clone := *f.s
	return &clone
}

type fakeDemandReader struct {
	active int64
	tr     int64
	tk     int64
	err    error
}

func (f fakeDemandReader) GetDashboardStats(_ context.Context) (*usagestats.DashboardStats, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &usagestats.DashboardStats{ActiveUsers: f.active, TotalRequests: f.tr, TotalTokens: f.tk}, nil
}

type fakeSupplyCounter struct {
	counts map[string]int
	err    error
}

func (f fakeSupplyCounter) CountActiveSupplyAccounts(_ context.Context) (map[string]int, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.counts, nil
}

func gate(enabled bool, floor int, maxR, minR float64) *SupplyDemandGateSettings {
	return &SupplyDemandGateSettings{Enabled: enabled, SampleFloor: floor, MaxSuppliersPerUser: maxR, MinSuppliersPerUser: minR}
}

func newBalance(g *SupplyDemandGateSettings, active int64, activeErr error, counts map[string]int, countErr error) *SupplyDemandBalanceService {
	return NewSupplyDemandBalanceService(
		fakeGateReader{s: g},
		fakeDemandReader{active: active, err: activeErr},
		fakeSupplyCounter{counts: counts, err: countErr},
	)
}

func TestAllowNewSupplier(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		g          *SupplyDemandGateSettings
		active     int64
		activeErr  error
		counts     map[string]int
		countErr   error
		platform   string
		wantReject bool
	}{
		{name: "gate disabled → allow", g: gate(false, 10, 1.0, 0.05), active: 100, counts: map[string]int{"anthropic": 999}, platform: "anthropic", wantReject: false},
		{name: "supplier ratio 0 → allow", g: gate(true, 10, 0, 0.05), active: 100, counts: map[string]int{"anthropic": 999}, platform: "anthropic", wantReject: false},
		{name: "below sample floor → allow (cold start)", g: gate(true, 50, 1.0, 0.05), active: 10, counts: map[string]int{"anthropic": 999}, platform: "anthropic", wantReject: false},
		{name: "oversupplied → reject", g: gate(true, 10, 1.0, 0.05), active: 100, counts: map[string]int{"anthropic": 150}, platform: "anthropic", wantReject: true},
		{name: "at threshold (equal) → allow", g: gate(true, 10, 1.0, 0.05), active: 100, counts: map[string]int{"anthropic": 100}, platform: "anthropic", wantReject: false},
		{name: "just over threshold → reject", g: gate(true, 10, 1.0, 0.05), active: 100, counts: map[string]int{"anthropic": 101}, platform: "anthropic", wantReject: true},
		{name: "per-platform isolation: other platform oversupplied, this one fine", g: gate(true, 10, 1.0, 0.05), active: 100, counts: map[string]int{"openai": 999, "anthropic": 10}, platform: "anthropic", wantReject: false},
		{name: "demand read error → allow (fail-open)", g: gate(true, 10, 1.0, 0.05), activeErr: errors.New("db down"), counts: map[string]int{"anthropic": 999}, platform: "anthropic", wantReject: false},
		{name: "supply count error → allow (fail-open)", g: gate(true, 10, 1.0, 0.05), active: 100, countErr: errors.New("db down"), platform: "anthropic", wantReject: false},
		{name: "empty platform defaults anthropic", g: gate(true, 10, 1.0, 0.05), active: 100, counts: map[string]int{"anthropic": 150}, platform: "", wantReject: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBalance(tt.g, tt.active, tt.activeErr, tt.counts, tt.countErr)
			err := b.AllowNewSupplier(ctx, tt.platform)
			gotReject := errors.Is(err, ErrSupplierRejectedOversupplied)
			if gotReject != tt.wantReject {
				t.Fatalf("AllowNewSupplier reject=%v want=%v (err=%v)", gotReject, tt.wantReject, err)
			}
		})
	}
}

func TestAllowNewConsumer(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		g          *SupplyDemandGateSettings
		active     int64
		activeErr  error
		counts     map[string]int
		countErr   error
		wantReject bool
	}{
		{name: "gate disabled → allow", g: gate(false, 10, 1.0, 0.05), active: 1000, counts: map[string]int{"anthropic": 0}, wantReject: false},
		{name: "consumer ratio 0 → allow", g: gate(true, 10, 1.0, 0), active: 1000, counts: map[string]int{"anthropic": 0}, wantReject: false},
		{name: "below sample floor → allow", g: gate(true, 50, 1.0, 0.05), active: 10, counts: nil, wantReject: false},
		{name: "undersupplied → reject", g: gate(true, 10, 1.0, 0.1), active: 100, counts: map[string]int{"anthropic": 5}, wantReject: true},
		{name: "supply sums across platforms", g: gate(true, 10, 1.0, 0.1), active: 100, counts: map[string]int{"anthropic": 6, "openai": 5}, wantReject: false},
		{name: "at threshold (equal) → allow", g: gate(true, 10, 1.0, 0.1), active: 100, counts: map[string]int{"anthropic": 10}, wantReject: false},
		{name: "just under threshold → reject", g: gate(true, 10, 1.0, 0.1), active: 100, counts: map[string]int{"anthropic": 9}, wantReject: true},
		{name: "demand read error → allow (fail-open)", g: gate(true, 10, 1.0, 0.1), activeErr: errors.New("db down"), counts: map[string]int{"anthropic": 0}, wantReject: false},
		{name: "supply count error → allow (fail-open)", g: gate(true, 10, 1.0, 0.1), active: 100, countErr: errors.New("db down"), wantReject: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBalance(tt.g, tt.active, tt.activeErr, tt.counts, tt.countErr)
			err := b.AllowNewConsumer(ctx)
			gotReject := errors.Is(err, ErrRegistrationRejectedUndersupplied)
			if gotReject != tt.wantReject {
				t.Fatalf("AllowNewConsumer reject=%v want=%v (err=%v)", gotReject, tt.wantReject, err)
			}
		})
	}
}

func TestBalanceNilReceiverAllows(t *testing.T) {
	var b *SupplyDemandBalanceService
	if err := b.AllowNewSupplier(context.Background(), "anthropic"); err != nil {
		t.Fatalf("nil receiver AllowNewSupplier should allow, got %v", err)
	}
	if err := b.AllowNewConsumer(context.Background()); err != nil {
		t.Fatalf("nil receiver AllowNewConsumer should allow, got %v", err)
	}
}
