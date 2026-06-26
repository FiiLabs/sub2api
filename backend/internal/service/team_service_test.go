package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type teamRepoMemory struct {
	teams       map[int64]*Team
	members     map[int64][]TeamMember
	invitations map[int64]*TeamInvitation
	nextID      int64
}

func newTeamRepoMemory() *teamRepoMemory {
	return &teamRepoMemory{
		teams:       map[int64]*Team{},
		members:     map[int64][]TeamMember{},
		invitations: map[int64]*TeamInvitation{},
		nextID:      1,
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
	inv := &TeamInvitation{
		ID:              id,
		TeamID:          input.TeamID,
		Email:           input.Email,
		Role:            input.Role,
		TokenHash:       input.TokenHash,
		Status:          domain.TeamInvitationStatusPending,
		InvitedByUserID: input.ActorUserID,
		ExpiresAt:       input.ExpiresAt,
	}
	r.invitations[id] = inv
	return inv, nil
}

func (r *teamRepoMemory) GetInvitationByTokenHash(ctx context.Context, tokenHash string) (*TeamInvitation, error) {
	for _, inv := range r.invitations {
		if inv.TokenHash == tokenHash && inv.Status == domain.TeamInvitationStatusPending {
			cp := *inv
			return &cp, nil
		}
	}
	return nil, ErrTeamInvitationInvalid
}

func (r *teamRepoMemory) AcceptInvitation(ctx context.Context, invitationID, acceptingUserID, teamID int64, role string) (*TeamMember, error) {
	inv, ok := r.invitations[invitationID]
	if !ok {
		return nil, ErrTeamInvitationInvalid
	}
	// Idempotency: same user re-accepting an accepted invitation returns the
	// existing active membership.
	if inv.Status != domain.TeamInvitationStatusPending {
		if inv.Status == domain.TeamInvitationStatusAccepted &&
			inv.AcceptedByUserID != nil && *inv.AcceptedByUserID == acceptingUserID {
			for _, m := range r.members[teamID] {
				if m.UserID == acceptingUserID && m.Status == domain.TeamMemberStatusActive {
					mm := m
					return &mm, nil
				}
			}
			return nil, ErrTeamMembershipNotFound
		}
		return nil, ErrTeamInvitationExpired
	}

	invitedBy := inv.InvitedByUserID
	members := r.members[teamID]
	// Reactivate an existing active row (return as-is) or a left row, else insert.
	for i := range members {
		if members[i].UserID == acceptingUserID && members[i].Status == domain.TeamMemberStatusActive {
			r.markInvitationAccepted(inv, acceptingUserID)
			mm := members[i]
			return &mm, nil
		}
	}
	for i := len(members) - 1; i >= 0; i-- {
		if members[i].UserID == acceptingUserID && members[i].Status == domain.TeamMemberStatusLeft {
			members[i].Role = role
			members[i].Status = domain.TeamMemberStatusActive
			members[i].JoinedAt = teamTestPtrTime(time.Now())
			if invitedBy > 0 {
				ib := invitedBy
				members[i].InvitedByUserID = &ib
			}
			r.members[teamID] = members
			r.markInvitationAccepted(inv, acceptingUserID)
			mm := members[i]
			return &mm, nil
		}
	}
	id := r.nextID
	r.nextID++
	member := TeamMember{
		ID:       id,
		TeamID:   teamID,
		UserID:   acceptingUserID,
		Role:     role,
		Status:   domain.TeamMemberStatusActive,
		JoinedAt: teamTestPtrTime(time.Now()),
	}
	if invitedBy > 0 {
		ib := invitedBy
		member.InvitedByUserID = &ib
	}
	r.members[teamID] = append(members, member)
	r.markInvitationAccepted(inv, acceptingUserID)
	return &member, nil
}

func (r *teamRepoMemory) markInvitationAccepted(inv *TeamInvitation, acceptingUserID int64) {
	inv.Status = domain.TeamInvitationStatusAccepted
	uid := acceptingUserID
	inv.AcceptedByUserID = &uid
}

func (r *teamRepoMemory) TransferOwnership(ctx context.Context, teamID, newOwnerUserID, prevOwnerUserID int64) error {
	members := r.members[teamID]
	foundNew := false
	for i := range members {
		if members[i].UserID == newOwnerUserID && members[i].Status == domain.TeamMemberStatusActive {
			foundNew = true
		}
	}
	if !foundNew {
		return ErrTeamMembershipNotFound
	}
	for i := range members {
		if members[i].UserID == newOwnerUserID {
			members[i].Role = domain.TeamRoleOwner
		} else if members[i].UserID == prevOwnerUserID && prevOwnerUserID != newOwnerUserID {
			members[i].Role = domain.TeamRoleAdmin
		}
	}
	r.members[teamID] = members
	if team, ok := r.teams[teamID]; ok {
		team.OwnerUserID = newOwnerUserID
	}
	return nil
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

// CountActiveAPIKeysByBillingSubjectID 内存桩不跟踪 API Key，恒返回 0。
func (r *teamRepoMemory) CountActiveAPIKeysByBillingSubjectID(ctx context.Context, billingSubjectID int64) (int, error) {
	return 0, nil
}

// CountActiveTeamsByOwner 统计内存桩中该 owner 的活跃团队数（status=active；内存桩中已解散的团队从 map 删除，故无需检查软删标记）。
func (r *teamRepoMemory) CountActiveTeamsByOwner(ctx context.Context, ownerUserID int64) (int, error) {
	n := 0
	for _, tm := range r.teams {
		if tm.OwnerUserID == ownerUserID && tm.Status == domain.TeamStatusActive {
			n++
		}
	}
	return n, nil
}

// DissolveTeam 内存桩解散：成员标记为 left（与 RemoveMember 一致的软删标记），
// 团队从 map 移除（模拟软删后不可再查到）。
func (r *teamRepoMemory) DissolveTeam(ctx context.Context, teamID int64) error {
	if _, ok := r.teams[teamID]; !ok {
		return ErrTeamNotFound
	}
	members := r.members[teamID]
	for i := range members {
		members[i].Status = domain.TeamMemberStatusLeft
	}
	r.members[teamID] = members
	delete(r.teams, teamID)
	return nil
}

// UsageByMember 内存桩不维护用量数据，恒返回空切片。
func (r *teamRepoMemory) UsageByMember(_ context.Context, _ int64, _, _ time.Time) ([]TeamMemberUsage, error) {
	return nil, nil
}

func teamTestPtrTime(t time.Time) *time.Time { return &t }

func TestTeamServiceListMembersRequiresViewPermission(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
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
	svc := NewTeamService(repo, nil, nil, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	_, _, _, err = svc.InviteMember(context.Background(), InviteTeamMemberInput{ActorUserID: 7, TeamID: team.ID, Email: "x@example.com", Role: domain.TeamRoleOwner, ExpiresAt: time.Now().Add(time.Hour)})
	require.ErrorIs(t, err, ErrTeamInvalidRole)

	invitation, token, acceptLink, err := svc.InviteMember(context.Background(), InviteTeamMemberInput{ActorUserID: 7, TeamID: team.ID, Email: "X@Example.com", Role: domain.TeamRoleDeveloper, ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Equal(t, "x@example.com", invitation.Email)
	require.NotEmpty(t, invitation.TokenHash)
	// With no notifier configured the link is a relative path carrying the token.
	require.Contains(t, acceptLink, "/teams/accept?token=")
}

func TestTeamServiceUpdateMemberValidations(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
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
	svc := NewTeamService(repo, nil, nil, nil, nil)

	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 42, Name: "Platform Team", Slug: "platform-team"})
	require.NoError(t, err)
	require.Equal(t, int64(1), team.ID)

	member, err := repo.GetMembership(context.Background(), team.ID, 42)
	require.NoError(t, err)
	require.Equal(t, domain.TeamRoleOwner, member.Role)
}

func TestTeamServiceCreateTeamAutoGeneratesSlug(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)

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
	svc := NewTeamService(repo, nil, nil, nil, nil)

	// All-non-ASCII name slugifies to "" -> falls back to "team-<suffix>".
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "团队名称"})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(team.Slug, "team-"), "got slug %q", team.Slug)
	require.Equal(t, len("team-")+6, len(team.Slug))
}

