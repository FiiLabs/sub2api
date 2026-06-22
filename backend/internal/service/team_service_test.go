package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
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
	// Mirror the production repo: owner defaults to the actor unless OwnerUserID
	// overrides it; the creator is always the actor.
	owner := input.ActorUserID
	if input.OwnerUserID > 0 {
		owner = input.OwnerUserID
	}
	team := &Team{
		ID:              id,
		Name:            input.Name,
		Slug:            input.Slug,
		OwnerUserID:     owner,
		CreatedByUserID: input.ActorUserID,
		Status:          domain.TeamStatusActive,
	}
	r.teams[id] = team
	r.members[id] = append(r.members[id], TeamMember{
		TeamID:   id,
		UserID:   owner,
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

func (r *teamRepoMemory) ListMembers(ctx context.Context, teamID int64) ([]TeamMember, []TeamInvitation, error) {
	return r.members[teamID], nil, nil
}

func (r *teamRepoMemory) InviteMember(ctx context.Context, input InviteTeamMemberInput) (*TeamInvitation, error) {
	id := r.nextID
	r.nextID++
	return &TeamInvitation{
		ID:              id,
		TeamID:          input.TeamID,
		Email:           input.Email,
		Role:            input.Role,
		TokenHash:       input.TokenHash,
		Status:          domain.TeamInvitationStatusPending,
		InvitedByUserID: input.ActorUserID,
		ExpiresAt:       input.ExpiresAt,
	}, nil
}

func (r *teamRepoMemory) UpdateMember(ctx context.Context, actorUserID, teamID, userID int64, input UpdateTeamMemberInput) (*TeamMember, error) {
	members := r.members[teamID]
	for i := range members {
		if members[i].UserID == userID {
			if input.Role != nil {
				members[i].Role = *input.Role
			}
			if input.Status != nil {
				members[i].Status = *input.Status
			}
			r.members[teamID] = members
			m := members[i]
			return &m, nil
		}
	}
	return nil, ErrTeamMembershipNotFound
}

func (r *teamRepoMemory) RemoveMember(ctx context.Context, actorUserID, teamID, userID int64) error {
	members := r.members[teamID]
	for i := range members {
		if members[i].UserID == userID && members[i].Status != domain.TeamMemberStatusLeft {
			// In the in-memory mock, status "left" doubles as the soft-deleted marker.
			members[i].Status = domain.TeamMemberStatusLeft
			r.members[teamID] = members
			return nil
		}
	}
	return ErrTeamMembershipNotFound
}

func (r *teamRepoMemory) AdminListTeams(ctx context.Context, filter AdminTeamListFilter, params pagination.PaginationParams) ([]AdminTeamSummary, *pagination.PaginationResult, error) {
	out := make([]AdminTeamSummary, 0, len(r.teams))
	for id, team := range r.teams {
		if filter.Status != "" && team.Status != filter.Status {
			continue
		}
		active := 0
		for _, m := range r.members[id] {
			if m.Status == domain.TeamMemberStatusActive {
				active++
			}
		}
		out = append(out, AdminTeamSummary{Team: *team, MemberCount: active})
	}
	total := int64(len(out))
	return out, &pagination.PaginationResult{Total: total, Page: params.Page, PageSize: params.PageSize, Pages: 1}, nil
}

func (r *teamRepoMemory) GetTeamByID(ctx context.Context, teamID int64) (*Team, error) {
	team, ok := r.teams[teamID]
	if !ok {
		return nil, ErrTeamNotFound
	}
	t := *team
	return &t, nil
}

func (r *teamRepoMemory) AdminGetTeamSummary(ctx context.Context, teamID int64) (*AdminTeamSummary, error) {
	team, ok := r.teams[teamID]
	if !ok {
		return nil, ErrTeamNotFound
	}
	active := 0
	for _, m := range r.members[teamID] {
		if m.Status == domain.TeamMemberStatusActive {
			active++
		}
	}
	return &AdminTeamSummary{Team: *team, MemberCount: active}, nil
}

func (r *teamRepoMemory) AddMember(ctx context.Context, teamID, userID int64, role string, invitedByUserID int64) (*TeamMember, error) {
	members := r.members[teamID]
	// Reject an existing active membership.
	for i := range members {
		if members[i].UserID == userID && members[i].Status == domain.TeamMemberStatusActive {
			return nil, ErrTeamMemberExists
		}
	}
	// Reactivate the most recent soft-deleted/left row if present (status "left"
	// is the in-memory soft-deleted marker).
	for i := len(members) - 1; i >= 0; i-- {
		if members[i].UserID == userID && members[i].Status == domain.TeamMemberStatusLeft {
			members[i].Role = role
			members[i].Status = domain.TeamMemberStatusActive
			members[i].JoinedAt = teamTestPtrTime(time.Now())
			if invitedByUserID > 0 {
				members[i].InvitedByUserID = &invitedByUserID
			}
			r.members[teamID] = members
			m := members[i]
			return &m, nil
		}
	}
	id := r.nextID
	r.nextID++
	member := TeamMember{
		ID:       id,
		TeamID:   teamID,
		UserID:   userID,
		Role:     role,
		Status:   domain.TeamMemberStatusActive,
		JoinedAt: teamTestPtrTime(time.Now()),
	}
	if invitedByUserID > 0 {
		member.InvitedByUserID = &invitedByUserID
	}
	r.members[teamID] = append(members, member)
	return &member, nil
}

func (r *teamRepoMemory) UpdateTeam(ctx context.Context, teamID int64, name *string, status *string) (*Team, error) {
	team, ok := r.teams[teamID]
	if !ok {
		return nil, ErrTeamNotFound
	}
	if name != nil {
		team.Name = *name
	}
	if status != nil {
		team.Status = *status
	}
	r.teams[teamID] = team
	t := *team
	return &t, nil
}

func teamTestPtrTime(t time.Time) *time.Time { return &t }

func TestTeamServiceListMembersRequiresViewPermission(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	members, _, err := svc.ListMembers(context.Background(), 7, team.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)

	// non-member is denied
	_, _, err = svc.ListMembers(context.Background(), 99, team.ID)
	require.Error(t, err)
}

func TestTeamServiceInviteMemberRejectsOwnerRole(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	_, _, err = svc.InviteMember(context.Background(), InviteTeamMemberInput{ActorUserID: 7, TeamID: team.ID, Email: "x@example.com", Role: domain.TeamRoleOwner, ExpiresAt: time.Now().Add(time.Hour)})
	require.ErrorIs(t, err, ErrTeamInvalidRole)

	invitation, token, err := svc.InviteMember(context.Background(), InviteTeamMemberInput{ActorUserID: 7, TeamID: team.ID, Email: "X@Example.com", Role: domain.TeamRoleDeveloper, ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Equal(t, "x@example.com", invitation.Email)
	require.NotEmpty(t, invitation.TokenHash)
}

func TestTeamServiceUpdateMemberValidations(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)
	repo.members[team.ID] = append(repo.members[team.ID], TeamMember{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleViewer, Status: domain.TeamMemberStatusActive})

	// empty update rejected
	_, err = svc.UpdateMember(context.Background(), 7, team.ID, 8, UpdateTeamMemberInput{})
	require.Error(t, err)

	// owner role rejected
	owner := domain.TeamRoleOwner
	_, err = svc.UpdateMember(context.Background(), 7, team.ID, 8, UpdateTeamMemberInput{Role: &owner})
	require.ErrorIs(t, err, ErrTeamInvalidRole)

	dev := domain.TeamRoleDeveloper
	member, err := svc.UpdateMember(context.Background(), 7, team.ID, 8, UpdateTeamMemberInput{Role: &dev})
	require.NoError(t, err)
	require.Equal(t, domain.TeamRoleDeveloper, member.Role)
}

func TestTeamServiceCreateTeamCreatesOwnerWorkspace(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)

	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 42, Name: "Platform Team", Slug: "platform-team"})
	require.NoError(t, err)
	require.Equal(t, int64(1), team.ID)

	member, err := repo.GetMembership(context.Background(), team.ID, 42)
	require.NoError(t, err)
	require.Equal(t, domain.TeamRoleOwner, member.Role)
}

