package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
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
	// ErrTeamInvitationExpired is returned when an invitation has lapsed (or is no
	// longer pending) at accept time.
	ErrTeamInvitationExpired = infraerrors.BadRequest("TEAM_INVITATION_EXPIRED", "team invitation is expired")
	// ErrTeamInvitationEmailMismatch is returned when the accepting account's email
	// does not match the invited email (acceptance is bound to the invited email).
	ErrTeamInvitationEmailMismatch = infraerrors.Forbidden("TEAM_INVITATION_EMAIL_MISMATCH", "invitation does not match your account email")

	// ErrTeamMemberExists is returned by AdminAddMember when an active membership
	// already exists for the target (team, user).
	ErrTeamMemberExists = infraerrors.BadRequest("TEAM_MEMBER_EXISTS", "team member already exists")
	// ErrTeamOwnerImmutable is returned when an admin attempts to modify or remove
	// the team owner's membership (ownership is managed separately, not here).
	ErrTeamOwnerImmutable = infraerrors.BadRequest("TEAM_OWNER_IMMUTABLE", "team owner membership cannot be modified here")

	// ErrTeamDissolveHasBalance / ErrTeamDissolveHasActiveKeys 是解散团队的安全最小语义守卫：
	// 团队主体仍有余额、或仍有启用中团队 API Key 时拒绝解散，要求调用方先清理。
	ErrTeamDissolveHasBalance    = infraerrors.Conflict("TEAM_DISSOLVE_HAS_BALANCE", "team has remaining balance; zero it before dissolving")
	ErrTeamDissolveHasActiveKeys = infraerrors.Conflict("TEAM_DISSOLVE_HAS_ACTIVE_KEYS", "team has active API keys; delete them before dissolving")
)

// MaxTeamsPerOwner 普通用户自助创建团队的上限（owner 口径）。admin 经 AdminCreateTeam 不受此限。
const MaxTeamsPerOwner = 5

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

	// Aggregates populated by ListMembers for the team members view.
	// KeyCount is the number of API keys this member owns within the team;
	// Last7dActualCost is the member's actual spend as actor over the last 7 days.
	KeyCount         int
	Last7dActualCost float64
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
	CreatedAt        time.Time
}

// InvitationPreview is the read-only view of an invitation returned by
// PreviewInvitation (no mutation). Expired reflects whether the invitation has
// lapsed past its ExpiresAt at preview time.
type InvitationPreview struct {
	TeamID   int64
	TeamName string
	Role     string
	Email    string
	Status   string
	Expired  bool
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
	// OwnerUserID, when > 0, overrides the team owner (used by AdminCreateTeam so a
	// platform admin can create a team on behalf of another user). When 0 the owner
	// defaults to ActorUserID (user self-service path).
	OwnerUserID int64
	// Concurrency/RpmLimit：nil = 用默认(5/0)，非 nil = 用该值(含 0=不限制)。仅 admin 创建路径会传。
	Concurrency *int
	RpmLimit    *int
}

// AdminCreateTeamInput holds the input for AdminCreateTeam. The owner is resolved
// by OwnerUserID first, falling back to OwnerEmail. AdminUserID is the platform
// admin performing the action (recorded as the team creator).
type AdminCreateTeamInput struct {
	Name        string
	Slug        string
	OwnerUserID int64
	OwnerEmail  string
	AdminUserID int64
	Concurrency *int
	RpmLimit    *int
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
	Concurrency int
	RpmLimit    int
	MemberCount    int
	ActiveKeyCount int
}

// AdminTeamListFilter holds the optional filters for AdminListTeams.
type AdminTeamListFilter struct {
	Search string // matches name OR slug (case-insensitive)
	Status string // optional: "active" / "disabled"
}