func TestTeamServiceCreateTeamRequiresName(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)

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
	svc := NewTeamService(repo, nil, nil, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Billing Team", Slug: "billing-team"})
	require.NoError(t, err)

	can, err := svc.Can(context.Background(), 7, team.ID, domain.TeamPermissionManageBilling)
	require.NoError(t, err)
	require.True(t, can)

	can, err = svc.Can(context.Background(), 7, team.ID, domain.TeamPermissionDissolveTeam)
	require.NoError(t, err)
	require.True(t, can)
}

func TestTeamServiceListWorkspacesIncludesPersonalAndTeams(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
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
	svc := NewTeamService(repo, nil, nil, nil, nil)
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
	svc := NewTeamService(repo, nil, nil, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleOwner, AdminUserID: 1})
	require.ErrorIs(t, err, ErrTeamInvalidRole)
}

func TestTeamServiceAdminAddMemberRejectsDuplicateActive(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
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
	svc := NewTeamService(repo, nil, nil, nil, nil)
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
	svc := NewTeamService(repo, nil, nil, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, Role: domain.TeamRoleViewer, AdminUserID: 1})
	require.Error(t, err)
}

func TestTeamServiceAdminUpdateMemberProtectsOwner(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	// The owner (user 7) cannot be modified through the admin member API.
	dev := domain.TeamRoleDeveloper
	_, err = svc.AdminUpdateMember(context.Background(), 1, team.ID, 7, UpdateTeamMemberInput{Role: &dev})
	require.ErrorIs(t, err, ErrTeamOwnerImmutable)
}

