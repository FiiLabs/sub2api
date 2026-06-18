package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedeemEffectiveSubjectIDFallsBackToUser(t *testing.T) {
	// When no billing subject is resolved, redeem should fall back to the
	// user's personal subject id (the user id).
	require.Equal(t, int64(42), redeemEffectiveSubjectID(42, 0))
	require.Equal(t, int64(42), redeemEffectiveSubjectID(42, -1))
}

func TestRedeemEffectiveSubjectIDUsesBillingSubject(t *testing.T) {
	require.Equal(t, int64(900), redeemEffectiveSubjectID(42, 900))
}
