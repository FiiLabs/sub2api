//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestValidateAPIKey(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	activeUser := &User{Status: StatusActive, Balance: 10}

	cases := []struct {
		name       string
		key        *APIKey
		sub        *UserSubscription
		wantOK     bool
		wantStatus int
	}{
		{"nil key", nil, nil, false, 401},
		{"no user", &APIKey{Status: StatusAPIKeyActive}, nil, false, 401},
		{"happy path balance", &APIKey{Status: StatusAPIKeyActive, User: activeUser}, nil, true, 200},
		{"zero balance", &APIKey{Status: StatusAPIKeyActive, User: &User{Status: StatusActive, Balance: 0}}, nil, false, 403},
		{"quota exhausted status", &APIKey{Status: StatusAPIKeyQuotaExhausted, User: activeUser}, nil, false, 429},
		{"expired status", &APIKey{Status: StatusAPIKeyExpired, User: activeUser}, nil, false, 403},
		// Newly added cases covering previously untested branches:
		// sub != nil happy path: subscription present → balance check skipped, ok=true.
		{"sub present happy path", &APIKey{Status: StatusAPIKeyActive, User: activeUser}, &UserSubscription{}, true, 200},
		// User inactive (step 4 in ValidateAPIKey): 401 USER_INACTIVE.
		{"user inactive", &APIKey{Status: StatusAPIKeyActive, User: &User{Status: "inactive", Balance: 10}}, nil, false, 401},
		// Runtime expiry (step 6): ExpiresAt in the past, status is NOT StatusAPIKeyExpired → 403.
		{"runtime expired", &APIKey{
			Status:    StatusAPIKeyActive,
			User:      activeUser,
			ExpiresAt: func() *time.Time { t := time.Now().Add(-time.Hour); return &t }(),
		}, nil, false, 403},
		// Runtime quota-exhausted (step 6): Quota>0 and QuotaUsed>=Quota, status NOT StatusAPIKeyQuotaExhausted → 429.
		{"runtime quota exhausted", &APIKey{
			Status:    StatusAPIKeyActive,
			User:      activeUser,
			Quota:     10,
			QuotaUsed: 10,
		}, nil, false, 429},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, status, _, _ := ValidateAPIKey(ctx, tc.key, tc.sub, cfg)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantStatus, status)
		})
	}
}