func TestTeamServiceAdminUpdateMemberValidations(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
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
	svc := NewTeamService(repo, nil, nil, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	err = svc.AdminRemoveMember(context.Background(), 1, team.ID, 7)
	require.ErrorIs(t, err, ErrTeamOwnerImmutable)
}

func TestTeamServiceAdminUpdateTeamValidations(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
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
	svc := NewTeamService(repo, nil, nil, nil, nil)
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

// --- Invitation accept / preview ---------------------------------------------

// inviteTestSetup creates a team (owner=ownerID) and a pending invitation for
// inviteEmail/role, returning the plaintext token and the service wired with a
// lookup that resolves inviteEmail -> inviteeID.
func inviteTestSetup(t *testing.T, ownerID, inviteeID int64, inviteEmail, role string) (*TeamService, *teamRepoMemory, string, int64) {
	t.Helper()
	repo := newTeamRepoMemory()
	lookup := teamUserLookupStub{byID: map[int64]*User{
		inviteeID: {ID: inviteeID, Email: inviteEmail, Username: "invitee"},
		ownerID:   {ID: ownerID, Email: "owner@example.com", Username: "owner"},
	}}
	svc := &TeamService{repo: repo, userLookup: lookup}
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: ownerID, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)
	_, token, _, err := svc.InviteMember(context.Background(), InviteTeamMemberInput{
		ActorUserID: ownerID, TeamID: team.ID, Email: inviteEmail, Role: role, ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	return svc, repo, token, team.ID
}

func TestTeamServiceAcceptInvitationCreatesMembership(t *testing.T) {
	svc, repo, token, teamID := inviteTestSetup(t, 7, 8, "invitee@example.com", domain.TeamRoleDeveloper)

	member, err := svc.AcceptInvitation(context.Background(), 8, token)
	require.NoError(t, err)
	require.Equal(t, int64(8), member.UserID)
	require.Equal(t, domain.TeamRoleDeveloper, member.Role)
	require.Equal(t, domain.TeamMemberStatusActive, member.Status)
	// invited_by is the inviter (owner 7).
	require.NotNil(t, member.InvitedByUserID)
	require.Equal(t, int64(7), *member.InvitedByUserID)

	// Membership is active and the invitation is now accepted.
	got, err := repo.GetMembership(context.Background(), teamID, 8)
	require.NoError(t, err)
	require.Equal(t, domain.TeamRoleDeveloper, got.Role)
	for _, inv := range repo.invitations {
		require.Equal(t, domain.TeamInvitationStatusAccepted, inv.Status)
		require.NotNil(t, inv.AcceptedByUserID)
		require.Equal(t, int64(8), *inv.AcceptedByUserID)
	}
}

func TestTeamServiceAcceptInvitationRejectsEmailMismatch(t *testing.T) {
	repo := newTeamRepoMemory()
	// The accepting user's email differs from the invited email.
	lookup := teamUserLookupStub{byID: map[int64]*User{
		8: {ID: 8, Email: "someone-else@example.com", Username: "other"},
		7: {ID: 7, Email: "owner@example.com", Username: "owner"},
	}}
	svc := &TeamService{repo: repo, userLookup: lookup}
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)
	_, token, _, err := svc.InviteMember(context.Background(), InviteTeamMemberInput{
		ActorUserID: 7, TeamID: team.ID, Email: "invitee@example.com", Role: domain.TeamRoleViewer, ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	_, err = svc.AcceptInvitation(context.Background(), 8, token)
	require.ErrorIs(t, err, ErrTeamInvitationEmailMismatch)

	// No membership was created for user 8.
	_, err = repo.GetMembership(context.Background(), team.ID, 8)
	require.Error(t, err)
}

func TestTeamServiceAcceptInvitationRejectsExpired(t *testing.T) {
	repo := newTeamRepoMemory()
	lookup := teamUserLookupStub{byID: map[int64]*User{
		8: {ID: 8, Email: "invitee@example.com", Username: "invitee"},
		7: {ID: 7, Email: "owner@example.com", Username: "owner"},
	}}
	svc := &TeamService{repo: repo, userLookup: lookup}
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)
	_, token, _, err := svc.InviteMember(context.Background(), InviteTeamMemberInput{
		ActorUserID: 7, TeamID: team.ID, Email: "invitee@example.com", Role: domain.TeamRoleViewer,
		ExpiresAt: time.Now().Add(-time.Hour), // already expired
	})
	require.NoError(t, err)

	_, err = svc.AcceptInvitation(context.Background(), 8, token)
	require.ErrorIs(t, err, ErrTeamInvitationExpired)
}

func TestTeamServiceAcceptInvitationIsIdempotent(t *testing.T) {
	svc, repo, token, teamID := inviteTestSetup(t, 7, 8, "invitee@example.com", domain.TeamRoleDeveloper)

	first, err := svc.AcceptInvitation(context.Background(), 8, token)
	require.NoError(t, err)

	// Re-accepting through the service: the token no longer resolves to a PENDING
	// invitation (GetInvitationByTokenHash returns pending-only), so the second
	// service call is rejected as invalid rather than mutating anything. The point
	// is that it is harmless and creates no duplicate membership. (Repo-level
	// idempotency for the already-accepted-by-same-user case is covered by the
	// repository test suite.)
	_, err = svc.AcceptInvitation(context.Background(), 8, token)
	require.Error(t, err)

	// The first accept stands and there is exactly one membership row for user 8.
	got, err := repo.GetMembership(context.Background(), teamID, 8)
	require.NoError(t, err)
	require.Equal(t, first.UserID, got.UserID)
	count := 0
	for _, m := range repo.members[teamID] {
		if m.UserID == 8 {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestTeamServiceAcceptInvitationReactivatesLeftMember(t *testing.T) {
	svc, repo, _, teamID := inviteTestSetup(t, 7, 8, "invitee@example.com", domain.TeamRoleViewer)

	// User 8 was previously a member who left.
	repo.members[teamID] = append(repo.members[teamID], TeamMember{
		ID: 999, TeamID: teamID, UserID: 8, Role: domain.TeamRoleViewer, Status: domain.TeamMemberStatusLeft,
	})

	// A fresh invitation (developer role) is accepted -> reactivates the left row.
	_, token2, _, err := svc.InviteMember(context.Background(), InviteTeamMemberInput{
		ActorUserID: 7, TeamID: teamID, Email: "invitee@example.com", Role: domain.TeamRoleDeveloper, ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	member, err := svc.AcceptInvitation(context.Background(), 8, token2)
	require.NoError(t, err)
	require.Equal(t, domain.TeamRoleDeveloper, member.Role)
	require.Equal(t, domain.TeamMemberStatusActive, member.Status)

	// The left row was reactivated, not duplicated.
	total := 0
	active := 0
	for _, m := range repo.members[teamID] {
		if m.UserID == 8 {
			total++
			if m.Status == domain.TeamMemberStatusActive {
				active++
			}
		}
	}
	require.Equal(t, 1, total)
	require.Equal(t, 1, active)
}

func TestTeamServicePreviewInvitationNoMutation(t *testing.T) {
	svc, repo, token, teamID := inviteTestSetup(t, 7, 8, "invitee@example.com", domain.TeamRoleDeveloper)

	preview, err := svc.PreviewInvitation(context.Background(), token)
	require.NoError(t, err)
	require.Equal(t, teamID, preview.TeamID)
	require.Equal(t, "Ops", preview.TeamName)
	require.Equal(t, domain.TeamRoleDeveloper, preview.Role)
	require.Equal(t, "invitee@example.com", preview.Email)
	require.Equal(t, domain.TeamInvitationStatusPending, preview.Status)
	require.False(t, preview.Expired)

	// No mutation: invitation still pending, no membership created.
	for _, inv := range repo.invitations {
		require.Equal(t, domain.TeamInvitationStatusPending, inv.Status)
		require.Nil(t, inv.AcceptedByUserID)
	}
	_, err = repo.GetMembership(context.Background(), teamID, 8)
	require.Error(t, err)

	// Unknown token -> invalid.
	_, err = svc.PreviewInvitation(context.Background(), "deadbeef-not-a-real-token")
	require.ErrorIs(t, err, ErrTeamInvitationInvalid)
}

// --- Ownership transfer -------------------------------------------------------

func TestTeamServiceTransferOwnershipSwapsRoles(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)
	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleDeveloper, AdminUserID: 7})
	require.NoError(t, err)

	require.NoError(t, svc.TransferOwnership(context.Background(), 7, team.ID, 8))

	// owner_user_id updated.
	got, err := repo.GetTeamByID(context.Background(), team.ID)
	require.NoError(t, err)
	require.Equal(t, int64(8), got.OwnerUserID)

	// New owner is owner; previous owner demoted to admin.
	newOwner, err := repo.GetMembership(context.Background(), team.ID, 8)
	require.NoError(t, err)
	require.Equal(t, domain.TeamRoleOwner, newOwner.Role)
	prevOwner, err := repo.GetMembership(context.Background(), team.ID, 7)
	require.NoError(t, err)
	require.Equal(t, domain.TeamRoleAdmin, prevOwner.Role)
}

func TestTeamServiceTransferOwnershipRejectsNonOwner(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)
	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleAdmin, AdminUserID: 7})
	require.NoError(t, err)
	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 9, Role: domain.TeamRoleDeveloper, AdminUserID: 7})
	require.NoError(t, err)

	// User 8 (admin, not owner) cannot transfer ownership.
	err = svc.TransferOwnership(context.Background(), 8, team.ID, 9)
	require.ErrorIs(t, err, ErrTeamPermissionDenied)

	// Self-transfer rejected.
	err = svc.TransferOwnership(context.Background(), 7, team.ID, 7)
	require.Error(t, err)
}

