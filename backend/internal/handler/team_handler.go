package handler

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TeamHTTPService interface {
	ListWorkspaces(ctx context.Context, userID int64) ([]service.WorkspaceSubject, error)
	CreateTeam(ctx context.Context, input service.CreateTeamInput) (*service.Team, error)
	ListMembers(ctx context.Context, actorUserID, teamID int64) ([]service.TeamMember, []service.TeamInvitation, error)
	InviteMember(ctx context.Context, input service.InviteTeamMemberInput) (*service.TeamInvitation, string, string, error)
	UpdateMember(ctx context.Context, actorUserID, teamID, userID int64, input service.UpdateTeamMemberInput) (*service.TeamMember, error)
	RemoveMember(ctx context.Context, actorUserID, teamID, userID int64) error
	PreviewInvitation(ctx context.Context, plainToken string) (*service.InvitationPreview, error)
	AcceptInvitation(ctx context.Context, actorUserID int64, plainToken string) (*service.TeamMember, error)
	TransferOwnership(ctx context.Context, actorUserID, teamID, newOwnerUserID int64) error
	UpdateTeamSettings(ctx context.Context, actorUserID, teamID int64, input service.UpdateTeamSettingsInput) (*service.Team, error)
	DissolveTeam(ctx context.Context, actorUserID, teamID int64) error
	// GetTeam loads a team by id (used to return the joined team summary on accept).
	GetTeam(ctx context.Context, teamID int64) (*service.Team, error)
	// UsageByMember returns per-actor usage aggregates for a team within [start, end).
	// Requires team.usage.view.all (owner/admin).
	UsageByMember(ctx context.Context, actorUserID, teamID int64, start, end time.Time) ([]service.TeamMemberUsage, error)
}

type TeamHandler struct {
	teamService TeamHTTPService
}

func NewTeamHandler(teamService TeamHTTPService) *TeamHandler {
	return &TeamHandler{teamService: teamService}
}

type CreateTeamRequest struct {
	Name string `json:"name" binding:"required"`
	// Slug is optional; when empty the service auto-generates one from Name.
	Slug string `json:"slug"`
}

type InviteMemberRequest struct {
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role" binding:"required,oneof=admin billing developer viewer"`
}

type UpdateMemberRequest struct {
	Role   *string `json:"role" binding:"omitempty,oneof=admin billing developer viewer"`
	Status *string `json:"status" binding:"omitempty,oneof=active suspended"`
}

func (h *TeamHandler) ListWorkspaces(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	workspaces, err := h.teamService.ListWorkspaces(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"workspaces": workspaces})
}

func (h *TeamHandler) CreateTeam(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req CreateTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid team request")
		return
	}
	team, err := h.teamService.CreateTeam(c.Request.Context(), service.CreateTeamInput{ActorUserID: subject.UserID, Name: req.Name, Slug: req.Slug})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"team": team})
}

func (h *TeamHandler) ListMembers(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "Invalid team ID")
		return
	}
	members, invitations, err := h.teamService.ListMembers(c.Request.Context(), subject.UserID, teamID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"members": teamMemberDTOsFromService(members), "invitations": teamInvitationDTOsFromService(invitations)})
}

func (h *TeamHandler) InviteMember(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "Invalid team ID")
		return
	}
	var req InviteMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid invitation request")
		return
	}
	invitation, token, acceptLink, err := h.teamService.InviteMember(c.Request.Context(), service.InviteTeamMemberInput{
		ActorUserID: subject.UserID, TeamID: teamID, Email: req.Email, Role: req.Role,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"invitation": teamInvitationDTOFromService(invitation), "token": token, "accept_link": acceptLink})
}

func (h *TeamHandler) UpdateMember(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "Invalid team ID")
		return
	}
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	var req UpdateMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid member request")
		return
	}
	member, err := h.teamService.UpdateMember(c.Request.Context(), subject.UserID, teamID, userID, service.UpdateTeamMemberInput{Role: req.Role, Status: req.Status})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"member": teamMemberDTOFromService(member)})
}

