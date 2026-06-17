package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler"
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
		}}}, nil),
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
		}}, nil),
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

func (r routeTeamRepo) GetMembership(_ context.Context, _, _ int64) (*service.TeamMember, error) {
	return nil, nil
}

func (r routeTeamRepo) ListWorkspaces(_ context.Context, _ int64) ([]service.WorkspaceSubject, error) {
	return r.workspaces, nil
}