func TestTeamServiceTransferOwnershipRequiresActiveNewOwner(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	// User 8 is not a member at all.
	err = svc.TransferOwnership(context.Background(), 7, team.ID, 8)
	require.ErrorIs(t, err, ErrTeamMembershipNotFound)
}

func TestTeamServiceAdminTransferOwnershipBypassesGating(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)
	_, err = svc.AdminAddMember(context.Background(), AdminAddMemberInput{TeamID: team.ID, UserID: 8, Role: domain.TeamRoleDeveloper, AdminUserID: 7})
	require.NoError(t, err)

	// Admin (no membership) transfers ownership to active member 8.
	require.NoError(t, svc.AdminTransferOwnership(context.Background(), team.ID, 8))
	got, err := repo.GetTeamByID(context.Background(), team.ID)
	require.NoError(t, err)
	require.Equal(t, int64(8), got.OwnerUserID)

	// New owner must still be an active member.
	err = svc.AdminTransferOwnership(context.Background(), team.ID, 999)
	require.ErrorIs(t, err, ErrTeamMembershipNotFound)
}

// --- Invitation email notifier -----------------------------------------------

type inviteNotifierSpy struct {
	called    bool
	toEmail   string
	link      string
	teamName  string
	returnErr error
}

func (s *inviteNotifierSpy) SendInvite(_ context.Context, toEmail, acceptLink, teamName string) error {
	s.called = true
	s.toEmail = toEmail
	s.link = acceptLink
	s.teamName = teamName
	return s.returnErr
}

