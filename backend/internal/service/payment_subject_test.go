package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestCreateOrderRequestCarriesBillingSubject(t *testing.T) {
	req := CreateOrderRequest{UserID: 42, BillingSubjectID: 900, TeamID: 77, PaymentType: payment.TypeStripe, OrderType: payment.OrderTypeBalance, Amount: 10}
	require.Equal(t, int64(42), req.UserID)
	require.Equal(t, int64(900), req.BillingSubjectID)
	require.Equal(t, int64(77), req.TeamID)
}

func TestPaymentInt64PtrOrNil(t *testing.T) {
	require.Nil(t, int64PtrOrNil(0))
	require.Nil(t, int64PtrOrNil(-5))
	v := int64PtrOrNil(900)
	require.NotNil(t, v)
	require.Equal(t, int64(900), *v)
}