func TestTeamServiceCreateTeamAutoGeneratesSlug(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)

	// Name only (no slug): slug is auto-generated from the name plus a suffix.
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 42, Name: "Platform Team"})
	require.NoError(t, err)
	require.NotEmpty(t, team.Slug)
	require.True(t, strings.HasPrefix(team.Slug, "platform-team-"), "got slug %q", team.Slug)
	// base "platform-team" + "-" + 6 hex chars
	require.Equal(t, len("platform-team-")+6, len(team.Slug))

	// An explicit slug is preserved as-is.
	team2, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 42, Name: "Another", Slug: "custom-slug"})
	require.NoError(t, err)
	require.Equal(t, "custom-slug", team2.Slug)
}

func TestTeamServiceCreateTeamSlugFallbackForNonASCIIName(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)

	// All-non-ASCII name slugifies to "" -> falls back to "team-<suffix>".
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "团队名称"})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(team.Slug, "team-"), "got slug %q", team.Slug)
	require.Equal(t, len("team-")+6, len(team.Slug))
}

func TestTeamServiceCreateTeamRequiresName(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)

	// Name is required even though slug is now optional.
	_, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Slug: "ops"})
	require.Error(t, err)
}

func TestTeamServiceAdminCreateTeamByOwnerID(t *testing.T) {
	repo := newTeamRepoMemory()
	lookup := teamUserLookupStub{byID: map[int64]*User{
		55: {ID: 55, Email: "owner@example.com", Username: "owner"},
	}}
	svc := &TeamService{repo: repo, userLookup: lookup}

	summary, err := svc.AdminCreateTeam(context.Background(), AdminCreateTeamInput{
		Name:        "Owned Team",
		OwnerUserID: 55,
		AdminUserID: 1,
	})
	require.NoError(t, err)
	// Owner is the resolved user (55), NOT the admin (1).
	require.Equal(t, int64(55), summary.OwnerUserID)
	require.True(t, strings.HasPrefix(summary.Slug, "owned-team-"), "got slug %q", summary.Slug)

	// The creator is the admin while the owner membership is the resolved user.
	team := repo.teams[summary.ID]
	require.Equal(t, int64(1), team.CreatedByUserID)
	owner, err := repo.GetMembership(context.Background(), summary.ID, 55)
	require.NoError(t, err)
	require.Equal(t, domain.TeamRoleOwner, owner.Role)
	// The admin is not a member.
	_, err = repo.GetMembership(context.Background(), summary.ID, 1)
	require.Error(t, err)

	// Unknown owner id yields not-found.
	_, err = svc.AdminCreateTeam(context.Background(), AdminCreateTeamInput{Name: "X", OwnerUserID: 999, AdminUserID: 1})
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestTeamServiceAdminCreateTeamByOwnerEmail(t *testing.T) {
	repo := newTeamRepoMemory()
	lookup := teamUserLookupStub{byEmail: map[string]*User{
		"owner@example.com": {ID: 77, Email: "owner@example.com", Username: "owner"},
	}}
	svc := &TeamService{repo: repo, userLookup: lookup}

	summary, err := svc.AdminCreateTeam(context.Background(), AdminCreateTeamInput{
		Name:        "Email Team",
		OwnerEmail:  "Owner@Example.com", // resolved case-insensitively
		AdminUserID: 2,
	})
	require.NoError(t, err)
	require.Equal(t, int64(77), summary.OwnerUserID)

	// Unknown email yields not-found.
	_, err = svc.AdminCreateTeam(context.Background(), AdminCreateTeamInput{Name: "Y", OwnerEmail: "ghost@example.com", AdminUserID: 2})
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestTeamServiceAdminCreateTeamRequiresOwner(t *testing.T) {
	repo := newTeamRepoMemory()
	lookup := teamUserLookupStub{}
	svc := &TeamService{repo: repo, userLookup: lookup}

	// Neither owner_user_id nor owner_email provided -> bad request.
	_, err := svc.AdminCreateTeam(context.Background(), AdminCreateTeamInput{Name: "No Owner", AdminUserID: 1})
	require.Error(t, err)

	// Empty name -> bad request.
	_, err = svc.AdminCreateTeam(context.Background(), AdminCreateTeamInput{OwnerUserID: 1, AdminUserID: 1})
	require.Error(t, err)
}

func TestTeamServiceCanChecksRolePermissions(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
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
	svc := NewTeamService(repo, nil, nil)
	_, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 9, Name: "AI Ops", Slug: "ai-ops"})
	require.NoError(t, err)

	workspaces, err := svc.ListWorkspaces(context.Background(), 9)
	require.NoError(t, err)
	require.Len(t, workspaces, 2)
	require.Equal(t, domain.BillingSubjectTypeUser, workspaces[0].Type)
	require.Equal(t, domain.BillingSubjectTypeTeam, workspaces[1].Type)
	require.True(t, workspaces[1].Permissions[domain.TeamPermissionManageMembers])
}

