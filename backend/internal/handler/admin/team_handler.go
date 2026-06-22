package admin

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AdminTeamService is the admin-facing subset of *service.TeamService consumed by
// AdminTeamHandler. *service.TeamService satisfies it.
type AdminTeamService interface {
	AdminListTeams(ctx context.Context, filter service.AdminTeamListFilter, params pagination.PaginationParams) ([]service.AdminTeamSummary, *pagination.PaginationResult, error)
	AdminCreateTeam(ctx context.Context, input service.AdminCreateTeamInput) (*service.AdminTeamSummary, error)
	AdminGetTeam(ctx context.Context, teamID int64) (*service.AdminTeamSummary, []service.TeamMember, []service.TeamInvitation, error)
	AdminUpdateTeam(ctx context.Context, teamID int64, input service.AdminUpdateTeamInput) (*service.AdminTeamSummary, error)
	AdminAddMember(ctx context.Context, input service.AdminAddMemberInput) (*service.TeamMember, error)
	AdminUpdateMember(ctx context.Context, adminUserID, teamID, userID int64, input service.UpdateTeamMemberInput) (*service.TeamMember, error)
	AdminRemoveMember(ctx context.Context, adminUserID, teamID, userID int64) error
	AdminTransferOwnership(ctx context.Context, teamID, newOwnerUserID int64) error
}

// AdminTeamHandler handles platform-admin team management. Access is gated by the
// admin auth middleware at the route level; no team-membership checks apply.
type AdminTeamHandler struct {
	teamService AdminTeamService
}

// NewAdminTeamHandler creates a new admin team handler. *service.TeamService is
// passed directly (it implements AdminTeamService).
func NewAdminTeamHandler(teamService *service.TeamService) *AdminTeamHandler {
	return &AdminTeamHandler{teamService: teamService}
}

// --- DTOs ---------------------------------------------------------------------

type adminTeamUserDTO struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

type adminTeamDTO struct {
	ID               int64             `json:"id"`
	Name             string            `json:"name"`
	Slug             string            `json:"slug"`
	Status           string            `json:"status"`
	OwnerUserID      int64             `json:"owner_user_id"`
	Owner            *adminTeamUserDTO `json:"owner"`
	BillingSubjectID *int64            `json:"billing_subject_id"`
	Balance          float64           `json:"balance"`
	MemberCount      int               `json:"member_count"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type adminTeamMemberDTO struct {
	ID           int64             `json:"id"`
	TeamID       int64             `json:"team_id"`
	UserID       int64             `json:"user_id"`
	Role         string            `json:"role"`
	Status       string            `json:"status"`
	JoinedAt     *time.Time        `json:"joined_at"`
	LastActiveAt *time.Time        `json:"last_active_at"`
	User         *adminTeamUserDTO `json:"user"`
}

type adminTeamInvitationDTO struct {
	ID        int64     `json:"id"`
	TeamID    int64     `json:"team_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func adminTeamUserDTOFromService(u *service.User) *adminTeamUserDTO {
	if u == nil {
		return nil
	}
	return &adminTeamUserDTO{ID: u.ID, Username: u.Username, Email: u.Email}
}

func adminTeamDTOFromSummary(s *service.AdminTeamSummary) adminTeamDTO {
	return adminTeamDTO{
		ID:               s.ID,
		Name:             s.Name,
		Slug:             s.Slug,
		Status:           s.Status,
		OwnerUserID:      s.OwnerUserID,
		Owner:            adminTeamUserDTOFromService(s.OwnerUser),
		BillingSubjectID: s.BillingSubjectID,
		Balance:          s.Balance,
		MemberCount:      s.MemberCount,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

func adminTeamMemberDTOFromService(m *service.TeamMember) adminTeamMemberDTO {
	return adminTeamMemberDTO{
		ID:           m.ID,
		TeamID:       m.TeamID,
		UserID:       m.UserID,
		Role:         m.Role,
		Status:       m.Status,
		JoinedAt:     m.JoinedAt,
		LastActiveAt: m.LastActiveAt,
		User:         adminTeamUserDTOFromService(m.User),
	}
}

func adminTeamInvitationDTOFromService(i *service.TeamInvitation) adminTeamInvitationDTO {
	return adminTeamInvitationDTO{
		ID:        i.ID,
		TeamID:    i.TeamID,
		Email:     i.Email,
		Role:      i.Role,
		Status:    i.Status,
		ExpiresAt: i.ExpiresAt,
		// TeamInvitation does not carry a CreatedAt field on the service model;
		// invitations are returned with the zero value for created_at.
		CreatedAt: time.Time{},
	}
}

// --- Requests -----------------------------------------------------------------

type adminCreateTeamRequest struct {
	Name        string `json:"name" binding:"required"`
	Slug        string `json:"slug"`
	OwnerUserID int64  `json:"owner_user_id"`
	OwnerEmail  string `json:"owner_email" binding:"omitempty,email"`
}

type adminUpdateTeamRequest struct {
	Name   *string `json:"name"`
	Status *string `json:"status" binding:"omitempty,oneof=active disabled"`
}

type adminAddTeamMemberRequest struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email" binding:"omitempty,email"`
	Role   string `json:"role" binding:"required,oneof=admin billing developer viewer"`
}

type adminUpdateTeamMemberRequest struct {
	Role   *string `json:"role" binding:"omitempty,oneof=admin billing developer viewer"`
	Status *string `json:"status" binding:"omitempty,oneof=active suspended"`
}

type adminTransferOwnershipRequest struct {
	UserID int64 `json:"user_id" binding:"required"`
}

// --- Handlers -----------------------------------------------------------------

// List handles GET /api/v1/admin/teams
func (h *AdminTeamHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)

	search := strings.TrimSpace(c.Query("search"))
	if runes := []rune(search); len(runes) > 100 {
		search = string(runes[:100])
	}

	filter := service.AdminTeamListFilter{
		Search: search,
		Status: strings.TrimSpace(c.Query("status")),
	}
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}

	teams, result, err := h.teamService.AdminListTeams(c.Request.Context(), filter, params)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	items := make([]adminTeamDTO, 0, len(teams))
	for i := range teams {
		items = append(items, adminTeamDTOFromSummary(&teams[i]))
	}

	response.PaginatedWithResult(c, items, &response.PaginationResult{
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
		Pages:    result.Pages,
	})
}