// inviteNotifierSpyWithBase additionally provides a base URL so the built accept
// link is absolute (exercising buildInvitationAcceptLink's AcceptBaseURL branch).
type inviteNotifierSpyWithBase struct {
	inviteNotifierSpy
	base string
}

func (s *inviteNotifierSpyWithBase) AcceptBaseURL(_ context.Context) string { return s.base }

func TestTeamServiceInviteMemberInvokesNotifier(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
	_, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)
	spy := &inviteNotifierSpyWithBase{base: "https://app.example.com"}
	svc.SetInviteNotifier(spy)

	invitation, token, acceptLink, err := svc.InviteMember(context.Background(), InviteTeamMemberInput{
		ActorUserID: 7, TeamID: 1, Email: "Invitee@Example.com", Role: domain.TeamRoleDeveloper, ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	require.NotNil(t, invitation)

	require.True(t, spy.called)
	require.Equal(t, "invitee@example.com", spy.toEmail) // normalized
	require.Equal(t, "Ops", spy.teamName)
	// The link returned to the caller matches the link handed to the notifier and is
	// absolute (base URL + path + token).
	require.Equal(t, acceptLink, spy.link)
	require.Equal(t, "https://app.example.com/teams/accept?token="+token, acceptLink)
}

func TestTeamServiceInviteMemberNotifierErrorIsNonFatal(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
	_, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)
	spy := &inviteNotifierSpy{returnErr: ErrEmailNotConfigured}
	svc.SetInviteNotifier(spy)

	invitation, token, acceptLink, err := svc.InviteMember(context.Background(), InviteTeamMemberInput{
		ActorUserID: 7, TeamID: 1, Email: "invitee@example.com", Role: domain.TeamRoleViewer, ExpiresAt: time.Now().Add(time.Hour),
	})
	// Delivery failure must NOT fail the invite.
	require.NoError(t, err)
	require.NotNil(t, invitation)
	require.NotEmpty(t, token)
	require.True(t, spy.called)
	// With no base URL the link is a relative path.
	require.Equal(t, "/teams/accept?token="+token, acceptLink)
}

