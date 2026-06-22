package handler

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TeamHTTPService interface {
	ListWorkspaces(ctx context.Context, userID int64) ([]service.WorkspaceSubject, error)
	CreateTeam(ctx context.Context, input service.CreateTeamInput) (*service.Team, error)
	ListMembers(ctx context.Context, actorUserID, teamID int64) ([]service.TeamMember, []service.TeamInvitation, error)
	InviteMember(ctx context.Context, input service.InviteTeamMemberInput) (*service.TeamInvitation, string, error)
	UpdateMember(ctx context.Context, actorUserID, teamID, userID int64, input service.UpdateTeamMemberInput) (*service.TeamMember, error)
	RemoveMember(ctx context.Context, actorUserID, teamID, userID int64) error
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
	response.Success(c, gin.H{"members": members, "invitations": invitations})
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
	invitation, token, err := h.teamService.InviteMember(c.Request.Context(), service.InviteTeamMemberInput{
		ActorUserID: subject.UserID, TeamID: teamID, Email: req.Email, Role: req.Role,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"invitation": invitation, "token": token})
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
	response.Success(c, gin.H{"member": member})
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