func (h *TeamHandler) RemoveMember(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "Invalid team ID")
		return
	}
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	if err := h.teamService.RemoveMember(c.Request.Context(), subject.UserID, teamID, userID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "member removed"})
}

type acceptInvitationRequest struct {
	Token string `json:"token" binding:"required"`
}

type transferOwnershipRequest struct {
	UserID int64 `json:"user_id" binding:"required"`
}

type updateTeamSettingsRequest struct {
	Name *string `json:"name" binding:"omitempty,min=1,max=120"`
}

// UpdateTeamSettings handles PATCH /teams/:id. Requires team.settings.manage (owner/admin).
func (h *TeamHandler) UpdateTeamSettings(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "Invalid team ID")
		return
	}
	var req updateTeamSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid team settings request")
		return
	}
	team, err := h.teamService.UpdateTeamSettings(c.Request.Context(), subject.UserID, teamID, service.UpdateTeamSettingsInput{Name: req.Name})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"team": teamSummaryDTOFromService(team)})
}

// teamSummaryDTO is the minimal team shape returned on invitation accept.
type teamSummaryDTO struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	OwnerUserID      int64  `json:"owner_user_id"`
	BillingSubjectID *int64 `json:"billing_subject_id"`
	Status           string `json:"status"`
}

func teamSummaryDTOFromService(t *service.Team) *teamSummaryDTO {
	if t == nil {
		return nil
	}
	return &teamSummaryDTO{
		ID:               t.ID,
		Name:             t.Name,
		Slug:             t.Slug,
		OwnerUserID:      t.OwnerUserID,
		BillingSubjectID: t.BillingSubjectID,
		Status:           t.Status,
	}
}

// teamMemberUserDTO is the minimal user shape embedded in a member row. It
// deliberately omits service.User's sensitive/internal fields (password hash,
// TOTP secret, balance, allowed groups, token version, …).
type teamMemberUserDTO struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// teamMemberDTO is the snake_case member shape returned to non-admin clients.
// The frontend keys off role/status/user to render rows; owner rows are gated on
// member.role === 'owner', so the role must surface under the json name "role".
type teamMemberDTO struct {
	ID               int64              `json:"id"`
	TeamID           int64              `json:"team_id"`
	UserID           int64              `json:"user_id"`
	Role             string             `json:"role"`
	Status           string             `json:"status"`
	JoinedAt         *time.Time         `json:"joined_at"`
	LastActiveAt     *time.Time         `json:"last_active_at"`
	User             *teamMemberUserDTO `json:"user"`
	KeyCount         int                `json:"key_count"`
	Last7dActualCost float64            `json:"last_7d_actual_cost"`
}

