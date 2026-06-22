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

func (a teamHandlerServiceAdapter) InviteMember(_ context.Context, input service.InviteTeamMemberInput) (*service.TeamInvitation, string, string, error) {
	return &service.TeamInvitation{TeamID: input.TeamID, Email: input.Email, Role: input.Role, Status: "pending", ExpiresAt: input.ExpiresAt}, "plain-token", "/teams/accept?token=plain-token", nil
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

func (a teamHandlerServiceAdapter) PreviewInvitation(_ context.Context, _ string) (*service.InvitationPreview, error) {
	return &service.InvitationPreview{}, nil
}

func (a teamHandlerServiceAdapter) AcceptInvitation(_ context.Context, _ int64, _ string) (*service.TeamMember, error) {
	return &service.TeamMember{}, nil
}

func (a teamHandlerServiceAdapter) TransferOwnership(_ context.Context, _, _, _ int64) error {
	return nil
}

func (a teamHandlerServiceAdapter) GetTeam(_ context.Context, teamID int64) (*service.Team, error) {
	return &service.Team{ID: teamID}, nil
}

type teamMemberHandlerServiceStub struct {
	workspaces  []service.WorkspaceSubject
	members     []service.TeamMember
	invitations []service.TeamInvitation

	previewToken string
	previewErr   error

	acceptActorID int64
	acceptToken   string
	acceptErr     error

	transferArgs [3]int64 // actorUserID, teamID, newOwnerUserID
	transferErr  error
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

func (s *teamMemberHandlerServiceStub) InviteMember(_ context.Context, input service.InviteTeamMemberInput) (*service.TeamInvitation, string, string, error) {
	return &service.TeamInvitation{ID: 3, TeamID: input.TeamID, Email: input.Email, Role: input.Role, Status: "pending", ExpiresAt: input.ExpiresAt}, "plain-token", "/teams/accept?token=plain-token", nil
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

func (s *teamMemberHandlerServiceStub) PreviewInvitation(_ context.Context, token string) (*service.InvitationPreview, error) {
	s.previewToken = token
	if s.previewErr != nil {
		return nil, s.previewErr
	}
	return &service.InvitationPreview{TeamID: 7, TeamName: "Platform", Role: "developer", Email: "new@example.com", Status: "pending", Expired: false}, nil
}

func (s *teamMemberHandlerServiceStub) AcceptInvitation(_ context.Context, actorUserID int64, token string) (*service.TeamMember, error) {
	s.acceptActorID = actorUserID
	s.acceptToken = token
	if s.acceptErr != nil {
		return nil, s.acceptErr
	}
	return &service.TeamMember{ID: 5, TeamID: 7, UserID: actorUserID, Role: "developer", Status: "active"}, nil
}

func (s *teamMemberHandlerServiceStub) TransferOwnership(_ context.Context, actorUserID, teamID, newOwnerUserID int64) error {
	s.transferArgs = [3]int64{actorUserID, teamID, newOwnerUserID}
	return s.transferErr
}

func (s *teamMemberHandlerServiceStub) GetTeam(_ context.Context, teamID int64) (*service.Team, error) {
	bsID := int64(200 + teamID)
	return &service.Team{ID: teamID, Name: "Platform", Slug: "platform", OwnerUserID: 11, BillingSubjectID: &bsID, Status: "active"}, nil
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

// The members payload must use the project's snake_case convention (the frontend
// reads member.role / member.user_id to render owner rows correctly) and must NOT
// leak the embedded user's sensitive fields or the invitation token hash.
func TestTeamHandlerListMembersReturnsSnakeCaseWithoutSensitiveFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	teamSvc := &teamMemberHandlerServiceStub{
		members: []service.TeamMember{
			{
				ID: 1, TeamID: 7, UserID: 11, Role: "owner", Status: "active",
				User: &service.User{
					ID: 11, Username: "owner", Email: "owner@example.com",
					PasswordHash:  "secret-pw-hash",
					Balance:       42.5,
					AllowedGroups: []int64{1, 2},
				},
			},
		},
		invitations: []service.TeamInvitation{
			{ID: 3, TeamID: 7, Email: "dev@example.com", Role: "developer", Status: "pending", TokenHash: "secret-token-hash"},
		},
	}
	h := NewTeamHandler(teamSvc)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11, TeamID: 7, Permissions: map[string]bool{"team.members.manage": true}})
		c.Next()
	})
	router.GET("/api/v1/teams/:id/members", h.ListMembers)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/7/members", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	// snake_case fields the frontend depends on (owner row rendering keys off member.role)
	require.Contains(t, body, `"user_id":11`)
	require.Contains(t, body, `"role":"owner"`)
	require.Contains(t, body, `"username":"owner"`)
	require.Contains(t, body, `"email":"owner@example.com"`)
	require.Contains(t, body, `"expires_at"`)
	require.Contains(t, body, `"created_at"`)

	// no Go-default PascalCase leaking through
	require.NotContains(t, body, `"UserID"`)
	require.NotContains(t, body, `"Role"`)

	// no sensitive / superfluous embedded-user fields
	require.NotContains(t, body, "PasswordHash")
	require.NotContains(t, body, "secret-pw-hash")
	require.NotContains(t, body, "AllowedGroups")
	require.NotContains(t, body, "allowed_groups")

	// the invitation token hash must never reach the client
	require.NotContains(t, body, "TokenHash")
	require.NotContains(t, body, "token_hash")
	require.NotContains(t, body, "secret-token-hash")
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
	require.Contains(t, rec.Body.String(), `"accept_link"`)
}