func TestTeamServiceAdminAddMemberCreatesActiveMembership(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	member, err := svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleDeveloper, AdminUserID: 1})
	require.NoError(t, err)
	require.Equal(t, int64(8), member.UserID)
	require.Equal(t, domain.TeamRoleDeveloper, member.Role)
	require.Equal(t, domain.TeamMemberStatusActive, member.Status)
	require.NotNil(t, member.InvitedByUserID)
	require.Equal(t, int64(1), *member.InvitedByUserID)
}

func TestTeamServiceAdminAddMemberRejectsOwnerRole(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleOwner, AdminUserID: 1})
	require.ErrorIs(t, err, ErrTeamInvalidRole)
}

func TestTeamServiceAdminAddMemberRejectsDuplicateActive(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleViewer, AdminUserID: 1})
	require.NoError(t, err)

	// Adding the same active member again is rejected.
	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleDeveloper, AdminUserID: 1})
	require.ErrorIs(t, err, ErrTeamMemberExists)
}

func TestTeamServiceAdminAddMemberReactivatesLeftMembership(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	first, err := svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleViewer, AdminUserID: 1})
	require.NoError(t, err)

	// Remove (soft-delete) then re-add: should reactivate the same row, not create a duplicate.
	require.NoError(t, svc.AdminRemoveMember(context.Background(), 1, team.ID, 8))

	reactivated, err := svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleDeveloper, AdminUserID: 2})
	require.NoError(t, err)
	require.Equal(t, first.ID, reactivated.ID, "expected the soft-deleted row to be reactivated")
	require.Equal(t, domain.TeamRoleDeveloper, reactivated.Role)
	require.Equal(t, domain.TeamMemberStatusActive, reactivated.Status)

	// Only one active membership for user 8 should exist (the reactivated row).
	active := 0
	total := 0
	for _, m := range repo.members[team.ID] {
		if m.UserID == 8 {
			total++
			if m.Status == domain.TeamMemberStatusActive {
				active++
			}
		}
	}
	require.Equal(t, 1, active)
	require.Equal(t, 1, total, "expected the soft-deleted row to be reused, not duplicated")
}