// --- AdminUpdateTeam concurrency/rpm tests -----------------------------------

// adminUpdateRepoStub 内嵌 teamRepoMemory，覆写 AdminGetTeamSummary 以注入非 nil 的 BillingSubjectID。
type adminUpdateRepoStub struct {
	*teamRepoMemory
	billingSubjectID int64
}

func (s *adminUpdateRepoStub) AdminGetTeamSummary(ctx context.Context, teamID int64) (*AdminTeamSummary, error) {
	summary, err := s.teamRepoMemory.AdminGetTeamSummary(ctx, teamID)
	if err != nil {
		return nil, err
	}
	summary.BillingSubjectID = &s.billingSubjectID
	return summary, nil
}

// updateLimitsBillingStub 内嵌 BillingSubjectRepository，记录 UpdateLimits 的调用参数。
type updateLimitsBillingStub struct {
	BillingSubjectRepository
	calledSubjectID int64
	calledConc      int
	calledRpm       int
}

func (s *updateLimitsBillingStub) UpdateLimits(_ context.Context, subjectID int64, concurrency, rpmLimit int) error {
	s.calledSubjectID = subjectID
	s.calledConc = concurrency
	s.calledRpm = rpmLimit
	return nil
}

func TestTeamServiceAdminUpdateTeamConcurrencyCallsUpdateLimits(t *testing.T) {
	base := newTeamRepoMemory()
	repo := &adminUpdateRepoStub{teamRepoMemory: base, billingSubjectID: 42}
	billingStub := &updateLimitsBillingStub{}
	svc := NewTeamService(repo, billingStub, nil, nil, nil)

	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Ops", Slug: "ops"})
	require.NoError(t, err)

	conc := 20
	_, err = svc.AdminUpdateTeam(context.Background(), team.ID, AdminUpdateTeamInput{Concurrency: &conc})
	require.NoError(t, err)

	require.Equal(t, int64(42), billingStub.calledSubjectID)
	require.Equal(t, 20, billingStub.calledConc)
	require.Equal(t, 0, billingStub.calledRpm)
}

// --- Owner cap tests ----------------------------------------------------------

func TestTeamService_CreateTeam_OwnerCapEnforced(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil, nil, nil)
	owner := int64(7)
	for i := 0; i < MaxTeamsPerOwner; i++ {
		_, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: owner, Name: fmt.Sprintf("T%d", i)})
		require.NoError(t, err)
	}
	// 第 MaxTeamsPerOwner+1 个应被拦截
	_, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: owner, Name: "overflow"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "TEAM_LIMIT_REACHED")
}

