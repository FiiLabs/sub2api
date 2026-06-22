package routes

import (
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

func TestRegisterUserRoutesListsResolvedWorkspaces(t *testing.T) {
	router := newUserRoutesTestRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
	req.Header.Set(middleware.SubjectHeader, "301")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"billing_subject_id":301`)
	require.Contains(t, rec.Body.String(), `"Personal"`)
}

func TestRegisterUserRoutesExposesTeamMemberEndpoints(t *testing.T) {
	router := newUserRoutesTestRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/44/members", nil)
	req.Header.Set(middleware.SubjectHeader, "301")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"members"`)
}

func newUserRoutesTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	teamService := service.NewTeamService(routeTeamRepo{workspaces: []service.WorkspaceSubject{{
		BillingSubjectID: 301,
		Type:             domain.BillingSubjectTypeUser,
		UserID:           21,
		Name:             "Personal",
		Role:             domain.TeamRoleOwner,
		Permissions:      domain.TeamRolePermissions(domain.TeamRoleOwner),
	}}}, nil, nil)

	RegisterUserRoutes(
		v1,
		&handler.Handlers{Team: handler.NewTeamHandler(teamService)},
		middleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 21})
			c.Next()
		}),
		nil,
		teamService,
	)
	return router
}