// teamUserLookupStub implements the narrow TeamUserLookup dependency for admin
// email/id-resolution tests.
type teamUserLookupStub struct {
	byEmail map[string]*User
	byID    map[int64]*User
}

func (s teamUserLookupStub) GetByEmail(_ context.Context, email string) (*User, error) {
	if u, ok := s.byEmail[email]; ok {
		return u, nil
	}
	return nil, ErrUserNotFound
}

func (s teamUserLookupStub) GetByID(_ context.Context, id int64) (*User, error) {
	if u, ok := s.byID[id]; ok {
		return u, nil
	}
	return nil, ErrUserNotFound
}

func TestTeamServiceAdminAddMemberResolvesByEmail(t *testing.T) {
	repo := newTeamRepoMemory()
	lookup := teamUserLookupStub{byEmail: map[string]*User{
		"dev@example.com": {ID: 55, Email: "dev@example.com", Username: "dev"},
	}}
	// Construct directly to inject the narrow lookup dependency.
	svc := &TeamService{repo: repo, userLookup: lookup}
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	member, err := svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, Email: "Dev@Example.com", Role: domain.TeamRoleBilling, AdminUserID: 1})
	require.NoError(t, err)
	require.Equal(t, int64(55), member.UserID)
	require.Equal(t, domain.TeamRoleBilling, member.Role)

	// Unknown email yields not-found.
	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, Email: "ghost@example.com", Role: domain.TeamRoleViewer, AdminUserID: 1})
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestTeamServiceAdminAddMemberMissingUserAndEmail(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, Role: domain.TeamRoleViewer, AdminUserID: 1})
	require.Error(t, err)
}

