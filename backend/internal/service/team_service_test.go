package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type teamRepoMemory struct {
	teams   map[int64]*Team
	members map[int64][]TeamMember
	nextID  int64
}

func newTeamRepoMemory() *teamRepoMemory {
	return &teamRepoMemory{
		teams:   map[int64]*Team{},
		members: map[int64][]TeamMember{},
		nextID:  1,
	}
}

func (r *teamRepoMemory) CreateTeam(ctx context.Context, input CreateTeamInput) (*Team, error) {
	id := r.nextID
	r.nextID++
	team := &Team{
		ID:              id,
		Name:            input.Name,
		Slug:            input.Slug,
		OwnerUserID:     input.ActorUserID,
		CreatedByUserID: input.ActorUserID,
		Status:          domain.TeamStatusActive,
	}
	r.teams[id] = team
	r.members[id] = append(r.members[id], TeamMember{
		TeamID:   id,
		UserID:   input.ActorUserID,
		Role:     domain.TeamRoleOwner,
		Status:   domain.TeamMemberStatusActive,
		JoinedAt: teamTestPtrTime(time.Now()),
	})
	return team, nil
}

func (r *teamRepoMemory) GetMembership(ctx context.Context, teamID, userID int64) (*TeamMember, error) {
	for _, member := range r.members[teamID] {
		if member.UserID == userID && member.Status == domain.TeamMemberStatusActive {
			return &member, nil
		}
	}
	return nil, ErrTeamMembershipNotFound
}

func (r *teamRepoMemory) ListWorkspaces(ctx context.Context, userID int64) ([]WorkspaceSubject, error) {
	out := []WorkspaceSubject{{
		BillingSubjectID: 100 + userID,
		Type:             domain.BillingSubjectTypeUser,
		UserID:           userID,
		Name:             "Personal",
		Role:             domain.TeamRoleOwner,
		Permissions:      domain.TeamRolePermissions(domain.TeamRoleOwner),
	}}
	for teamID, members := range r.members {
		for _, member := range members {
			if member.UserID == userID && member.Status == domain.TeamMemberStatusActive {
				out = append(out, WorkspaceSubject{
					BillingSubjectID: 200 + teamID,
					Type:             domain.BillingSubjectTypeTeam,
					TeamID:           teamID,
					Name:             r.teams[teamID].Name,
					Role:             member.Role,
					Permissions:      domain.TeamRolePermissions(member.Role),
				})
			}
		}
	}
	return out, nil
}

func teamTestPtrTime(t time.Time) *time.Time { return &t }

func TestTeamServiceCreateTeamCreatesOwnerWorkspace(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil)

	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 42, Name: "Platform Team", Slug: "platform-team"})
	require.NoError(t, err)
	require.Equal(t, int64(1), team.ID)

	member, err := repo.GetMembership(context.Background(), team.ID, 42)
	require.NoError(t, err)
	require.Equal(t, domain.TeamRoleOwner, member.Role)
}

func TestTeamServiceCanChecksRolePermissions(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Billing Team", Slug: "billing-team"})
	require.NoError(t, err)

	can, err := svc.Can(context.Background(), 7, team.ID, domain.TeamPermissionManageBilling)
	require.NoError(t, err)
	require.True(t, can)

	can, err = svc.Can(context.Background(), 7, team.ID, domain.TeamPermissionDeleteTeam)
	require.NoError(t, err)
	require.True(t, can)
}

func TestTeamServiceListWorkspacesIncludesPersonalAndTeams(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil)
	_, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 9, Name: "AI Ops", Slug: "ai-ops"})
	require.NoError(t, err)

	workspaces, err := svc.ListWorkspaces(context.Background(), 9)
	require.NoError(t, err)
	require.Len(t, workspaces, 2)
	require.Equal(t, domain.BillingSubjectTypeUser, workspaces[0].Type)
	require.Equal(t, domain.BillingSubjectTypeTeam, workspaces[1].Type)
	require.True(t, workspaces[1].Permissions[domain.TeamPermissionManageMembers])
}
