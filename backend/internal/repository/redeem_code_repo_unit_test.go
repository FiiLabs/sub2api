package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
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

func TestRedeemCodeRepo_ListBySubjectID(t *testing.T) {
	repo, ctx := newTestRedeemCodeRepo(t)
	subjectA := int64(100)
	subjectB := int64(200)
	mk := func(sub int64, v float64) {
		require.NoError(t, repo.Create(ctx, &service.RedeemCode{
			Code: fmt.Sprintf("c-%d-%v", sub, v), Type: service.AdjustmentTypeAdminBalance,
			Value: v, Status: service.StatusUsed, BillingSubjectID: &sub,
		}))
	}
	mk(subjectA, 1)
	mk(subjectA, 2)
	mk(subjectB, 3)

	items, res, err := repo.ListBySubjectID(ctx, subjectA, pagination.PaginationParams{Page: 1, PageSize: 10}, "")
	require.NoError(t, err)
	require.Equal(t, int64(2), res.Total)
	require.Len(t, items, 2)
	for _, it := range items {
		require.NotNil(t, it.BillingSubjectID)
		require.Equal(t, subjectA, *it.BillingSubjectID)
	}
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
