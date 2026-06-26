package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// newTestRedeemCodeRepo returns a RedeemCodeRepository backed by an in-memory
// sqlite ent client (auto-migrated from the ent schema). The client is shared
// with newWorkspaceEntClient defined in billing_subject_repository_unit_test.go.
func newTestRedeemCodeRepo(t *testing.T) (service.RedeemCodeRepository, context.Context) {
	t.Helper()
	client := newWorkspaceEntClient(t)
	repo := NewRedeemCodeRepository(client)
	return repo, context.Background()
}

func TestRedeemCodeRepo_BillingSubjectID_RoundTrip(t *testing.T) {
	repo, ctx := newTestRedeemCodeRepo(t)
	subjectID := int64(4242)
	code := &service.RedeemCode{
		Code:             "rt-bsid-1",
		Type:             service.AdjustmentTypeAdminBalance,
		Value:            12.5,
		Status:           service.StatusUsed,
		BillingSubjectID: &subjectID,
		Notes:            "team topup",
	}
	require.NoError(t, repo.Create(ctx, code))
	got, err := repo.GetByID(ctx, code.ID)
	require.NoError(t, err)
	require.NotNil(t, got.BillingSubjectID)
	require.Equal(t, subjectID, *got.BillingSubjectID)
}
