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

func TestTeamRepositoryListMembersAggregatesKeysAndRecentCost(t *testing.T) {
	client := newWorkspaceEntClient(t)
	ctx := context.Background()
	repo, team, ownerID := seedTeamWithOwner(t, client)

	member := createWorkspaceTestUser(t, client, "dev@example.com")
	_, err := repo.AddMember(ctx, team.ID, member.ID, domain.TeamRoleDeveloper, ownerID)
	require.NoError(t, err)

	// A second team whose keys/usage must NOT bleed into this team's totals.
	otherOwner := createWorkspaceTestUser(t, client, "other@example.com")
	otherTeam, err := repo.CreateTeam(ctx, service.CreateTeamInput{ActorUserID: otherOwner.ID, Name: "Other", Slug: "other"})
	require.NoError(t, err)

	// Keys: owner has 2 in this team, member has 1; owner also has 1 in the other team.
	mustCreateTeamAPIKey(t, client, team.ID, ownerID, "o1")
	keyID := mustCreateTeamAPIKey(t, client, team.ID, ownerID, "o2")
	mustCreateTeamAPIKey(t, client, team.ID, member.ID, "m1")
	mustCreateTeamAPIKey(t, client, otherTeam.ID, ownerID, "x1")

	accID := mustCreateMinimalAccount(t, client)
	now := time.Now()

	// Owner as actor in THIS team: 1.0 (1h) + 0.5 (2d) are within 7d; 9.9 (8d) is excluded.
	mustCreateTeamUsage(t, client, team.ID, ownerID, keyID, accID, 1.0, now.Add(-1*time.Hour), "r1")
	mustCreateTeamUsage(t, client, team.ID, ownerID, keyID, accID, 0.5, now.Add(-2*24*time.Hour), "r2")
	mustCreateTeamUsage(t, client, team.ID, ownerID, keyID, accID, 9.9, now.Add(-8*24*time.Hour), "r3")
	// Member as actor in THIS team: 0.25 (3h).
	mustCreateTeamUsage(t, client, team.ID, member.ID, keyID, accID, 0.25, now.Add(-3*time.Hour), "r4")
	// Owner's spend in the OTHER team must be excluded from this team's totals.
	mustCreateTeamUsage(t, client, otherTeam.ID, ownerID, keyID, accID, 7.0, now.Add(-1*time.Hour), "r5")

	members, _, err := repo.ListMembers(ctx, team.ID)
	require.NoError(t, err)

	byUser := make(map[int64]service.TeamMember, len(members))
	for _, m := range members {
		byUser[m.UserID] = m
	}

	require.Equal(t, 2, byUser[ownerID].KeyCount, "owner key count within team")
	require.Equal(t, 1, byUser[member.ID].KeyCount, "member key count within team")
	require.InDelta(t, 1.5, byUser[ownerID].Last7dActualCost, 1e-9, "owner 7d actual cost")
	require.InDelta(t, 0.25, byUser[member.ID].Last7dActualCost, 1e-9, "member 7d actual cost")
}

func mustCreateTeamAPIKey(t *testing.T, client *dbent.Client, teamID, userID int64, key string) int64 {
	t.Helper()
	k, err := client.APIKey.Create().
		SetUserID(userID).
		SetTeamID(teamID).
		SetKey(key).
		SetName(key).
		SetStatus(service.StatusActive).
		Save(context.Background())
	require.NoError(t, err)
	return k.ID
}

func mustCreateMinimalAccount(t *testing.T, client *dbent.Client) int64 {
	t.Helper()
	a, err := client.Account.Create().
		SetName("acc").
		SetPlatform("claude").
		SetType("api_key").
		Save(context.Background())
	require.NoError(t, err)
	return a.ID
}

func mustCreateTeamUsage(t *testing.T, client *dbent.Client, teamID, actorUserID, apiKeyID, accountID int64, cost float64, createdAt time.Time, reqID string) {
	t.Helper()
	_, err := client.UsageLog.Create().
		SetUserID(actorUserID).
		SetAPIKeyID(apiKeyID).
		SetAccountID(accountID).
		SetRequestID(reqID).
		SetModel("claude-3").
		SetTeamID(teamID).
		SetActorUserID(actorUserID).
		SetActualCost(cost).
		SetCreatedAt(createdAt).
		Save(context.Background())
	require.NoError(t, err)
}