// AdminUpdateTeamInput holds the mutable team fields for AdminUpdateTeam.
type AdminUpdateTeamInput struct {
	Name        *string
	Status      *string
	Concurrency *int
	RpmLimit    *int
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

// TeamMemberUsage holds per-actor usage aggregates for a team within a time window.
type TeamMemberUsage struct {
	UserID     int64   `json:"user_id"`
	Requests   int64   `json:"requests"`
	TotalCost  float64 `json:"total_cost"`
	ActualCost float64 `json:"actual_cost"`
}

type TeamRepository interface {
	CreateTeam(ctx context.Context, input CreateTeamInput) (*Team, error)
	GetMembership(ctx context.Context, teamID, userID int64) (*TeamMember, error)
	ListWorkspaces(ctx context.Context, userID int64) ([]WorkspaceSubject, error)
	ListMembers(ctx context.Context, teamID int64) ([]TeamMember, []TeamInvitation, error)
	InviteMember(ctx context.Context, input InviteTeamMemberInput) (*TeamInvitation, error)
	UpdateMember(ctx context.Context, actorUserID, teamID, userID int64, input UpdateTeamMemberInput) (*TeamMember, error)
	RemoveMember(ctx context.Context, actorUserID, teamID, userID int64) error

	// GetInvitationByTokenHash returns the pending (non-deleted) invitation whose
	// token_hash matches; a not-found error is returned when none matches.
	GetInvitationByTokenHash(ctx context.Context, tokenHash string) (*TeamInvitation, error)
	// AcceptInvitation creates-or-reactivates an active membership for the accepting
	// user (role/invited_by from the invitation) and marks the invitation accepted,
	// in a single transaction. It is idempotent when the same user re-accepts an
	// already-accepted invitation (returns the existing membership).
	AcceptInvitation(ctx context.Context, invitationID, acceptingUserID, teamID int64, role string) (*TeamMember, error)
	// TransferOwnership promotes newOwnerUserID to owner and demotes prevOwnerUserID
	// to admin, updating teams.owner_user_id, in a single transaction. The new owner
	// must be an active member.
	TransferOwnership(ctx context.Context, teamID, newOwnerUserID, prevOwnerUserID int64) error

	// Platform-admin operations (no team-membership gating).
	AdminListTeams(ctx context.Context, filter AdminTeamListFilter, params pagination.PaginationParams) ([]AdminTeamSummary, *pagination.PaginationResult, error)
	GetTeamByID(ctx context.Context, teamID int64) (*Team, error)
	AdminGetTeamSummary(ctx context.Context, teamID int64) (*AdminTeamSummary, error)
	AddMember(ctx context.Context, teamID, userID int64, role string, invitedByUserID int64) (*TeamMember, error)
	UpdateTeam(ctx context.Context, teamID int64, name *string, status *string) (*Team, error)
	// CountActiveAPIKeysByBillingSubjectID 统计该计费主体名下「启用中」的 API Key 数（解散前置校验）。
	CountActiveAPIKeysByBillingSubjectID(ctx context.Context, billingSubjectID int64) (int, error)
	// CountActiveTeamsByOwner 统计某用户作为 owner 拥有的「活跃」团队数（status=active 且未软删），用于自助建团队上限。
	CountActiveTeamsByOwner(ctx context.Context, ownerUserID int64) (int, error)
	// DissolveTeam 在单事务内软删团队成员、团队与团队计费主体（高危不可逆，调用方须已校验 owner + 前置条件）。
	DissolveTeam(ctx context.Context, teamID int64) error
	// UsageByMember 返回团队在 [start, end) 时间窗内按成员（actor_user_id）聚合的用量明细。
	UsageByMember(ctx context.Context, teamID int64, start, end time.Time) ([]TeamMemberUsage, error)
}

// TeamInviteNotifier delivers a team invitation to the invitee's email. It is a
// best-effort dependency: InviteMember never fails when delivery fails. The
// concrete adapter (over EmailService + SettingService) is wired in production;
// it may be nil in tests/contexts where delivery is not needed.
type TeamInviteNotifier interface {
	SendInvite(ctx context.Context, toEmail, acceptLink, teamName string) error
}

// TeamUserLookup is the narrow user-resolution dependency used by admin member
// and team management (resolving a target user by email or id). *userRepository
// (the concrete UserRepository implementation) satisfies it.
type TeamUserLookup interface {
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByID(ctx context.Context, id int64) (*User, error)
}

// SubjectBalanceInvalidator 失效计费主体余额缓存（*BillingCacheService 实现）。
type SubjectBalanceInvalidator interface {
	InvalidateSubjectBalance(ctx context.Context, subjectID int64) error
}

type TeamService struct {
	repo                TeamRepository
	billingSubject      BillingSubjectRepository
	userLookup          TeamUserLookup
	inviteNotifier      TeamInviteNotifier
	redeemCodeRepo      RedeemCodeRepository
	subjectBalanceCache SubjectBalanceInvalidator
}

func NewTeamService(repo TeamRepository, billingSubject BillingSubjectRepository, userRepo UserRepository, redeemCodeRepo RedeemCodeRepository, subjectBalanceCache SubjectBalanceInvalidator) *TeamService {
	// userRepo (the full UserRepository) is stored behind the narrow TeamUserLookup
	// interface; accepting the full interface keeps wire injection binding-free,
	// while the narrow field keeps the admin email-resolution dependency minimal.
	s := &TeamService{repo: repo, billingSubject: billingSubject, redeemCodeRepo: redeemCodeRepo, subjectBalanceCache: subjectBalanceCache}
	if userRepo != nil {
		s.userLookup = userRepo
	}
	return s
}

// SetInviteNotifier injects the (best-effort) invitation email notifier. Wire
// uses this after construction so NewTeamService stays binding-free.
func (s *TeamService) SetInviteNotifier(n TeamInviteNotifier) {
	s.inviteNotifier = n
}

func (s *TeamService) CreateTeam(ctx context.Context, input CreateTeamInput) (*Team, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Slug = strings.TrimSpace(input.Slug)
	if input.ActorUserID <= 0 || input.Name == "" {
		return nil, infraerrors.BadRequest("TEAM_INVALID_INPUT", "team name is required")
	}
	// 自助创建上限（owner 口径）。admin 路径走 AdminCreateTeam，不经此处。
	count, err := s.repo.CountActiveTeamsByOwner(ctx, input.ActorUserID)
	if err != nil {
		return nil, err
	}
	if count >= MaxTeamsPerOwner {
		return nil, infraerrors.BadRequest("TEAM_LIMIT_REACHED",
			fmt.Sprintf("已达自助创建团队上限（%d 个），如需更多请联系管理员", MaxTeamsPerOwner))
	}
	slug, err := buildTeamSlug(input.Name, input.Slug)
	if err != nil {
		return nil, err
	}
	input.Slug = slug
	return s.repo.CreateTeam(ctx, input)
}

// GetTeam returns a team by id (no membership gating). Used by handlers to render
// the joined team summary after an invitation is accepted.
func (s *TeamService) GetTeam(ctx context.Context, teamID int64) (*Team, error) {
	return s.repo.GetTeamByID(ctx, teamID)
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
	items, err := s.repo.ListWorkspaces(ctx, userID)
	if err != nil {
		return nil, err
	}
	if hasPersonalWorkspace(items) {
		return items, nil
	}
	// Self-heal: a user without a personal billing subject (e.g. the bootstrap
	// admin created outside userRepo.Create, or an account created before the
	// subjects were backfilled) would otherwise get USER_NOT_FOUND on every
	// authenticated route, since SubjectContextMiddleware resolves the active
	// workspace from this list and aborts when it is empty. Idempotently ensure
	// the personal subject exists, then re-list. EnsurePersonalForUser is
	// safe to call repeatedly (check-first + unique-conflict backstop).
	if s.billingSubject == nil || s.userLookup == nil {
		return items, nil
	}
	user, lookupErr := s.userLookup.GetByID(ctx, userID)
	if lookupErr != nil || user == nil {
		// Unknown/deleted user: nothing to heal. Return what we have (empty list
		// → caller surfaces USER_NOT_FOUND, which is correct for a stale token).
		return items, nil
	}
	if _, ensureErr := s.billingSubject.EnsurePersonalForUser(ctx, user); ensureErr != nil {
		slog.WarnContext(ctx, "ListWorkspaces: failed to ensure personal billing subject",
			"user_id", userID, "error", ensureErr.Error())
		return items, nil
	}
	healed, err := s.repo.ListWorkspaces(ctx, userID)
	if err != nil {
		return nil, err
	}
	return healed, nil
}

// hasPersonalWorkspace reports whether the resolved workspaces already include
// the caller's personal (user) billing subject.
func hasPersonalWorkspace(items []WorkspaceSubject) bool {
	for _, item := range items {
		if item.Type == domain.BillingSubjectTypeUser {
			return true
		}
	}
	return false
}

func (s *TeamService) ListMembers(ctx context.Context, actorUserID, teamID int64) ([]TeamMember, []TeamInvitation, error) {
	if _, err := s.Require(ctx, actorUserID, teamID, domain.TeamPermissionViewUsage); err != nil {
		return nil, nil, err
	}
	return s.repo.ListMembers(ctx, teamID)
}

// InviteMember creates a pending invitation and best-effort delivers it by email.
// It returns the invitation, the plaintext token (shown once so the caller can
// surface a copyable link), and the accept link. Email delivery never fails the
// invite: a delivery error is logged and the (copyable) link remains the fallback.
func (s *TeamService) InviteMember(ctx context.Context, input InviteTeamMemberInput) (*TeamInvitation, string, string, error) {
	if _, err := s.Require(ctx, input.ActorUserID, input.TeamID, domain.TeamPermissionManageMembers); err != nil {
		return nil, "", "", err
	}
	if input.Role == domain.TeamRoleOwner {
		return nil, "", "", ErrTeamInvalidRole
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if input.Email == "" {
		return nil, "", "", infraerrors.BadRequest("TEAM_INVITATION_INVALID", "invitation email is required")
	}
	plain, tokenHash, err := GenerateInvitationToken()
	if err != nil {
		return nil, "", "", err
	}
	input.TokenHash = tokenHash
	invitation, err := s.repo.InviteMember(ctx, input)
	if err != nil {
		return nil, "", "", err
	}

	acceptLink := buildInvitationAcceptLink(ctx, s.inviteNotifier, plain)
	if s.inviteNotifier != nil {
		teamName := ""
		if team, terr := s.repo.GetTeamByID(ctx, input.TeamID); terr == nil && team != nil {
			teamName = team.Name
		}
		if sendErr := s.inviteNotifier.SendInvite(ctx, input.Email, acceptLink, teamName); sendErr != nil {
			// Best-effort: never fail the invite when delivery fails. The copyable
			// link returned to the caller is the fallback.
			slog.WarnContext(ctx, "InviteMember: failed to send invitation email",
				"team_id", input.TeamID, "error", sendErr.Error())
		}
	}
	return invitation, plain, acceptLink, nil
}

// buildInvitationAcceptLink builds the frontend accept link for a plaintext token.
// When the notifier exposes a base URL it is prefixed; otherwise a relative path
// is returned (still copyable/usable by the SPA).
func buildInvitationAcceptLink(ctx context.Context, notifier TeamInviteNotifier, plainToken string) string {
	path := "/teams/accept?token=" + url.QueryEscape(plainToken)
	if br, ok := notifier.(interface {
		AcceptBaseURL(ctx context.Context) string
	}); ok {
		if base := strings.TrimRight(br.AcceptBaseURL(ctx), "/"); base != "" {
			return base + path
		}
	}
	return path
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

// hashInvitationToken hashes a plaintext invitation token with the same scheme as
// GenerateInvitationToken (sha256 hex).
func hashInvitationToken(plainToken string) string {
	sum := sha256.Sum256([]byte(plainToken))
	return hex.EncodeToString(sum[:])
}

// PreviewInvitation returns a read-only view of the invitation behind a plaintext
// token (team name, role, invited email, status, and whether it has expired). It
// performs no mutation. A token that does not match a pending invitation yields
// ErrTeamInvitationInvalid. The invitee need not be a team member to call this.
func (s *TeamService) PreviewInvitation(ctx context.Context, plainToken string) (*InvitationPreview, error) {
	plainToken = strings.TrimSpace(plainToken)
	if plainToken == "" {
		return nil, ErrTeamInvitationInvalid
	}
	invitation, err := s.repo.GetInvitationByTokenHash(ctx, hashInvitationToken(plainToken))
	if err != nil {
		return nil, ErrTeamInvitationInvalid
	}
	preview := &InvitationPreview{
		TeamID:  invitation.TeamID,
		Role:    invitation.Role,
		Email:   invitation.Email,
		Status:  invitation.Status,
		Expired: !invitation.ExpiresAt.IsZero() && time.Now().After(invitation.ExpiresAt),
	}
	if team, terr := s.repo.GetTeamByID(ctx, invitation.TeamID); terr == nil && team != nil {
		preview.TeamName = team.Name
	}
	return preview, nil
}

// AcceptInvitation accepts the invitation behind a plaintext token for the actor.
// Acceptance is bound to the invited email: the actor's account email must equal
// the invited email (normalized). An invitation that is not pending or has lapsed
// is rejected with ErrTeamInvitationExpired. On success the actor is added to (or
// reactivated on) the team with the invited role.
func (s *TeamService) AcceptInvitation(ctx context.Context, actorUserID int64, plainToken string) (*TeamMember, error) {
	if actorUserID <= 0 {
		return nil, infraerrors.Unauthorized("USER_NOT_AUTHENTICATED", "user not authenticated")
	}
	plainToken = strings.TrimSpace(plainToken)
	if plainToken == "" {
		return nil, ErrTeamInvitationInvalid
	}
	invitation, err := s.repo.GetInvitationByTokenHash(ctx, hashInvitationToken(plainToken))
	if err != nil {
		return nil, ErrTeamInvitationInvalid
	}
	if invitation.Status != domain.TeamInvitationStatusPending {
		return nil, ErrTeamInvitationExpired
	}
	if !invitation.ExpiresAt.IsZero() && time.Now().After(invitation.ExpiresAt) {
		return nil, ErrTeamInvitationExpired
	}
	if s.userLookup == nil {
		return nil, ErrUserNotFound
	}
	user, err := s.userLookup.GetByID(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	if normalizeEmail(user.Email) != normalizeEmail(invitation.Email) {
		return nil, ErrTeamInvitationEmailMismatch
	}
	return s.repo.AcceptInvitation(ctx, invitation.ID, actorUserID, invitation.TeamID, invitation.Role)
}

// normalizeEmail lowercases and trims an email for case/space-insensitive matching.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// TransferOwnership transfers team ownership from the current owner (the actor) to
// newOwnerUserID. The actor must be the current owner (owner-only, stricter than
// "manage members"). Self-transfer is rejected; the new owner must be an active
// member. The previous owner is demoted to admin (kept in the team).
func (s *TeamService) TransferOwnership(ctx context.Context, actorUserID, teamID, newOwnerUserID int64) error {
	if teamID <= 0 || newOwnerUserID <= 0 {
		return infraerrors.BadRequest("TEAM_INVALID_INPUT", "team_id and user_id are required")
	}
	team, err := s.repo.GetTeamByID(ctx, teamID)
	if err != nil {
		return err
	}
	if team.OwnerUserID != actorUserID {
		return ErrTeamPermissionDenied
	}
	if newOwnerUserID == actorUserID {
		return infraerrors.BadRequest("TEAM_TRANSFER_SELF", "cannot transfer ownership to yourself")
	}
	if err := s.requireActiveMember(ctx, teamID, newOwnerUserID); err != nil {
		return err
	}
	return s.repo.TransferOwnership(ctx, teamID, newOwnerUserID, team.OwnerUserID)
}

// AdminDeleteTeam 平台 admin 强删团队：直接软删 成员/团队/团队计费主体，无 owner/余额/启用-key 守卫
// （守卫仅作用于 owner 自助 DissolveTeam）。团队 API key 的删除与缓存失效由调用方在此之前完成。
func (s *TeamService) AdminDeleteTeam(ctx context.Context, teamID int64) error {
	if teamID <= 0 {
		return ErrTeamNotFound
	}
	return s.repo.DissolveTeam(ctx, teamID)
}

// AdminTransferOwnership transfers team ownership as a platform admin (no
// membership gating on the actor). The new owner must still be an active member;
// the previous owner is demoted to admin.
func (s *TeamService) AdminTransferOwnership(ctx context.Context, teamID, newOwnerUserID int64) error {
	if teamID <= 0 || newOwnerUserID <= 0 {
		return infraerrors.BadRequest("TEAM_INVALID_INPUT", "team_id and user_id are required")
	}
	team, err := s.repo.GetTeamByID(ctx, teamID)
	if err != nil {
		return err
	}
	if team.OwnerUserID == newOwnerUserID {
		// Already the owner: nothing to do (idempotent, avoids demoting self to admin).
		return nil
	}
	if err := s.requireActiveMember(ctx, teamID, newOwnerUserID); err != nil {
		return err
	}
	return s.repo.TransferOwnership(ctx, teamID, newOwnerUserID, team.OwnerUserID)
}

// requireActiveMember verifies that userID is an active member of teamID, returning
// ErrTeamMembershipNotFound otherwise.
func (s *TeamService) requireActiveMember(ctx context.Context, teamID, userID int64) error {
	member, err := s.repo.GetMembership(ctx, teamID, userID)
	if err != nil {
		return err
	}
	if member.Status != domain.TeamMemberStatusActive {
		return ErrTeamMembershipNotFound
	}
	return nil
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

// AdminCreateTeam creates a team on behalf of a resolved owner as a platform
// admin. The owner is resolved by OwnerUserID first (verified via GetByID),
// falling back to OwnerEmail (resolved via GetByEmail); at least one is required.
// The slug is auto-generated from the name when not provided. No team-membership
// checks are performed; access is gated by the admin auth middleware at the route
// level. The team creator is recorded as the admin (AdminUserID) while ownership
// is assigned to the resolved owner.
func (s *TeamService) AdminCreateTeam(ctx context.Context, input AdminCreateTeamInput) (*AdminTeamSummary, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return nil, infraerrors.BadRequest("TEAM_INVALID_INPUT", "team name is required")
	}

	ownerID, err := s.resolveTeamOwner(ctx, input.OwnerUserID, input.OwnerEmail)
	if err != nil {
		return nil, err
	}

	slug, err := buildTeamSlug(input.Name, input.Slug)
	if err != nil {
		return nil, err
	}

	team, err := s.repo.CreateTeam(ctx, CreateTeamInput{
		ActorUserID: input.AdminUserID,
		OwnerUserID: ownerID,
		Name:        input.Name,
		Slug:        slug,
		Concurrency: input.Concurrency,
		RpmLimit:    input.RpmLimit,
	})
	if err != nil {
		return nil, err
	}
	return s.repo.AdminGetTeamSummary(ctx, team.ID)
}

// resolveTeamOwner resolves a team owner user id from an explicit id or an email.
// OwnerUserID takes precedence and is verified to exist; otherwise email is
// resolved (lowercased/trimmed). At least one must be provided.
func (s *TeamService) resolveTeamOwner(ctx context.Context, ownerUserID int64, ownerEmail string) (int64, error) {
	if ownerUserID > 0 {
		if s.userLookup == nil {
			return 0, ErrUserNotFound
		}
		owner, err := s.userLookup.GetByID(ctx, ownerUserID)
		if err != nil {
			return 0, err
		}
		return owner.ID, nil
	}
	email := strings.ToLower(strings.TrimSpace(ownerEmail))
	if email == "" {
		return 0, infraerrors.BadRequest("TEAM_INVALID_INPUT", "owner_user_id or owner_email is required")
	}
	if s.userLookup == nil {
		return 0, ErrUserNotFound
	}
	owner, err := s.userLookup.GetByEmail(ctx, email)
	if err != nil {
		return 0, err
	}
	return owner.ID, nil
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
	if summary.BillingSubjectID != nil && *summary.BillingSubjectID > 0 {
		count, err := s.repo.CountActiveAPIKeysByBillingSubjectID(ctx, *summary.BillingSubjectID)
		if err != nil {
			return nil, nil, nil, err
		}
		summary.ActiveKeyCount = count
	}
	members, invitations, err := s.repo.ListMembers(ctx, teamID)
	if err != nil {
		return nil, nil, nil, err
	}
	return summary, members, invitations, nil
}

// UpdateTeamSettingsInput 用户端团队设置入参（当前仅团队名）。
type UpdateTeamSettingsInput struct {
	Name *string
}

// UpdateTeamSettings 更新团队设置（名称），需 team.settings.manage（owner/admin）。
// 不开放改 status（停用/启用属平台管理员后台或独立流程，非自助设置项）。
func (s *TeamService) UpdateTeamSettings(ctx context.Context, actorUserID, teamID int64, input UpdateTeamSettingsInput) (*Team, error) {
	if _, err := s.Require(ctx, actorUserID, teamID, domain.TeamPermissionManageSettings); err != nil {
		return nil, err
	}
	var name *string
	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		if trimmed == "" {
			return nil, infraerrors.BadRequest("TEAM_INVALID_INPUT", "team name cannot be empty")
		}
		name = &trimmed
	}
	if name == nil {
		return nil, infraerrors.BadRequest("TEAM_UPDATE_EMPTY", "team update is empty")
	}
	return s.repo.UpdateTeam(ctx, teamID, name, nil)
}

// DissolveTeam 解散团队（owner only，高危不可逆，安全最小语义）：
//   - 余额 > 0 或仍有启用中团队 API Key → 拒绝，要求先清理；
//   - 否则同事务软删 成员 → 团队 → 团队计费主体。
func (s *TeamService) DissolveTeam(ctx context.Context, actorUserID, teamID int64) error {
	if _, err := s.Require(ctx, actorUserID, teamID, domain.TeamPermissionDissolveTeam); err != nil {
		return err
	}
	team, err := s.repo.GetTeamByID(ctx, teamID)
	if err != nil {
		return err
	}
	if team.BillingSubjectID != nil && *team.BillingSubjectID > 0 {
		subj, err := s.billingSubject.GetByID(ctx, *team.BillingSubjectID)
		if err != nil {
			return err
		}
		if subj != nil && subj.Balance > 0 {
			return ErrTeamDissolveHasBalance
		}
		count, err := s.repo.CountActiveAPIKeysByBillingSubjectID(ctx, *team.BillingSubjectID)
		if err != nil {
			return err
		}
		if count > 0 {
			return ErrTeamDissolveHasActiveKeys
		}
	}
	return s.repo.DissolveTeam(ctx, teamID)
}

// AdminUpdateTeam updates a team's name, status, concurrency and/or rpm_limit.
// An empty update is rejected. No membership checks are performed.
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
	if input.Concurrency != nil && *input.Concurrency < 0 {
		return nil, infraerrors.BadRequest("TEAM_INVALID_LIMIT", "concurrency must be >= 0")
	}
	if input.RpmLimit != nil && *input.RpmLimit < 0 {
		return nil, infraerrors.BadRequest("TEAM_INVALID_LIMIT", "rpm_limit must be >= 0")
	}
	if name == nil && input.Status == nil && input.Concurrency == nil && input.RpmLimit == nil {
		return nil, infraerrors.BadRequest("TEAM_UPDATE_EMPTY", "team update is empty")
	}
	if name != nil || input.Status != nil {
		if _, err := s.repo.UpdateTeam(ctx, teamID, name, input.Status); err != nil {
			return nil, err
		}
	}
	if input.Concurrency != nil || input.RpmLimit != nil {
		summary, err := s.repo.AdminGetTeamSummary(ctx, teamID)
		if err != nil {
			return nil, err
		}
		if summary.BillingSubjectID == nil {
			return nil, ErrTeamNotFound
		}
		concurrency := summary.Concurrency
		if input.Concurrency != nil {
			concurrency = *input.Concurrency
		}
		rpmLimit := summary.RpmLimit
		if input.RpmLimit != nil {
			rpmLimit = *input.RpmLimit
		}
		if err := s.billingSubject.UpdateLimits(ctx, *summary.BillingSubjectID, concurrency, rpmLimit); err != nil {
			return nil, err
		}
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

// UsageByMember 返回团队在 [start, end) 时间窗内按成员（actor_user_id）聚合的用量明细。
// 调用方需持有 team.usage.view.all 权限（owner/admin），否则返回 ErrTeamPermissionDenied。
func (s *TeamService) UsageByMember(ctx context.Context, actorUserID, teamID int64, start, end time.Time) ([]TeamMemberUsage, error) {
	if _, err := s.Require(ctx, actorUserID, teamID, domain.TeamPermissionViewUsageAll); err != nil {
		return nil, err
	}
	return s.repo.UsageByMember(ctx, teamID, start, end)
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

// maxTeamSlugBaseLen caps the slug base (the slugified name portion) so the final
// slug, including the random suffix, stays within a reasonable length.
const maxTeamSlugBaseLen = 80

// buildTeamSlug returns the slug to persist for a team. If an explicit slug is
// provided it is returned as-is (already trimmed by the caller). Otherwise a slug
// is auto-generated from the name: the slugified base (falling back to "team" when
// slugification yields an empty string, e.g. for all-non-ASCII names) plus a short
// random suffix to keep it unique.
func buildTeamSlug(name, explicitSlug string) (string, error) {
	if explicitSlug != "" {
		return explicitSlug, nil
	}
	base := slugifyTeamName(name)
	if base == "" {
		base = "team"
	}
	suffix, err := randHexSuffix()
	if err != nil {
		return "", err
	}
	return base + "-" + suffix, nil
}

// slugifyTeamName lowercases name, collapses any run of characters outside
// [a-z0-9] into a single "-", trims leading/trailing "-", and caps the length.
// It may return "" when name has no ASCII alphanumerics (e.g. all-CJK names);
// callers handle that fallback.
func slugifyTeamName(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		// Any other character becomes a single "-" (runs collapse).
		if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > maxTeamSlugBaseLen {
		slug = strings.Trim(slug[:maxTeamSlugBaseLen], "-")
	}
	return slug
}

// AdminAdjustTeamBalance 平台 admin 调整团队（计费主体）余额。
// operation: "set"（绝对值）/ "add"（加）/ "subtract"（减）。
// 结果余额不得为负。写带 BillingSubjectID 的审计记录（best-effort），并异步失效主体余额缓存。
func (s *TeamService) AdminAdjustTeamBalance(ctx context.Context, teamID int64, amount float64, operation, notes string) (*AdminTeamSummary, error) {
	summary, err := s.repo.AdminGetTeamSummary(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if summary.BillingSubjectID == nil || *summary.BillingSubjectID <= 0 {
		return nil, infraerrors.BadRequest("TEAM_NO_BILLING_SUBJECT", "team has no billing subject")
	}
	subjectID := *summary.BillingSubjectID
	oldBalance := summary.Balance

	var newBalance float64
	switch operation {
	case "set":
		newBalance = amount
	case "add":
		newBalance = oldBalance + amount
	case "subtract":
		newBalance = oldBalance - amount
	default:
		return nil, infraerrors.BadRequest("TEAM_BALANCE_BAD_OP", "invalid operation")
	}
	if newBalance < 0 {
		return nil, infraerrors.BadRequest("TEAM_BALANCE_NEGATIVE",
			fmt.Sprintf("balance cannot be negative, current: %.2f, result would be: %.2f", oldBalance, newBalance))
	}

	delta := newBalance - oldBalance
	if err := s.billingSubject.UpdateBalance(ctx, subjectID, delta); err != nil {
		return nil, err
	}

	// 审计（best-effort，失败仅记日志，不阻断主流程）
	if delta != 0 && s.redeemCodeRepo != nil {
		if code, gerr := GenerateRedeemCode(); gerr == nil {
			now := time.Now()
			rec := &RedeemCode{
				Code:             code,
				Type:             AdjustmentTypeAdminBalance,
				Value:            delta,
				Status:           StatusUsed,
				UsedAt:           &now,
				Notes:            notes,
				BillingSubjectID: &subjectID,
			}
			if cerr := s.redeemCodeRepo.Create(ctx, rec); cerr != nil {
				slog.WarnContext(ctx, "AdminAdjustTeamBalance: failed to write audit record",
					"team_id", teamID, "error", cerr.Error())
			}
		} else {
			slog.WarnContext(ctx, "AdminAdjustTeamBalance: failed to generate audit code",
				"team_id", teamID, "error", gerr.Error())
		}
	}

	// 异步失效主体余额缓存
	if delta != 0 && s.subjectBalanceCache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if ierr := s.subjectBalanceCache.InvalidateSubjectBalance(cacheCtx, subjectID); ierr != nil {
				slog.Warn("AdminAdjustTeamBalance: invalidate subject balance cache failed",
					"subject_id", subjectID, "error", ierr.Error())
			}
		}()
	}

	// 返回最新 summary（重新读，含新余额）
	return s.repo.AdminGetTeamSummary(ctx, teamID)
}

// randHexSuffix returns ~6 hex chars of crypto-random entropy, reusing the
// crypto/rand style of GenerateInvitationToken.
func randHexSuffix() (string, error) {
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
