package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/teammember"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// seedTeamWithOwner creates a user (the owner) and a team owned by them via the
// repo (which also creates the owner's active membership + billing subject), and
// returns the repo, the team, and the owner user id.
func seedTeamWithOwner(t *testing.T, client *dbent.Client) (service.TeamRepository, *service.Team, int64) {
	t.Helper()
	ctx := context.Background()
	owner := createWorkspaceTestUser(t, client, "owner@example.com")
	repo := NewTeamRepository(client)
	team, err := repo.CreateTeam(ctx, service.CreateTeamInput{ActorUserID: owner.ID, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)
	return repo, team, owner.ID
}

// createPendingInvitation inserts a pending invitation row directly and returns it.
func createPendingInvitation(t *testing.T, client *dbent.Client, teamID, inviterID int64, email, role, tokenHash string, expiresAt time.Time) *dbent.TeamInvitation {
	t.Helper()
	inv, err := client.TeamInvitation.Create().
		SetTeamID(teamID).
		SetEmail(email).
		SetRole(role).
		SetTokenHash(tokenHash).
		SetStatus(domain.TeamInvitationStatusPending).
		SetInvitedByUserID(inviterID).
		SetExpiresAt(expiresAt).
		Save(context.Background())
	require.NoError(t, err)
	return inv
}

func TestTeamRepositoryGetInvitationByTokenHash(t *testing.T) {
	client := newWorkspaceEntClient(t)
	ctx := context.Background()
	repo, team, ownerID := seedTeamWithOwner(t, client)

	createPendingInvitation(t, client, team.ID, ownerID, "invitee@example.com", domain.TeamRoleDeveloper, "hash-1", time.Now().Add(time.Hour))

	got, err := repo.GetInvitationByTokenHash(ctx, "hash-1")
	require.NoError(t, err)
	require.Equal(t, "invitee@example.com", got.Email)
	require.Equal(t, domain.TeamRoleDeveloper, got.Role)
	require.Equal(t, team.ID, got.TeamID)

	// Unknown hash -> invalid.
	_, err = repo.GetInvitationByTokenHash(ctx, "nope")
	require.ErrorIs(t, err, service.ErrTeamInvitationInvalid)
}

func TestTeamRepositoryAcceptInvitationCreatesMembership(t *testing.T) {
	client := newWorkspaceEntClient(t)
	ctx := context.Background()
	repo, team, ownerID := seedTeamWithOwner(t, client)
	invitee := createWorkspaceTestUser(t, client, "invitee@example.com")
	inv := createPendingInvitation(t, client, team.ID, ownerID, "invitee@example.com", domain.TeamRoleDeveloper, "hash-acc", time.Now().Add(time.Hour))

	member, err := repo.AcceptInvitation(ctx, inv.ID, invitee.ID, team.ID, inv.Role)
	require.NoError(t, err)
	require.Equal(t, invitee.ID, member.UserID)
	require.Equal(t, domain.TeamRoleDeveloper, member.Role)
	require.Equal(t, domain.TeamMemberStatusActive, member.Status)
	require.NotNil(t, member.InvitedByUserID)
	require.Equal(t, ownerID, *member.InvitedByUserID)

	// Membership is active.
	got, err := repo.GetMembership(ctx, team.ID, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, domain.TeamRoleDeveloper, got.Role)

	// Invitation is now accepted, bound to the accepting user.
	reloaded, err := client.TeamInvitation.Get(ctx, inv.ID)
	require.NoError(t, err)
	require.Equal(t, domain.TeamInvitationStatusAccepted, reloaded.Status)
	require.NotNil(t, reloaded.AcceptedByUserID)
	require.Equal(t, invitee.ID, *reloaded.AcceptedByUserID)
}

func TestTeamRepositoryAcceptInvitationIdempotentSameUser(t *testing.T) {
	client := newWorkspaceEntClient(t)
	ctx := context.Background()
	repo, team, ownerID := seedTeamWithOwner(t, client)
	invitee := createWorkspaceTestUser(t, client, "invitee@example.com")
	inv := createPendingInvitation(t, client, team.ID, ownerID, "invitee@example.com", domain.TeamRoleViewer, "hash-idem", time.Now().Add(time.Hour))

	first, err := repo.AcceptInvitation(ctx, inv.ID, invitee.ID, team.ID, inv.Role)
	require.NoError(t, err)

	// Re-accepting (already accepted by same user) returns the existing membership.
	second, err := repo.AcceptInvitation(ctx, inv.ID, invitee.ID, team.ID, inv.Role)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)

	// Exactly one membership row for the invitee.
	count, err := client.TeamMember.Query().
		Where(teammember.TeamIDEQ(team.ID), teammember.UserIDEQ(invitee.ID)).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestTeamRepositoryAcceptInvitationReactivatesLeftMember(t *testing.T) {
	client := newWorkspaceEntClient(t)
	ctx := context.Background()
	repo, team, ownerID := seedTeamWithOwner(t, client)
	invitee := createWorkspaceTestUser(t, client, "invitee@example.com")

	// Invitee joined then left (soft-deleted).
	added, err := repo.AddMember(ctx, team.ID, invitee.ID, domain.TeamRoleViewer, ownerID)
	require.NoError(t, err)
	require.NoError(t, repo.RemoveMember(ctx, ownerID, team.ID, invitee.ID))

	inv := createPendingInvitation(t, client, team.ID, ownerID, "invitee@example.com", domain.TeamRoleDeveloper, "hash-react", time.Now().Add(time.Hour))
	member, err := repo.AcceptInvitation(ctx, inv.ID, invitee.ID, team.ID, inv.Role)
	require.NoError(t, err)
	require.Equal(t, added.ID, member.ID, "expected the soft-deleted membership row to be reactivated")
	require.Equal(t, domain.TeamRoleDeveloper, member.Role)
	require.Equal(t, domain.TeamMemberStatusActive, member.Status)
}

func TestTeamRepositoryTransferOwnership(t *testing.T) {
	client := newWorkspaceEntClient(t)
	ctx := context.Background()
	repo, team, ownerID := seedTeamWithOwner(t, client)
	newOwner := createWorkspaceTestUser(t, client, "newowner@example.com")
	_, err := repo.AddMember(ctx, team.ID, newOwner.ID, domain.TeamRoleDeveloper, ownerID)
	require.NoError(t, err)

	require.NoError(t, repo.TransferOwnership(ctx, team.ID, newOwner.ID, ownerID))

	// teams.owner_user_id updated.
	got, err := repo.GetTeamByID(ctx, team.ID)
	require.NoError(t, err)
	require.Equal(t, newOwner.ID, got.OwnerUserID)

	// Roles swapped: new owner = owner, previous owner = admin.
	no, err := repo.GetMembership(ctx, team.ID, newOwner.ID)
	require.NoError(t, err)
	require.Equal(t, domain.TeamRoleOwner, no.Role)
	po, err := repo.GetMembership(ctx, team.ID, ownerID)
	require.NoError(t, err)
	require.Equal(t, domain.TeamRoleAdmin, po.Role)
}

func TestTeamRepositoryTransferOwnershipRequiresActiveNewOwner(t *testing.T) {
	client := newWorkspaceEntClient(t)
	ctx := context.Background()
	repo, team, ownerID := seedTeamWithOwner(t, client)
	stranger := createWorkspaceTestUser(t, client, "stranger@example.com")

	// stranger is not a member -> error, and owner unchanged.
	err := repo.TransferOwnership(ctx, team.ID, stranger.ID, ownerID)
	require.ErrorIs(t, err, service.ErrTeamMembershipNotFound)

	got, err := repo.GetTeamByID(ctx, team.ID)
	require.NoError(t, err)
	require.Equal(t, ownerID, got.OwnerUserID)
}
