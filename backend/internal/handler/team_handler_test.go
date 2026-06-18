package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTeamHandlerListWorkspaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &TeamHandler{teamService: teamHandlerServiceAdapter{workspaces: []service.WorkspaceSubject{
		{BillingSubjectID: 1, Type: "user", UserID: 9, Name: "Personal", Balance: 3.25},
	}}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 9})
		c.Next()
	})
	router.GET("/api/v1/workspaces", h.ListWorkspaces)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"billing_subject_id":1`)
	require.Contains(t, rec.Body.String(), `"Personal"`)
}

type teamHandlerServiceAdapter struct {
	workspaces []service.WorkspaceSubject
}

func (a teamHandlerServiceAdapter) ListWorkspaces(_ context.Context, _ int64) ([]service.WorkspaceSubject, error) {
	return a.workspaces, nil
}

func (a teamHandlerServiceAdapter) CreateTeam(_ context.Context, input service.CreateTeamInput) (*service.Team, error) {
	return &service.Team{ID: 1, Name: input.Name, Slug: input.Slug, OwnerUserID: input.ActorUserID}, nil
}

func (a teamHandlerServiceAdapter) ListMembers(context.Context, int64, int64) ([]service.TeamMember, []service.TeamInvitation, error) {
	return nil, nil, nil
}

func (a teamHandlerServiceAdapter) InviteMember(_ context.Context, input service.InviteTeamMemberInput) (*service.TeamInvitation, string, error) {
	return &service.TeamInvitation{TeamID: input.TeamID, Email: input.Email, Role: input.Role, Status: "pending", ExpiresAt: input.ExpiresAt}, "plain-token", nil
}

func (a teamHandlerServiceAdapter) UpdateMember(_ context.Context, _ int64, teamID int64, userID int64, input service.UpdateTeamMemberInput) (*service.TeamMember, error) {
	role := "viewer"
	if input.Role != nil {
		role = *input.Role
	}
	return &service.TeamMember{TeamID: teamID, UserID: userID, Role: role, Status: "active"}, nil
}

func (a teamHandlerServiceAdapter) RemoveMember(context.Context, int64, int64, int64) error {
	return nil
}

type teamMemberHandlerServiceStub struct {
	workspaces  []service.WorkspaceSubject
	members     []service.TeamMember
	invitations []service.TeamInvitation
}

func (s *teamMemberHandlerServiceStub) ListWorkspaces(_ context.Context, userID int64) ([]service.WorkspaceSubject, error) {
	if len(s.workspaces) > 0 {
		return s.workspaces, nil
	}
	return []service.WorkspaceSubject{{BillingSubjectID: 2, Type: "team", UserID: userID, TeamID: 7, Name: "Platform", Role: "admin", Permissions: map[string]bool{"team.members.manage": true}}}, nil
}

func (s *teamMemberHandlerServiceStub) CreateTeam(_ context.Context, input service.CreateTeamInput) (*service.Team, error) {
	return &service.Team{ID: 7, Name: input.Name, Slug: input.Slug, OwnerUserID: input.ActorUserID}, nil
}

func (s *teamMemberHandlerServiceStub) ListMembers(_ context.Context, _ int64, _ int64) ([]service.TeamMember, []service.TeamInvitation, error) {
	return s.members, s.invitations, nil
}

func (s *teamMemberHandlerServiceStub) InviteMember(_ context.Context, input service.InviteTeamMemberInput) (*service.TeamInvitation, string, error) {
	return &service.TeamInvitation{ID: 3, TeamID: input.TeamID, Email: input.Email, Role: input.Role, Status: "pending", ExpiresAt: input.ExpiresAt}, "plain-token", nil
}

func (s *teamMemberHandlerServiceStub) UpdateMember(_ context.Context, _ int64, teamID int64, userID int64, input service.UpdateTeamMemberInput) (*service.TeamMember, error) {
	role := "viewer"
	status := "active"
	if input.Role != nil {
		role = *input.Role
	}
	if input.Status != nil {
		status = *input.Status
	}
	return &service.TeamMember{TeamID: teamID, UserID: userID, Role: role, Status: status}, nil
}

func (s *teamMemberHandlerServiceStub) RemoveMember(_ context.Context, _ int64, _ int64, _ int64) error {
	return nil
}

func TestTeamHandlerListMembersRequiresTeamPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	teamSvc := &teamMemberHandlerServiceStub{
		members: []service.TeamMember{{ID: 1, TeamID: 7, UserID: 11, Role: "owner", Status: "active"}},
	}
	h := NewTeamHandler(teamSvc)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11, BillingSubjectID: 2, SubjectType: "team", TeamID: 7, Permissions: map[string]bool{"team.members.manage": true}})
		c.Next()
	})
	router.GET("/api/v1/teams/:id/members", h.ListMembers)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/7/members", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"members"`)
}

func TestTeamHandlerInviteMemberReturnsToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewTeamHandler(&teamMemberHandlerServiceStub{})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11, BillingSubjectID: 2, SubjectType: "team", TeamID: 7, Permissions: map[string]bool{"team.members.manage": true}})
		c.Next()
	})
	router.POST("/api/v1/teams/:id/invitations", h.InviteMember)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/7/invitations", strings.NewReader(`{"email":"new@example.com","role":"developer"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"token"`)
	require.Contains(t, rec.Body.String(), `"invitation"`)
}
