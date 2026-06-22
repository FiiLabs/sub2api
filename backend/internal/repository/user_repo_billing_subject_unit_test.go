package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/billingsubject"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestUserRepositoryCreateProvisionsPersonalBillingSubject covers fix "方案 2" at
// the repository choke point: every user created through userRepo.Create gets a
// personal billing subject in the same transaction, seeded from the user.
func TestUserRepositoryCreateProvisionsPersonalBillingSubject(t *testing.T) {
	repo, client := newUserEntRepo(t)
	ctx := context.Background()

	u := &service.User{
		Email:        "provision@example.com",
		Username:     "provision",
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		Balance:      12.5,
		Concurrency:  4,
	}
	require.NoError(t, repo.Create(ctx, u))
	require.Positive(t, u.ID)

	require.Equal(t, 1, countPersonalSubjects(t, client, u.ID),
		"userRepo.Create must provision exactly one personal billing subject")

	sub, err := client.BillingSubject.Query().
		Where(billingsubject.UserIDEQ(u.ID), billingsubject.DeletedAtIsNil()).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, domain.BillingSubjectTypeUser, sub.Type)
	require.Equal(t, u.Balance, sub.Balance)
	require.Equal(t, u.Concurrency, sub.Concurrency)
}

// TestEnsurePersonalBillingSubjectWithClientIsIdempotent exercises the helper's
// check-first guard directly: a second call for a user that already has a
// subject is a no-op (important because it runs inside the creation tx, where a
// unique-conflict would otherwise poison the transaction on Postgres).
func TestEnsurePersonalBillingSubjectWithClientIsIdempotent(t *testing.T) {
	_, client := newUserEntRepo(t)
	ctx := context.Background()

	u := createWorkspaceTestUser(t, client, "helper-idem@example.com")

	require.NoError(t, ensurePersonalBillingSubjectWithClient(ctx, client, u))
	require.NoError(t, ensurePersonalBillingSubjectWithClient(ctx, client, u))
	require.NoError(t, ensurePersonalBillingSubjectWithClient(ctx, client, u))

	require.Equal(t, 1, countPersonalSubjects(t, client, u.ID),
		"ensurePersonalBillingSubjectWithClient must be idempotent")
}