func TestTeamService_AdminCreateTeam_BypassesOwnerCap(t *testing.T) {
	repo := newTeamRepoMemory()
	lookup := teamUserLookupStub{byID: map[int64]*User{
		8: {ID: 8, Email: "owner@example.com", Username: "owner"},
	}}
	svc := &TeamService{repo: repo, userLookup: lookup}
	owner := int64(8)
	for i := 0; i < MaxTeamsPerOwner; i++ {
		_, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: owner, Name: fmt.Sprintf("S%d", i)})
		require.NoError(t, err)
	}
	// admin 代建第 6 个、指派同一 owner —— 不受上限影响
	_, err := svc.AdminCreateTeam(context.Background(), AdminCreateTeamInput{AdminUserID: 1, OwnerUserID: owner, Name: "admin-extra"})
	require.NoError(t, err)
}

// --- AdminAdjustTeamBalance tests -----------------------------------------------

// adminBalanceRepoStub 内嵌 teamRepoMemory，覆写 AdminGetTeamSummary 以注入
// BillingSubjectID 和 Balance（这样 AdminAdjustTeamBalance 能读到初始余额并在最
// 终重读时返回更新后的余额）。subjectID=0 表示无主体（返回 BillingSubjectID=nil）。
type adminBalanceRepoStub struct {
	*teamRepoMemory
	billingSubjectID int64
	// balance 是 AdminGetTeamSummary 返回的当前余额，在 UpdateBalance 被调用后由
	// fakeBillingSubjectForBalance 通过回调更新。
	mu      sync.Mutex
	balance float64
}

func newAdminBalanceRepoStub(base *teamRepoMemory, subjectID int64, balance float64) *adminBalanceRepoStub {
	return &adminBalanceRepoStub{teamRepoMemory: base, billingSubjectID: subjectID, balance: balance}
}

func (s *adminBalanceRepoStub) AdminGetTeamSummary(ctx context.Context, teamID int64) (*AdminTeamSummary, error) {
	summary, err := s.teamRepoMemory.AdminGetTeamSummary(ctx, teamID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.billingSubjectID > 0 {
		sid := s.billingSubjectID
		summary.BillingSubjectID = &sid
	} else {
		summary.BillingSubjectID = nil
	}
	summary.Balance = s.balance
	return summary, nil
}

func (s *adminBalanceRepoStub) applyDelta(delta float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.balance += delta
}

// fakeBillingSubjectForBalance 记录 UpdateBalance 的累计 delta，并将变化同步回 repo stub。
type fakeBillingSubjectForBalance struct {
	BillingSubjectRepository
	mu      sync.Mutex
	deltas  map[int64]float64
	onDelta func(subjectID int64, delta float64)
}

func newFakeBillingSubjectForBalance() *fakeBillingSubjectForBalance {
	return &fakeBillingSubjectForBalance{deltas: make(map[int64]float64)}
}

func (f *fakeBillingSubjectForBalance) UpdateBalance(_ context.Context, subjectID int64, delta float64) error {
	f.mu.Lock()
	f.deltas[subjectID] += delta
	f.mu.Unlock()
	if f.onDelta != nil {
		f.onDelta(subjectID, delta)
	}
	return nil
}

func (f *fakeBillingSubjectForBalance) lastDelta(subjectID int64) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deltas[subjectID]
}

// fakeRedeemCodeRepoForBalance 记录 Create 调用的关键字段。
type fakeRedeemCodeRepoForBalance struct {
	RedeemCodeRepository
	mu                   sync.Mutex
	created              int
	lastType             string
	lastBillingSubjectID *int64
	lastUsedBy           *int64
}

func newFakeRedeemCodeRepoForBalance() *fakeRedeemCodeRepoForBalance {
	return &fakeRedeemCodeRepoForBalance{}
}

func (f *fakeRedeemCodeRepoForBalance) Create(_ context.Context, code *RedeemCode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created++
	f.lastType = code.Type
	f.lastUsedBy = code.UsedBy
	if code.BillingSubjectID != nil {
		sid := *code.BillingSubjectID
		f.lastBillingSubjectID = &sid
	} else {
		f.lastBillingSubjectID = nil
	}
	return nil
}

// fakeSubjectBalanceCacheForBalance 线程安全地记录被失效的 subjectID。
type fakeSubjectBalanceCacheForBalance struct {
	mu          sync.Mutex
	invalidated map[int64]bool
}