func TestTeamHandlerPreviewInvitation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &teamMemberHandlerServiceStub{}
	h := NewTeamHandler(stub)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11})
		c.Next()
	})
	router.GET("/api/v1/teams/invitations/preview", h.PreviewInvitation)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/invitations/preview?token=abc123", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `"team_id":7`)
	require.Contains(t, body, `"team_name":"Platform"`)
	require.Contains(t, body, `"role":"developer"`)
	require.Contains(t, body, `"email":"new@example.com"`)
	require.Contains(t, body, `"status":"pending"`)
	require.Contains(t, body, `"expired":false`)
	require.Equal(t, "abc123", stub.previewToken)
}

func TestTeamHandlerPreviewInvitationRequiresToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewTeamHandler(&teamMemberHandlerServiceStub{})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11})
		c.Next()
	})
	router.GET("/api/v1/teams/invitations/preview", h.PreviewInvitation)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/invitations/preview", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTeamHandlerAcceptInvitationReturnsTeamAndMember(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &teamMemberHandlerServiceStub{}
	h := NewTeamHandler(stub)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11})
		c.Next()
	})
	router.POST("/api/v1/teams/invitations/accept", h.AcceptInvitation)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/invitations/accept", strings.NewReader(`{"token":"abc123"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `"team"`)
	require.Contains(t, body, `"member"`)
	require.Contains(t, body, `"billing_subject_id":207`)
	require.Equal(t, "abc123", stub.acceptToken)
	require.Equal(t, int64(11), stub.acceptActorID)
}

func TestTeamHandlerAcceptInvitationPropagatesError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &teamMemberHandlerServiceStub{acceptErr: service.ErrTeamInvitationEmailMismatch}
	h := NewTeamHandler(stub)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11})
		c.Next()
	})
	router.POST("/api/v1/teams/invitations/accept", h.AcceptInvitation)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/invitations/accept", strings.NewReader(`{"token":"abc123"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	// ErrTeamInvitationEmailMismatch is Forbidden -> HTTP 403.
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTeamHandlerTransferOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &teamMemberHandlerServiceStub{}
	h := NewTeamHandler(stub)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11})
		c.Next()
	})
	router.POST("/api/v1/teams/:id/transfer-ownership", h.TransferOwnership)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/7/transfer-ownership", strings.NewReader(`{"user_id":8}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "ownership transferred")
	require.Equal(t, [3]int64{11, 7, 8}, stub.transferArgs)
}
