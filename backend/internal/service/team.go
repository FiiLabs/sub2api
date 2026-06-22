package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrTeamNotFound           = infraerrors.NotFound("TEAM_NOT_FOUND", "team not found")
	ErrTeamMembershipNotFound = infraerrors.Forbidden("TEAM_MEMBERSHIP_NOT_FOUND", "team membership not found")
	ErrTeamPermissionDenied   = infraerrors.Forbidden("TEAM_PERMISSION_DENIED", "team permission denied")
	ErrTeamInvalidRole        = infraerrors.BadRequest("TEAM_INVALID_ROLE", "invalid team role")
	ErrTeamInvitationInvalid  = infraerrors.BadRequest("TEAM_INVITATION_INVALID", "team invitation is invalid")

	// ErrTeamMemberExists is returned by AdminAddMember when an active membership
	// already exists for the target (team, user).
	ErrTeamMemberExists = infraerrors.BadRequest("TEAM_MEMBER_EXISTS", "team member already exists")
	// ErrTeamOwnerImmutable is returned when an admin attempts to modify or remove
	// the team owner's membership (ownership is managed separately, not here).
	ErrTeamOwnerImmutable = infraerrors.BadRequest("TEAM_OWNER_IMMUTABLE", "team owner membership cannot be modified here")
)

