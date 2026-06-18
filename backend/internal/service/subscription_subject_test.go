package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssignSubscriptionInputCarriesBillingSubject(t *testing.T) {
	input := AssignSubscriptionInput{UserID: 42, BillingSubjectID: 900, GroupID: 7, ValidityDays: 30}
	require.Equal(t, int64(42), input.UserID)
	require.Equal(t, int64(900), input.BillingSubjectID)
	require.Equal(t, int64(7), input.GroupID)
}

func TestUserSubscriptionCarriesBillingSubject(t *testing.T) {
	sub := UserSubscription{UserID: 42, GroupID: 7, BillingSubjectID: 900}
	require.Equal(t, int64(900), sub.BillingSubjectID)
}
