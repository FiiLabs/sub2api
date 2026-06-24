//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestRedeemAuditLogRepository_Create(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewRedeemAuditLogRepository(client)

	require.NoError(t, repo.Create(ctx, &service.RedeemAuditLog{
		RedeemCodeID:     101,
		Code:             "AUDIT-CODE",
		ActorUserID:      42,
		BillingSubjectID: 900,
		CodeType:         "balance",
		Amount:           12.5,
	}))

	var (
		actor    int64
		subject  int64
		amount   float64
		codeType string
	)
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT actor_user_id, billing_subject_id, amount, code_type FROM redeem_audit_logs WHERE redeem_code_id=$1",
		int64(101)).Scan(&actor, &subject, &amount, &codeType))
	require.Equal(t, int64(42), actor)
	require.Equal(t, int64(900), subject)
	require.InDelta(t, 12.5, amount, 0.000001)
	require.Equal(t, "balance", codeType)
}
