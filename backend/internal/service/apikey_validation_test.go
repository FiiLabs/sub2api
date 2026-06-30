//go:build unit

package service

import (
	"context"
	"testing"

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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, status, _, _ := ValidateAPIKey(ctx, tc.key, tc.sub, cfg)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantStatus, status)
		})
	}
}
