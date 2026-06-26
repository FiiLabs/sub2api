package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ValidateAPIKey is a pure (no-gin) billing-gate check that replicates the
// checks performed by apiKeyAuthWithSubscription in the same order/semantics.
//
// It covers: nil guards, key disabled/inactive check, user-active check,
// expired/quota-exhausted (by status and runtime), and balance/subscription
// fallback.  Subscription-limit checks (ValidateAndCheckLimits) are NOT
// performed here — the caller handles those, same as the middleware does.
//
// Returns:
//
//	ok=true, httpStatus=200, code="", message="" on success.
//	ok=false with appropriate httpStatus/code/message on failure.
func ValidateAPIKey(ctx context.Context, k *APIKey, sub *UserSubscription, cfg *config.Config) (ok bool, httpStatus int, code string, message string) {
	// ── 1. Nil key guard ───────────────────────────────────────────────────
	if k == nil {
		return false, 401, "INVALID_API_KEY", "Invalid API key"
	}

	// ── 2. Key status: disabled / unknown (not expired or quota_exhausted) ─
	// Expired and quota-exhausted keys are handled in the billing section below
	// (same ordering as the middleware) so they reach the correct HTTP status.
	if !k.IsActive() &&
		k.Status != StatusAPIKeyExpired &&
		k.Status != StatusAPIKeyQuotaExhausted {
		return false, 401, "API_KEY_DISABLED", "API key is disabled"
	}

	// ── 3. Nil user guard ──────────────────────────────────────────────────
	if k.User == nil {
		return false, 401, "USER_NOT_FOUND", "User associated with API key not found"
	}

	// ── 4. User active check ───────────────────────────────────────────────
	if !k.User.IsActive() {
		return false, 401, "USER_INACTIVE", "User account is not active"
	}

	// ── 5. Billing-gate: key status (quota_exhausted / expired) ───────────
	switch k.Status {
	case StatusAPIKeyQuotaExhausted:
		return false, 429, "API_KEY_QUOTA_EXHAUSTED", "API key 额度已用完"
	case StatusAPIKeyExpired:
		return false, 403, "API_KEY_EXPIRED", "API key 已过期"
	}

	// ── 6. Billing-gate: runtime expiry / quota exhaustion ────────────────
	if k.IsExpired() {
		return false, 403, "API_KEY_EXPIRED", "API key 已过期"
	}
	if k.IsQuotaExhausted() {
		return false, 429, "API_KEY_QUOTA_EXHAUSTED", "API key 额度已用完"
	}

	// ── 7. Billing-gate: subscription or balance fallback ─────────────────
	if sub == nil {
		// No subscription: check personal balance unless this key is
		// team-subject-scoped (team billing is handled by the handler, same as
		// the middleware's isTeamSubjectScoped guard).
		//
		// isTeamSubjectScoped logic (mirrors middleware): a key is team-scoped
		// when QuotaSubjectScoped is enabled and the key carries a non-zero TeamID.
		teamSubjectScoped := cfg != nil && cfg.Billing.QuotaSubjectScoped &&
			k.TeamID != nil && *k.TeamID > 0
		if !teamSubjectScoped && k.User.Balance <= 0 {
			return false, 403, "INSUFFICIENT_BALANCE", "Insufficient account balance"
		}
	}
	// If sub != nil the caller (handler) will run ValidateAndCheckLimits.

	return true, 200, "", ""
}
