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
)

var (
	ErrTeamNotFound           = infraerrors.NotFound("TEAM_NOT_FOUND", "team not found")
	ErrTeamMembershipNotFound = infraerrors.Forbidden("TEAM_MEMBERSHIP_NOT_FOUND", "team membership not found")
	ErrTeamPermissionDenied   = infraerrors.Forbidden("TEAM_PERMISSION_DENIED", "team permission denied")
	ErrTeamInvalidRole        = infraerrors.BadRequest("TEAM_INVALID_ROLE", "invalid team role")
	ErrTeamInvitationInvalid  = infraerrors.BadRequest("TEAM_INVITATION_INVALID", "team invitation is invalid")
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

type TeamRepository interface {
	CreateTeam(ctx context.Context, input CreateTeamInput) (*Team, error)
	GetMembership(ctx context.Context, teamID, userID int64) (*TeamMember, error)
	ListWorkspaces(ctx context.Context, userID int64) ([]WorkspaceSubject, error)
	ListMembers(ctx context.Context, teamID int64) ([]TeamMember, []TeamInvitation, error)
	InviteMember(ctx context.Context, input InviteTeamMemberInput) (*TeamInvitation, error)
	UpdateMember(ctx context.Context, actorUserID, teamID, userID int64, input UpdateTeamMemberInput) (*TeamMember, error)
	RemoveMember(ctx context.Context, actorUserID, teamID, userID int64) error
}

type TeamService struct {
	repo           TeamRepository
	billingSubject BillingSubjectRepository
}

func NewTeamService(repo TeamRepository, billingSubject BillingSubjectRepository) *TeamService {
	return &TeamService{repo: repo, billingSubject: billingSubject}
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

func GenerateInvitationToken() (plain string, tokenHash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	plain = hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(plain))
	return plain, hex.EncodeToString(sum[:]), nil
}
