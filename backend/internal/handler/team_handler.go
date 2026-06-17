package handler

import (
	"context"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type WorkspaceService interface {
	ListWorkspaces(ctx context.Context, userID int64) ([]service.WorkspaceSubject, error)
}

type TeamHandler struct {
	teamService WorkspaceService
}

func NewTeamHandler(teamService *service.TeamService) *TeamHandler {
	return &TeamHandler{teamService: teamService}
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
	response.Error(c, http.StatusNotImplemented, "Team creation endpoint is not ready")
}

func (h *TeamHandler) ListMembers(c *gin.Context) {
	response.Error(c, http.StatusNotImplemented, "Team members endpoint is not ready")
}

func (h *TeamHandler) InviteMember(c *gin.Context) {
	response.Error(c, http.StatusNotImplemented, "Team invitations endpoint is not ready")
}

func (h *TeamHandler) UpdateMember(c *gin.Context) {
	response.Error(c, http.StatusNotImplemented, "Team member update endpoint is not ready")
}

func (h *TeamHandler) RemoveMember(c *gin.Context) {
	response.Error(c, http.StatusNotImplemented, "Team member removal endpoint is not ready")
}