// Create handles POST /api/v1/admin/teams. It creates a team and assigns
// ownership to a resolved user (by owner_user_id or owner_email); the requesting
// admin is recorded as the creator. The slug is auto-generated from the name when
// not provided.
func (h *AdminTeamHandler) Create(c *gin.Context) {
	var req adminCreateTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.OwnerUserID <= 0 && strings.TrimSpace(req.OwnerEmail) == "" {
		response.BadRequest(c, "owner_user_id or owner_email is required")
		return
	}

	adminUserID := int64(0)
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		adminUserID = subject.UserID
	}

	summary, err := h.teamService.AdminCreateTeam(c.Request.Context(), service.AdminCreateTeamInput{
		Name:        req.Name,
		Slug:        req.Slug,
		OwnerUserID: req.OwnerUserID,
		OwnerEmail:  req.OwnerEmail,
		AdminUserID: adminUserID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	team := adminTeamDTOFromSummary(summary)
	response.Success(c, gin.H{"team": team})
}

// GetByID handles GET /api/v1/admin/teams/:id
func (h *AdminTeamHandler) GetByID(c *gin.Context) {
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "Invalid team ID")
		return
	}

	summary, members, invitations, err := h.teamService.AdminGetTeam(c.Request.Context(), teamID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	memberDTOs := make([]adminTeamMemberDTO, 0, len(members))
	for i := range members {
		memberDTOs = append(memberDTOs, adminTeamMemberDTOFromService(&members[i]))
	}
	invitationDTOs := make([]adminTeamInvitationDTO, 0, len(invitations))
	for i := range invitations {
		invitationDTOs = append(invitationDTOs, adminTeamInvitationDTOFromService(&invitations[i]))
	}

	team := adminTeamDTOFromSummary(summary)
	response.Success(c, gin.H{
		"team":        team,
		"members":     memberDTOs,
		"invitations": invitationDTOs,
	})
}

// Update handles PATCH /api/v1/admin/teams/:id
func (h *AdminTeamHandler) Update(c *gin.Context) {
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "Invalid team ID")
		return
	}

	var req adminUpdateTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	summary, err := h.teamService.AdminUpdateTeam(c.Request.Context(), teamID, service.AdminUpdateTeamInput{
		Name:   req.Name,
		Status: req.Status,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	team := adminTeamDTOFromSummary(summary)
	response.Success(c, gin.H{"team": team})
}

// AddMember handles POST /api/v1/admin/teams/:id/members
func (h *AdminTeamHandler) AddMember(c *gin.Context) {
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "Invalid team ID")
		return
	}

	var req adminAddTeamMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.UserID <= 0 && strings.TrimSpace(req.Email) == "" {
		response.BadRequest(c, "user_id or email is required")
		return
	}

	adminUserID := int64(0)
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		adminUserID = subject.UserID
	}

	member, err := h.teamService.AdminAddMember(c.Request.Context(), service.AdminAddMemberInput{
		TeamID:      teamID,
		UserID:      req.UserID,
		Email:       req.Email,
		Role:        req.Role,
		AdminUserID: adminUserID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	dto := adminTeamMemberDTOFromService(member)
	response.Success(c, gin.H{"member": dto})
}

// UpdateMember handles PATCH /api/v1/admin/teams/:id/members/:user_id
func (h *AdminTeamHandler) UpdateMember(c *gin.Context) {
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

	var req adminUpdateTeamMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	adminUserID := int64(0)
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		adminUserID = subject.UserID
	}

	member, err := h.teamService.AdminUpdateMember(c.Request.Context(), adminUserID, teamID, userID, service.UpdateTeamMemberInput{
		Role:   req.Role,
		Status: req.Status,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	dto := adminTeamMemberDTOFromService(member)
	response.Success(c, gin.H{"member": dto})
}

// RemoveMember handles DELETE /api/v1/admin/teams/:id/members/:user_id
func (h *AdminTeamHandler) RemoveMember(c *gin.Context) {
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

	adminUserID := int64(0)
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		adminUserID = subject.UserID
	}

	if err := h.teamService.AdminRemoveMember(c.Request.Context(), adminUserID, teamID, userID); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "member removed"})
}

// TransferOwnership handles POST /api/v1/admin/teams/:id/transfer-ownership. No
// membership gating on the admin; the service still validates the new owner is an
// active member and demotes the previous owner to admin.
func (h *AdminTeamHandler) TransferOwnership(c *gin.Context) {
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "Invalid team ID")
		return
	}

	var req adminTransferOwnershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	if err := h.teamService.AdminTransferOwnership(c.Request.Context(), teamID, req.UserID); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "ownership transferred"})
}