// teamInvitationDTO is the pending-invitation shape returned to non-admin clients.
// It excludes the token hash and the internal inviter/acceptor ids.
type teamInvitationDTO struct {
	ID        int64     `json:"id"`
	TeamID    int64     `json:"team_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func teamMemberUserDTOFromService(u *service.User) *teamMemberUserDTO {
	if u == nil {
		return nil
	}
	return &teamMemberUserDTO{ID: u.ID, Username: u.Username, Email: u.Email}
}

func teamMemberDTOFromService(m *service.TeamMember) teamMemberDTO {
	return teamMemberDTO{
		ID:               m.ID,
		TeamID:           m.TeamID,
		UserID:           m.UserID,
		Role:             m.Role,
		Status:           m.Status,
		JoinedAt:         m.JoinedAt,
		LastActiveAt:     m.LastActiveAt,
		User:             teamMemberUserDTOFromService(m.User),
		KeyCount:         m.KeyCount,
		Last7dActualCost: m.Last7dActualCost,
	}
}

func teamMemberDTOsFromService(members []service.TeamMember) []teamMemberDTO {
	out := make([]teamMemberDTO, 0, len(members))
	for i := range members {
		out = append(out, teamMemberDTOFromService(&members[i]))
	}
	return out
}

func teamInvitationDTOFromService(i *service.TeamInvitation) teamInvitationDTO {
	return teamInvitationDTO{
		ID:        i.ID,
		TeamID:    i.TeamID,
		Email:     i.Email,
		Role:      i.Role,
		Status:    i.Status,
		ExpiresAt: i.ExpiresAt,
		CreatedAt: i.CreatedAt,
	}
}

func teamInvitationDTOsFromService(invitations []service.TeamInvitation) []teamInvitationDTO {
	out := make([]teamInvitationDTO, 0, len(invitations))
	for i := range invitations {
		out = append(out, teamInvitationDTOFromService(&invitations[i]))
	}
	return out
}

// PreviewInvitation handles GET /teams/invitations/preview?token=…. It is mounted
// under the authenticated user group WITHOUT a team-membership Require: the invitee
// is not yet a member.
func (h *TeamHandler) PreviewInvitation(c *gin.Context) {
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	token := c.Query("token")
	if token == "" {
		response.BadRequest(c, "token is required")
		return
	}
	preview, err := h.teamService.PreviewInvitation(c.Request.Context(), token)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"team_id":   preview.TeamID,
		"team_name": preview.TeamName,
		"role":      preview.Role,
		"email":     preview.Email,
		"status":    preview.Status,
		"expired":   preview.Expired,
	})
}

// AcceptInvitation handles POST /teams/invitations/accept. Mounted under the
// authenticated user group WITHOUT a Require (the invitee is not yet a member).
// On success it returns the joined team summary and the new membership.
func (h *TeamHandler) AcceptInvitation(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req acceptInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid invitation request")
		return
	}
	member, err := h.teamService.AcceptInvitation(c.Request.Context(), subject.UserID, req.Token)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var teamDTO *teamSummaryDTO
	if team, terr := h.teamService.GetTeam(c.Request.Context(), member.TeamID); terr == nil {
		teamDTO = teamSummaryDTOFromService(team)
	}
	response.Success(c, gin.H{"team": teamDTO, "member": teamMemberDTOFromService(member)})
}

// TransferOwnership handles POST /teams/:id/transfer-ownership. Owner-only; the
// service enforces that the actor is the current owner.
func (h *TeamHandler) TransferOwnership(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "Invalid team ID")
		return
	}
	var req transferOwnershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid transfer request")
		return
	}
	if err := h.teamService.TransferOwnership(c.Request.Context(), subject.UserID, teamID, req.UserID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ownership transferred"})
}

// UsageByMember handles GET /teams/:id/usage/by-member.
// Returns per-actor usage aggregates for the team within [start_date, end_date].
// Gated by team.usage.view.all (owner/admin only).
func (h *TeamHandler) UsageByMember(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "Invalid team ID")
		return
	}

	// 解析 start_date / end_date（同 parseUsageDateRange 风格）；默认近 30 天。
	now := timezone.Now()
	startTime := now.AddDate(0, 0, -30)
	endTime := now
	if s := c.Query("start_date"); s != "" {
		if t, perr := timezone.ParseInLocation("2006-01-02", s); perr == nil {
			startTime = t
		}
	}
	if s := c.Query("end_date"); s != "" {
		if t, perr := timezone.ParseInLocation("2006-01-02", s); perr == nil {
			endTime = t.AddDate(0, 0, 1) // 半开区间上界
		}
	}

	items, err := h.teamService.UsageByMember(c.Request.Context(), subject.UserID, teamID, startTime, endTime)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": items})
}

// DissolveTeam handles DELETE /teams/:id. Owner-only (team.dissolve); irreversible.
func (h *TeamHandler) DissolveTeam(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "Invalid team ID")
		return
	}
	if err := h.teamService.DissolveTeam(c.Request.Context(), subject.UserID, teamID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "team dissolved"})
}