type Team struct {
	ID               int64
	Name             string
	Slug             string
	OwnerUserID      int64
	BillingSubjectID *int64
	Status           string
	AvatarURL        string
	CreatedByUserID  int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type TeamMember struct {
	ID              int64
	TeamID          int64
	UserID          int64
	Role            string
	Status          string
	InvitedByUserID *int64
	JoinedAt        *time.Time
	LastActiveAt    *time.Time
	User            *User
}

type TeamInvitation struct {
	ID               int64
	TeamID           int64
	Email            string
	Role             string
	TokenHash        string
	Status           string
	InvitedByUserID  int64
	AcceptedByUserID *int64
	ExpiresAt        time.Time
}

type WorkspaceSubject struct {
	BillingSubjectID int64           `json:"billing_subject_id"`
	Type             string          `json:"type"`
	UserID           int64           `json:"user_id,omitempty"`
	TeamID           int64           `json:"team_id,omitempty"`
	Name             string          `json:"name"`
	Role             string          `json:"role"`
	Permissions      map[string]bool `json:"permissions"`
	Balance          float64         `json:"balance"`
}

type CreateTeamInput struct {
	ActorUserID int64
	Name        string
	Slug        string
}

type InviteTeamMemberInput struct {
	ActorUserID int64
	TeamID      int64
	Email       string
	Role        string
	TokenHash   string
	ExpiresAt   time.Time
}

type UpdateTeamMemberInput struct {
	Role   *string
	Status *string
}

// AdminTeamSummary is the platform-admin view of a team, enriched with the owner
// user, the team billing subject balance and the active member count.
type AdminTeamSummary struct {
	Team
	OwnerUser   *User
	Balance     float64
	MemberCount int
}

// AdminTeamListFilter holds the optional filters for AdminListTeams.
type AdminTeamListFilter struct {
	Search string // matches name OR slug (case-insensitive)
	Status string // optional: "active" / "disabled"
}

// AdminUpdateTeamInput holds the mutable team fields for AdminUpdateTeam.
type AdminUpdateTeamInput struct {
	Name   *string
	Status *string
}

// AdminAddMemberInput holds the input for AdminAddMember. The target user is
// resolved by UserID first, falling back to Email.
type AdminAddMemberInput struct {
	TeamID      int64
	UserID      int64
	Email       string
	Role        string
	AdminUserID int64
}

type TeamRepository interface {
	CreateTeam(ctx context.Context, input CreateTeamInput) (*Team, error)
	GetMembership(ctx context.Context, teamID, userID int64) (*TeamMember, error)
	ListWorkspaces(ctx context.Context, userID int64) ([]WorkspaceSubject, error)
	ListMembers(ctx context.Context, teamID int64) ([]TeamMember, []TeamInvitation, error)
	InviteMember(ctx context.Context, input InviteTeamMemberInput) (*TeamInvitation, error)
	UpdateMember(ctx context.Context, actorUserID, teamID, userID int64, input UpdateTeamMemberInput) (*TeamMember, error)
	RemoveMember(ctx context.Context, actorUserID, teamID, userID int64) error

	// Platform-admin operations (no team-membership gating).
	AdminListTeams(ctx context.Context, filter AdminTeamListFilter, params pagination.PaginationParams) ([]AdminTeamSummary, *pagination.PaginationResult, error)
	GetTeamByID(ctx context.Context, teamID int64) (*Team, error)
	AdminGetTeamSummary(ctx context.Context, teamID int64) (*AdminTeamSummary, error)
	AddMember(ctx context.Context, teamID, userID int64, role string, invitedByUserID int64) (*TeamMember, error)
	UpdateTeam(ctx context.Context, teamID int64, name *string, status *string) (*Team, error)
}

// TeamUserLookup is the narrow user-resolution dependency used by admin member
// management (resolving a target user by email). *userRepository (the concrete
// UserRepository implementation) satisfies it.
type TeamUserLookup interface {
	GetByEmail(ctx context.Context, email string) (*User, error)
}

type TeamService struct {
	repo           TeamRepository
	billingSubject BillingSubjectRepository
	userLookup     TeamUserLookup
}

func NewTeamService(repo TeamRepository, billingSubject BillingSubjectRepository, userRepo UserRepository) *TeamService {
	// userRepo (the full UserRepository) is stored behind the narrow TeamUserLookup
	// interface; accepting the full interface keeps wire injection binding-free,
	// while the narrow field keeps the admin email-resolution dependency minimal.
	s := &TeamService{repo: repo, billingSubject: billingSubject}
	if userRepo != nil {
		s.userLookup = userRepo
	}
	return s
}

func (s *TeamService) CreateTeam(ctx context.Context, input CreateTeamInput) (*Team, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Slug = strings.TrimSpace(input.Slug)
	if input.ActorUserID <= 0 || input.Name == "" || input.Slug == "" {
		return nil, infraerrors.BadRequest("TEAM_INVALID_INPUT", "team name and slug are required")
	}
	return s.repo.CreateTeam(ctx, input)
}

func (s *TeamService) Can(ctx context.Context, actorUserID, teamID int64, permission string) (bool, error) {
	member, err := s.repo.GetMembership(ctx, teamID, actorUserID)
	if err != nil {
		return false, err
	}
	if member.Status != domain.TeamMemberStatusActive {
		return false, ErrTeamMembershipNotFound
	}
	return domain.TeamRolePermissions(member.Role)[permission], nil
}

func (s *TeamService) Require(ctx context.Context, actorUserID, teamID int64, permission string) (*TeamMember, error) {
	member, err := s.repo.GetMembership(ctx, teamID, actorUserID)
	if err != nil {
		return nil, err
	}
	if member.Status != domain.TeamMemberStatusActive {
		return nil, ErrTeamMembershipNotFound
	}
	if !domain.TeamRolePermissions(member.Role)[permission] {
		return nil, ErrTeamPermissionDenied
	}
	return member, nil
}

func (s *TeamService) ListWorkspaces(ctx context.Context, userID int64) ([]WorkspaceSubject, error) {
	if userID <= 0 {
		return nil, infraerrors.Unauthorized("USER_NOT_AUTHENTICATED", "user not authenticated")
	}
	return s.repo.ListWorkspaces(ctx, userID)
}

func (s *TeamService) ListMembers(ctx context.Context, actorUserID, teamID int64) ([]TeamMember, []TeamInvitation, error) {
	if _, err := s.Require(ctx, actorUserID, teamID, domain.TeamPermissionViewUsage); err != nil {
		return nil, nil, err
	}
	return s.repo.ListMembers(ctx, teamID)
}

func (s *TeamService) InviteMember(ctx context.Context, input InviteTeamMemberInput) (*TeamInvitation, string, error) {
	if _, err := s.Require(ctx, input.ActorUserID, input.TeamID, domain.TeamPermissionManageMembers); err != nil {
		return nil, "", err
	}
	if input.Role == domain.TeamRoleOwner {
		return nil, "", ErrTeamInvalidRole
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if input.Email == "" {
		return nil, "", infraerrors.BadRequest("TEAM_INVITATION_INVALID", "invitation email is required")
	}
	plain, tokenHash, err := GenerateInvitationToken()
	if err != nil {
		return nil, "", err
	}
	input.TokenHash = tokenHash
	invitation, err := s.repo.InviteMember(ctx, input)
	if err != nil {
		return nil, "", err
	}
	return invitation, plain, nil
}

func (s *TeamService) UpdateMember(ctx context.Context, actorUserID, teamID, userID int64, input UpdateTeamMemberInput) (*TeamMember, error) {
	if _, err := s.Require(ctx, actorUserID, teamID, domain.TeamPermissionManageMembers); err != nil {
		return nil, err
	}
	if input.Role != nil && *input.Role == domain.TeamRoleOwner {
		return nil, ErrTeamInvalidRole
	}
	if input.Role == nil && input.Status == nil {
		return nil, infraerrors.BadRequest("TEAM_MEMBER_UPDATE_EMPTY", "team member update is empty")
	}
	return s.repo.UpdateMember(ctx, actorUserID, teamID, userID, input)
}

func (s *TeamService) RemoveMember(ctx context.Context, actorUserID, teamID, userID int64) error {
	if _, err := s.Require(ctx, actorUserID, teamID, domain.TeamPermissionManageMembers); err != nil {
		return err
	}
	return s.repo.RemoveMember(ctx, actorUserID, teamID, userID)
}

// isAssignableTeamRole reports whether role is one of the non-owner roles an
// admin may assign to a member.
func isAssignableTeamRole(role string) bool {
	switch role {
	case domain.TeamRoleAdmin, domain.TeamRoleBilling, domain.TeamRoleDeveloper, domain.TeamRoleViewer:
		return true
	default:
		return false
	}
}

// AdminListTeams returns a paginated platform-admin view of all teams.
// It performs no team-membership permission checks; access is gated by the
// admin auth middleware at the route level.
func (s *TeamService) AdminListTeams(ctx context.Context, filter AdminTeamListFilter, params pagination.PaginationParams) ([]AdminTeamSummary, *pagination.PaginationResult, error) {
	filter.Search = strings.TrimSpace(filter.Search)
	filter.Status = strings.TrimSpace(filter.Status)
	return s.repo.AdminListTeams(ctx, filter, params)
}

// AdminGetTeam returns the team summary along with its members and pending
// invitations. No membership checks are performed.
func (s *TeamService) AdminGetTeam(ctx context.Context, teamID int64) (*AdminTeamSummary, []TeamMember, []TeamInvitation, error) {
	if teamID <= 0 {
		return nil, nil, nil, ErrTeamNotFound
	}
	summary, err := s.repo.AdminGetTeamSummary(ctx, teamID)
	if err != nil {
		return nil, nil, nil, err
	}
	members, invitations, err := s.repo.ListMembers(ctx, teamID)
	if err != nil {
		return nil, nil, nil, err
	}
	return summary, members, invitations, nil
}

// AdminUpdateTeam updates a team's name and/or status. An empty update is
// rejected. No membership checks are performed.
func (s *TeamService) AdminUpdateTeam(ctx context.Context, teamID int64, input AdminUpdateTeamInput) (*AdminTeamSummary, error) {
	if teamID <= 0 {
		return nil, ErrTeamNotFound
	}
	var name *string
	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		if trimmed == "" {
			return nil, infraerrors.BadRequest("TEAM_INVALID_INPUT", "team name cannot be empty")
		}
		name = &trimmed
	}
	if input.Status != nil {
		if *input.Status != domain.TeamStatusActive && *input.Status != domain.TeamStatusDisabled {
			return nil, infraerrors.BadRequest("TEAM_INVALID_STATUS", "invalid team status")
		}
	}
	if name == nil && input.Status == nil {
		return nil, infraerrors.BadRequest("TEAM_UPDATE_EMPTY", "team update is empty")
	}
	if _, err := s.repo.UpdateTeam(ctx, teamID, name, input.Status); err != nil {
		return nil, err
	}
	return s.repo.AdminGetTeamSummary(ctx, teamID)
}

// AdminAddMember adds (or reactivates) a member on a team as a platform admin.
// The target user is resolved by UserID first, falling back to Email. The owner
// role cannot be assigned. If an active membership already exists it returns
// ErrTeamMemberExists; a soft-deleted/left membership is reactivated instead of
// creating a duplicate row.
func (s *TeamService) AdminAddMember(ctx context.Context, input AdminAddMemberInput) (*TeamMember, error) {
	if input.TeamID <= 0 {
		return nil, ErrTeamNotFound
	}
	if !isAssignableTeamRole(input.Role) {
		return nil, ErrTeamInvalidRole
	}

	// Ensure the team exists before touching membership.
	if _, err := s.repo.GetTeamByID(ctx, input.TeamID); err != nil {
		return nil, err
	}

	userID := input.UserID
	if userID <= 0 {
		email := strings.ToLower(strings.TrimSpace(input.Email))
		if email == "" {
			return nil, infraerrors.BadRequest("TEAM_MEMBER_INVALID_INPUT", "user_id or email is required")
		}
		if s.userLookup == nil {
			return nil, ErrUserNotFound
		}
		user, err := s.userLookup.GetByEmail(ctx, email)
		if err != nil {
			return nil, err
		}
		userID = user.ID
	}

	return s.repo.AddMember(ctx, input.TeamID, userID, input.Role, input.AdminUserID)
}

// AdminUpdateMember updates a member's role and/or status as a platform admin.
// The owner role cannot be assigned, the team owner's own membership cannot be
// modified here, and an empty update is rejected.
func (s *TeamService) AdminUpdateMember(ctx context.Context, adminUserID, teamID, userID int64, input UpdateTeamMemberInput) (*TeamMember, error) {
	if teamID <= 0 || userID <= 0 {
		return nil, ErrTeamMembershipNotFound
	}
	if input.Role != nil && !isAssignableTeamRole(*input.Role) {
		return nil, ErrTeamInvalidRole
	}
	if input.Role == nil && input.Status == nil {
		return nil, infraerrors.BadRequest("TEAM_MEMBER_UPDATE_EMPTY", "team member update is empty")
	}
	team, err := s.repo.GetTeamByID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if team.OwnerUserID == userID {
		return nil, ErrTeamOwnerImmutable
	}
	return s.repo.UpdateMember(ctx, adminUserID, teamID, userID, input)
}

// AdminRemoveMember soft-deletes a member as a platform admin. The team owner
// cannot be removed.
func (s *TeamService) AdminRemoveMember(ctx context.Context, adminUserID, teamID, userID int64) error {
	if teamID <= 0 || userID <= 0 {
		return ErrTeamMembershipNotFound
	}
	team, err := s.repo.GetTeamByID(ctx, teamID)
	if err != nil {
		return err
	}
	if team.OwnerUserID == userID {
		return ErrTeamOwnerImmutable
	}
	return s.repo.RemoveMember(ctx, adminUserID, teamID, userID)
}

func GenerateInvitationToken() (plain string, tokenHash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	plain = hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(plain))
	return plain, hex.EncodeToString(sum[:]), nil
}
