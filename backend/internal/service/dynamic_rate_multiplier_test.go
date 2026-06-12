package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveDynamicRateMultiplier(t *testing.T) {
	min := 0.5
	max := 0.6

	tests := []struct {
		name              string
		group             *Group
		fixedMultiplier   float64
		accountMultiplier float64
		want              float64
	}{
		{
			name:              "disabled uses fixed multiplier",
			group:             &Group{DynamicRateEnabled: false, DynamicRateMode: DynamicRateModeAccountPlusMargin, DynamicRateMargin: 0.1},
			fixedMultiplier:   0.8,
			accountMultiplier: 0.4,
			want:              0.8,
		},
		{
			name:              "account plus margin",
			group:             &Group{DynamicRateEnabled: true, DynamicRateMode: DynamicRateModeAccountPlusMargin, DynamicRateMargin: 0.1},
			fixedMultiplier:   0.8,
			accountMultiplier: 0.4,
			want:              0.5,
		},
		{
			name:              "account markup",
			group:             &Group{DynamicRateEnabled: true, DynamicRateMode: DynamicRateModeAccountMarkup, DynamicRateMargin: 0.25},
			fixedMultiplier:   0.8,
			accountMultiplier: 0.4,
			want:              0.5,
		},
		{
			name:              "min bound",
			group:             &Group{DynamicRateEnabled: true, DynamicRateMode: DynamicRateModeAccountPlusMargin, DynamicRateMinMultiplier: &min},
			fixedMultiplier:   0.8,
			accountMultiplier: 0.4,
			want:              0.5,
		},
		{
			name:              "max bound",
			group:             &Group{DynamicRateEnabled: true, DynamicRateMode: DynamicRateModeAccountPlusMargin, DynamicRateMargin: 0.4, DynamicRateMaxMultiplier: &max},
			fixedMultiplier:   0.8,
			accountMultiplier: 0.4,
			want:              0.6,
		},
		{
			name:              "invalid enabled mode falls back to fixed multiplier",
			group:             &Group{DynamicRateEnabled: true, DynamicRateMode: "bad", DynamicRateMargin: 0.1},
			fixedMultiplier:   0.8,
			accountMultiplier: 0.4,
			want:              0.8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveDynamicRateMultiplier(tt.group, tt.fixedMultiplier, tt.accountMultiplier)
			require.InDelta(t, tt.want, got, 1e-12)
		})
	}
}

func TestResolvePrecheckRateMultiplier(t *testing.T) {
	max := 0.6

	tests := []struct {
		name            string
		group           *Group
		fixedMultiplier float64
		want            float64
	}{
		{
			name:            "disabled uses fixed multiplier",
			group:           &Group{DynamicRateEnabled: false, DynamicRateMaxMultiplier: &max},
			fixedMultiplier: 0.8,
			want:            0.8,
		},
		{
			name:            "dynamic enabled uses configured max multiplier",
			group:           &Group{DynamicRateEnabled: true, DynamicRateMode: DynamicRateModeAccountPlusMargin, DynamicRateMaxMultiplier: &max},
			fixedMultiplier: 0.8,
			want:            0.6,
		},
		{
			name:            "dynamic enabled without max falls back to fixed multiplier",
			group:           &Group{DynamicRateEnabled: true, DynamicRateMode: DynamicRateModeAccountPlusMargin},
			fixedMultiplier: 0.8,
			want:            0.8,
		},
		{
			name:            "negative values are clamped to zero",
			group:           &Group{DynamicRateEnabled: true, DynamicRateMode: DynamicRateModeAccountPlusMargin},
			fixedMultiplier: -0.8,
			want:            0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePrecheckRateMultiplier(tt.group, tt.fixedMultiplier)
			require.InDelta(t, tt.want, got, 1e-12)
		})
	}
}

func TestPrecheckMinimumCostUsesDynamicMax(t *testing.T) {
	max := 0.6
	group := &Group{
		RateMultiplier:           2,
		DynamicRateEnabled:       true,
		DynamicRateMode:          DynamicRateModeAccountPlusMargin,
		DynamicRateMaxMultiplier: &max,
	}

	got := precheckMinimumCost(group, group.RateMultiplier)

	require.InDelta(t, 0.000000006, got, 1e-15)
}
