package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestListWorkspacesSelfHealsMissingPersonalSubject is the end-to-end guarantee
// for fix "方案 1": a user with no personal billing subject (e.g. the bootstrap
// admin, or any account created before subjects were backfilled) gets the
// subject lazily created on the first ListWorkspaces call, and repeated calls
// create no duplicates (idempotent self-heal). Without this the user would hit
// USER_NOT_FOUND on every authenticated route via SubjectContextMiddleware.
func TestListWorkspacesSelfHealsMissingPersonalSubject(t *testing.T) {
	client := newWorkspaceEntClient(t)
	ctx := context.Background()

	// User with NO personal billing subject (inserted directly).
	u := createWorkspaceTestUser(t, client, "bootstrap-admin@example.com")
	require.Equal(t, 0, countPersonalSubjects(t, client, u.ID), "precondition: no personal subject")

	svc := service.NewTeamService(
		NewTeamRepository(client),
		NewBillingSubjectRepository(client),
		NewUserRepository(client, nil),
	)

	// First call heals: personal workspace appears.
	ws, err := svc.ListWorkspaces(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, ws, 1)
	require.Equal(t, domain.BillingSubjectTypeUser, ws[0].Type)
	require.Equal(t, domain.TeamRoleOwner, ws[0].Role)
	healedID := ws[0].BillingSubjectID
	require.Positive(t, healedID)
	require.Equal(t, 1, countPersonalSubjects(t, client, u.ID))

	// Repeated calls are idempotent: same subject, no duplicate row.
	for i := 0; i < 3; i++ {
		again, err := svc.ListWorkspaces(ctx, u.ID)
		require.NoError(t, err)
		require.Len(t, again, 1)
		require.Equal(t, healedID, again[0].BillingSubjectID)
	}
	require.Equal(t, 1, countPersonalSubjects(t, client, u.ID),
		"self-heal must not create duplicate personal subjects across repeated calls")
}

// TestListWorkspacesNoHealWhenPersonalPresent ensures the happy path (subject
// already exists) is unaffected and returns the existing workspace.
func TestListWorkspacesNoHealWhenPersonalPresent(t *testing.T) {
	client := newWorkspaceEntClient(t)
	ctx := context.Background()

	u := createWorkspaceTestUser(t, client, "healthy@example.com")
	pre, err := client.BillingSubject.Create().
		SetType(domain.BillingSubjectTypeUser).
		SetUserID(u.ID).
		SetStatus(u.Status).
		SetBalance(u.Balance).
		SetConcurrency(u.Concurrency).
		Save(ctx)
	require.NoError(t, err)

	svc := service.NewTeamService(
		NewTeamRepository(client),
		NewBillingSubjectRepository(client),
		NewUserRepository(client, nil),
	)

	ws, err := svc.ListWorkspaces(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, ws, 1)
	require.Equal(t, pre.ID, ws[0].BillingSubjectID)
	require.Equal(t, 1, countPersonalSubjects(t, client, u.ID))
}

// TestListWorkspacesDeletedUserDoesNotHeal ensures a stale token for a
// non-existent user does not fabricate a subject; the empty list lets the
// caller surface USER_NOT_FOUND, which is correct.
func TestListWorkspacesDeletedUserDoesNotHeal(t *testing.T) {
	client := newWorkspaceEntClient(t)
	ctx := context.Background()

	svc := service.NewTeamService(
		NewTeamRepository(client),
		NewBillingSubjectRepository(client),
		NewUserRepository(client, nil),
	)

	ws, err := svc.ListWorkspaces(ctx, 99999) // no such user
	require.NoError(t, err)
	require.Empty(t, ws)
}
