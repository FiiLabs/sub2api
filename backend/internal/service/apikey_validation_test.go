package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestValidateAPIKey(t *testing.T) {
	ctx := context.Background()

	activeUser := &User{
		ID:      1,
		Status:  StatusActive,
		Balance: 10.0,
	}

	zeroBalanceUser := &User{
		ID:      2,
		Status:  StatusActive,
		Balance: 0.0,
	}

	inactiveUser := &User{
		ID:      3,
		Status:  StatusDisabled,
		Balance: 10.0,
	}

	activeKey := &APIKey{
		ID:     1,
		Status: StatusAPIKeyActive,
		User:   activeUser,
	}

	expiredAtTime := time.Now().Add(-time.Hour)
	expiredKey := &APIKey{
		ID:        2,
		Status:    StatusAPIKeyExpired,
		User:      activeUser,
		ExpiresAt: &expiredAtTime,
	}

	quotaExhaustedKey := &APIKey{
		ID:     3,
		Status: StatusAPIKeyQuotaExhausted,
		User:   activeUser,
		Quota:  100.0,
		QuotaUsed: 100.0,
	}

	disabledKey := &APIKey{
		ID:     4,
		Status: StatusAPIKeyDisabled,
		User:   activeUser,
	}

	nilUserKey := &APIKey{
		ID:     5,
		Status: StatusAPIKeyActive,
		User:   nil,
	}

	zeroBalanceKey := &APIKey{
		ID:     6,
		Status: StatusAPIKeyActive,
		User:   zeroBalanceUser,
	}

	inactiveUserKey := &APIKey{
		ID:     7,
		Status: StatusAPIKeyActive,
		User:   inactiveUser,
	}

	// Active key with expired ExpiresAt but status still "active" (runtime expiry check)
	runtimeExpiredAt := time.Now().Add(-time.Minute)
	runtimeExpiredKey := &APIKey{
		ID:        8,
		Status:    StatusAPIKeyActive,
		User:      activeUser,
		ExpiresAt: &runtimeExpiredAt,
	}

	// Active key with quota exceeded at runtime but status still "active"
	runtimeQuotaExhaustedKey := &APIKey{
		ID:        9,
		Status:    StatusAPIKeyActive,
		User:      activeUser,
		Quota:     50.0,
		QuotaUsed: 60.0,
	}

	standardCfg := &config.Config{}

	// cfg that enables team-subject-scoped billing
	teamCfg := &config.Config{}
	teamCfg.Billing.QuotaSubjectScoped = true

	teamID := int64(42)
	teamScopedKey := &APIKey{
		ID:     10,
		Status: StatusAPIKeyActive,
		User:   zeroBalanceUser,
		TeamID: &teamID,
	}

	activeSub := &UserSubscription{
		ID:       1,
		Status:   SubscriptionStatusActive,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	tests := []struct {
		name           string
		key            *APIKey
		sub            *UserSubscription
		cfg            *config.Config
		wantOK         bool
		wantHTTPStatus int
		wantCode       string
	}{
		{
			name:           "active key with positive balance and no subscription → ok",
			key:            activeKey,
			sub:            nil,
			cfg:            standardCfg,
			wantOK:         true,
			wantHTTPStatus: 200,
			wantCode:       "",
		},
		{
			name:           "active key with positive balance and active subscription → ok",
			key:            activeKey,
			sub:            activeSub,
			cfg:            standardCfg,
			wantOK:         true,
			wantHTTPStatus: 200,
			wantCode:       "",
		},
		{
			name:           "nil key → 401 invalid",
			key:            nil,
			sub:            nil,
			cfg:            standardCfg,
			wantOK:         false,
			wantHTTPStatus: 401,
			wantCode:       "INVALID_API_KEY",
		},
		{
			name:           "nil user → 401 user not found",
			key:            nilUserKey,
			sub:            nil,
			cfg:            standardCfg,
			wantOK:         false,
			wantHTTPStatus: 401,
			wantCode:       "USER_NOT_FOUND",
		},
		{
			name:           "disabled key → 401 API_KEY_DISABLED",
			key:            disabledKey,
			sub:            nil,
			cfg:            standardCfg,
			wantOK:         false,
			wantHTTPStatus: 401,
			wantCode:       "API_KEY_DISABLED",
		},
		{
			name:           "inactive user → 401 USER_INACTIVE",
			key:            inactiveUserKey,
			sub:            nil,
			cfg:            standardCfg,
			wantOK:         false,
			wantHTTPStatus: 401,
			wantCode:       "USER_INACTIVE",
		},
		{
			name:           "expired status key → 403 API_KEY_EXPIRED",
			key:            expiredKey,
			sub:            nil,
			cfg:            standardCfg,
			wantOK:         false,
			wantHTTPStatus: 403,
			wantCode:       "API_KEY_EXPIRED",
		},
		{
			name:           "quota exhausted status key → 429 API_KEY_QUOTA_EXHAUSTED",
			key:            quotaExhaustedKey,
			sub:            nil,
			cfg:            standardCfg,
			wantOK:         false,
			wantHTTPStatus: 429,
			wantCode:       "API_KEY_QUOTA_EXHAUSTED",
		},
		{
			name:           "runtime expired key (status=active but ExpiresAt past) → 403 API_KEY_EXPIRED",
			key:            runtimeExpiredKey,
			sub:            nil,
			cfg:            standardCfg,
			wantOK:         false,
			wantHTTPStatus: 403,
			wantCode:       "API_KEY_EXPIRED",
		},
		{
			name:           "runtime quota exhausted key (status=active but QuotaUsed>=Quota) → 429 API_KEY_QUOTA_EXHAUSTED",
			key:            runtimeQuotaExhaustedKey,
			sub:            nil,
			cfg:            standardCfg,
			wantOK:         false,
			wantHTTPStatus: 429,
			wantCode:       "API_KEY_QUOTA_EXHAUSTED",
		},
		{
			name:           "active key with zero balance and no subscription → 403 INSUFFICIENT_BALANCE",
			key:            zeroBalanceKey,
			sub:            nil,
			cfg:            standardCfg,
			wantOK:         false,
			wantHTTPStatus: 403,
			wantCode:       "INSUFFICIENT_BALANCE",
		},
		{
			name:           "zero balance key with active subscription → ok (subscription bypasses balance check)",
			key:            zeroBalanceKey,
			sub:            activeSub,
			cfg:            standardCfg,
			wantOK:         true,
			wantHTTPStatus: 200,
			wantCode:       "",
		},
		{
			name:           "team-scoped key with zero balance and no subscription → ok (team billing bypasses personal balance)",
			key:            teamScopedKey,
			sub:            nil,
			cfg:            teamCfg,
			wantOK:         true,
			wantHTTPStatus: 200,
			wantCode:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, httpStatus, code, _ := ValidateAPIKey(ctx, tt.key, tt.sub, tt.cfg)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if httpStatus != tt.wantHTTPStatus {
				t.Errorf("httpStatus = %d, want %d", httpStatus, tt.wantHTTPStatus)
			}
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
		})
	}
}
