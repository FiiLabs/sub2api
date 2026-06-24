//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestResolveBalanceSubjectID(t *testing.T) {
	on := &config.Config{}
	on.Billing.QuotaSubjectScoped = true
	off := &config.Config{}
	off.Billing.QuotaSubjectScoped = false

	tests := []struct {
		name   string
		cfg    *config.Config
		apiKey *APIKey
		want   int64
	}{
		{"scoped_with_subject", on, &APIKey{BillingSubjectID: 7}, 7},
		{"scoped_no_subject", on, &APIKey{BillingSubjectID: 0}, 0},
		{"flag_off", off, &APIKey{BillingSubjectID: 7}, 0},
		{"nil_cfg", nil, &APIKey{BillingSubjectID: 7}, 0},
		{"nil_apikey", on, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveBalanceSubjectID(tt.cfg, tt.apiKey); got != tt.want {
				t.Errorf("resolveBalanceSubjectID() = %d, want %d", got, tt.want)
			}
		})
	}
}
