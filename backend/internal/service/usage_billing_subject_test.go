package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageBillingFingerprintIncludesBillingSubject(t *testing.T) {
	base := UsageBillingCommand{
		RequestID: "req-1", APIKeyID: 10, UserID: 20, BillingSubjectID: 100,
		AccountID: 30, Model: "gpt-5.4", BillingType: BillingTypeBalance,
		InputTokens: 100, OutputTokens: 25, BalanceCost: 0.05,
	}
	other := base
	other.BillingSubjectID = 200

	require.NotEqual(t, buildUsageBillingFingerprint(&base), buildUsageBillingFingerprint(&other))
}
