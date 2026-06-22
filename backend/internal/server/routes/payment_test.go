package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterPaymentRoutesRejectsUnavailableWorkspaceSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	RegisterPaymentRoutes(
		v1,
		&handler.PaymentHandler{},
		nil,
		nil,
		middleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 17})
			c.Next()
		}),
		middleware.AdminAuthMiddleware(func(c *gin.Context) { c.Next() }),
		nil,
		service.NewTeamService(routeTeamRepo{workspaces: []service.WorkspaceSubject{{
			BillingSubjectID: 201,
			Type:             domain.BillingSubjectTypeTeam,
			TeamID:           33,
			Name:             "Core Team",
			Role:             domain.TeamRoleAdmin,
			Permissions:      domain.TeamRolePermissions(domain.TeamRoleAdmin),
		}}}, nil, nil),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payment/orders", nil)
	req.Header.Set(middleware.SubjectHeader, "999")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRegisterPaymentRoutesRejectsTeamWorkspaceUntilPaymentIsSubjectAware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	RegisterPaymentRoutes(
		v1,
		&handler.PaymentHandler{},
		nil,
		nil,
		middleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 17})
			c.Next()
		}),
		middleware.AdminAuthMiddleware(func(c *gin.Context) { c.Next() }),
		nil,
		service.NewTeamService(routeTeamRepo{workspaces: []service.WorkspaceSubject{
			{
				BillingSubjectID: 101,
				Type:             domain.BillingSubjectTypeUser,
				UserID:           17,
				Name:             "Personal",
				Role:             domain.TeamRoleOwner,
				Permissions:      domain.TeamRolePermissions(domain.TeamRoleOwner),
			},
			{
				BillingSubjectID: 201,
				Type:             domain.BillingSubjectTypeTeam,
				TeamID:           33,
				Name:             "Core Team",
				Role:             domain.TeamRoleAdmin,
				Permissions:      domain.TeamRolePermissions(domain.TeamRoleAdmin),
			},
		}}, nil, nil),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payment/orders", nil)
	req.Header.Set(middleware.SubjectHeader, "201")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

type routeTeamRepo struct {
	workspaces []service.WorkspaceSubject
}

func (r routeTeamRepo) CreateTeam(_ context.Context, _ service.CreateTeamInput) (*service.Team, error) {
	return nil, nil
}

func (r routeTeamRepo) GetMembership(_ context.Context, teamID, userID int64) (*service.TeamMember, error) {
	return &service.TeamMember{
		TeamID: teamID,
		UserID: userID,
		Role:   domain.TeamRoleOwner,
		Status: domain.TeamMemberStatusActive,
	}, nil
}

func (r routeTeamRepo) ListWorkspaces(_ context.Context, _ int64) ([]service.WorkspaceSubject, error) {
	return r.workspaces, nil
}

func (r routeTeamRepo) ListMembers(_ context.Context, _ int64) ([]service.TeamMember, []service.TeamInvitation, error) {
	return []service.TeamMember{}, []service.TeamInvitation{}, nil
}

func (r routeTeamRepo) InviteMember(_ context.Context, _ service.InviteTeamMemberInput) (*service.TeamInvitation, error) {
	return nil, nil
}

func (r routeTeamRepo) GetInvitationByTokenHash(_ context.Context, _ string) (*service.TeamInvitation, error) {
	return nil, service.ErrTeamInvitationInvalid
}

func (r routeTeamRepo) AcceptInvitation(_ context.Context, _, _, _ int64, _ string) (*service.TeamMember, error) {
	return nil, nil
}

func (r routeTeamRepo) TransferOwnership(_ context.Context, _, _, _ int64) error {
	return nil
}

func (r routeTeamRepo) UpdateMember(_ context.Context, _, _, _ int64, _ service.UpdateTeamMemberInput) (*service.TeamMember, error) {
	return nil, nil
}

func (r routeTeamRepo) RemoveMember(_ context.Context, _, _, _ int64) error {
	return nil
}

func (r routeTeamRepo) AdminListTeams(_ context.Context, _ service.AdminTeamListFilter, _ pagination.PaginationParams) ([]service.AdminTeamSummary, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (r routeTeamRepo) GetTeamByID(_ context.Context, teamID int64) (*service.Team, error) {
	return &service.Team{ID: teamID}, nil
}

func (r routeTeamRepo) AdminGetTeamSummary(_ context.Context, teamID int64) (*service.AdminTeamSummary, error) {
	return &service.AdminTeamSummary{Team: service.Team{ID: teamID}}, nil
}

func (r routeTeamRepo) AddMember(_ context.Context, teamID, userID int64, role string, _ int64) (*service.TeamMember, error) {
	return &service.TeamMember{TeamID: teamID, UserID: userID, Role: role, Status: domain.TeamMemberStatusActive}, nil
}

func (r routeTeamRepo) UpdateTeam(_ context.Context, teamID int64, _ *string, _ *string) (*service.Team, error) {
	return &service.Team{ID: teamID}, nil
}