func newFakeSubjectBalanceCacheForBalance() *fakeSubjectBalanceCacheForBalance {
	return &fakeSubjectBalanceCacheForBalance{invalidated: make(map[int64]bool)}
}

func (f *fakeSubjectBalanceCacheForBalance) InvalidateSubjectBalance(_ context.Context, subjectID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalidated[subjectID] = true
	return nil
}

func (f *fakeSubjectBalanceCacheForBalance) wasInvalidated(subjectID int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.invalidated[subjectID]
}

func TestTeamService_AdminAdjustTeamBalance_AddAndAudit(t *testing.T) {
	base := newTeamRepoMemory()
	_, err := base.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 1, Name: "T10"})
	require.NoError(t, err)
	teamID := int64(1)

	repo := newAdminBalanceRepoStub(base, 500, 20.0)
	bs := newFakeBillingSubjectForBalance()
	// 让 bs.UpdateBalance 把变化同步回 repo stub（这样二次读返回新余额）
	bs.onDelta = func(subjectID int64, delta float64) { repo.applyDelta(delta) }
	redeem := newFakeRedeemCodeRepoForBalance()
	cache := newFakeSubjectBalanceCacheForBalance()
	svc := NewTeamService(repo, bs, nil, redeem, cache)

	sum, err := svc.AdminAdjustTeamBalance(context.Background(), teamID, 5.0, "add", "topup")
	require.NoError(t, err)
	require.InDelta(t, 25.0, sum.Balance, 1e-9)
	require.InDelta(t, 5.0, bs.lastDelta(500), 1e-9) // delta = +5
	redeem.mu.Lock()
	created := redeem.created
	lastType := redeem.lastType
	lastBSID := redeem.lastBillingSubjectID
	lastUsedBy := redeem.lastUsedBy
	redeem.mu.Unlock()
	require.Equal(t, 1, created)                              // 审计写入
	require.Equal(t, AdjustmentTypeAdminBalance, lastType)
	require.NotNil(t, lastBSID)
	require.Equal(t, int64(500), *lastBSID)
	require.Nil(t, lastUsedBy)
	require.Eventually(t, func() bool { return cache.wasInvalidated(500) }, time.Second, 10*time.Millisecond)
}

func TestTeamService_AdminAdjustTeamBalance_SubtractBelowZeroRejected(t *testing.T) {
	base := newTeamRepoMemory()
	_, err := base.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 1, Name: "T11"})
	require.NoError(t, err)

	repo := newAdminBalanceRepoStub(base, 501, 3.0)
	bs := newFakeBillingSubjectForBalance()
	svc := NewTeamService(repo, bs, nil, newFakeRedeemCodeRepoForBalance(), newFakeSubjectBalanceCacheForBalance())

	_, err = svc.AdminAdjustTeamBalance(context.Background(), 1, 10.0, "subtract", "")
	require.Error(t, err) // 结果 3-10 = -7 < 0
}

func TestTeamService_AdminAdjustTeamBalance_SetAndNoSubjectGuard(t *testing.T) {
	// set 到绝对值
	base := newTeamRepoMemory()
	_, err := base.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 1, Name: "T12"})
	require.NoError(t, err)

	repo := newAdminBalanceRepoStub(base, 502, 8.0)
	bs := newFakeBillingSubjectForBalance()
	bs.onDelta = func(subjectID int64, delta float64) { repo.applyDelta(delta) }
	svc := NewTeamService(repo, bs, nil, newFakeRedeemCodeRepoForBalance(), newFakeSubjectBalanceCacheForBalance())

	sum, err := svc.AdminAdjustTeamBalance(context.Background(), 1, 30.0, "set", "")
	require.NoError(t, err)
	require.InDelta(t, 30.0, sum.Balance, 1e-9)

	// 无计费主体的团队 → 报错
	base2 := newTeamRepoMemory()
	_, err = base2.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 2, Name: "T13"})
	require.NoError(t, err)
	repo2 := newAdminBalanceRepoStub(base2, 0 /*no subject*/, 0)
	svc2 := NewTeamService(repo2, newFakeBillingSubjectForBalance(), nil, newFakeRedeemCodeRepoForBalance(), newFakeSubjectBalanceCacheForBalance())
	_, err = svc2.AdminAdjustTeamBalance(context.Background(), 1, 1.0, "add", "")
	require.Error(t, err)
}