func TestTeamServiceAdminUpdateMemberProtectsOwner(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	// The owner (user 7) cannot be modified through the admin member API.
	dev := domain.TeamRoleDeveloper
	_, err = svc.AdminUpdateMember(context.Background(), 1, team.ID, 7, UpdateTeamMemberInput{Role: &dev})
	require.ErrorIs(t, err, ErrTeamOwnerImmutable)
}

func TestTeamServiceAdminUpdateMemberValidations(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)
	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleViewer, AdminUserID: 1})
	require.NoError(t, err)

	// Empty update rejected.
	_, err = svc.AdminUpdateMember(context.Background(), 1, team.ID, 8, UpdateTeamMemberInput{})
	require.Error(t, err)

	// owner role rejected.
	owner := domain.TeamRoleOwner
	_, err = svc.AdminUpdateMember(context.Background(), 1, team.ID, 8, UpdateTeamMemberInput{Role: &owner})
	require.ErrorIs(t, err, ErrTeamInvalidRole)

	// valid status update succeeds.
	suspended := domain.TeamMemberStatusSuspended
	member, err := svc.AdminUpdateMember(context.Background(), 1, team.ID, 8, UpdateTeamMemberInput{Status: &suspended})
	require.NoError(t, err)
	require.Equal(t, domain.TeamMemberStatusSuspended, member.Status)
}

func TestTeamServiceAdminRemoveMemberProtectsOwner(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	err = svc.AdminRemoveMember(context.Background(), 1, team.ID, 7)
	require.ErrorIs(t, err, ErrTeamOwnerImmutable)
}

func TestTeamServiceAdminUpdateTeamValidations(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	// Empty update rejected.
	_, err = svc.AdminUpdateTeam(context.Background(), team.ID, AdminUpdateTeamInput{})
	require.Error(t, err)

	// Invalid status rejected.
	bad := "frozen"
	_, err = svc.AdminUpdateTeam(context.Background(), team.ID, AdminUpdateTeamInput{Status: &bad})
	require.Error(t, err)

	// Valid update succeeds.
	name := "Operations"
	disabled := domain.TeamStatusDisabled
	summary, err := svc.AdminUpdateTeam(context.Background(), team.ID, AdminUpdateTeamInput{Name: &name, Status: &disabled})
	require.NoError(t, err)
	require.Equal(t, "Operations", summary.Name)
	require.Equal(t, domain.TeamStatusDisabled, summary.Status)
}

func TestTeamServiceAdminListAndGetTeam(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)
	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleViewer, AdminUserID: 1})
	require.NoError(t, err)

	summaries, result, err := svc.AdminListTeams(context.Background(), AdminTeamListFilter{}, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, int64(1), result.Total)
	require.Equal(t, 2, summaries[0].MemberCount) // owner + added member

	got, members, _, err := svc.AdminGetTeam(context.Background(), team.ID)
	require.NoError(t, err)
	require.Equal(t, team.ID, got.ID)
	require.Len(t, members, 2)
}
