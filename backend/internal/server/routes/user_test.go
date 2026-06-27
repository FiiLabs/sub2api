package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestRegisterUserRoutesExposesInvitationPreviewAndAccept(t *testing.T) {
	router := newUserRoutesTestRouter()

	// Preview is mounted (not captured by "/:id/members"): with the stub repo
	// returning no invitation, the handler reaches the service and yields 400
	// (invalid token), proving the route is wired rather than 404.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/invitations/preview?token=whatever", nil)
	req.Header.Set(middleware.SubjectHeader, "301")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.NotContains(t, rec.Body.String(), "404")

	// Accept is mounted too.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/teams/invitations/accept", strings.NewReader(`{"token":"whatever"}`))
	req.Header.Set(middleware.SubjectHeader, "301")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	// Invalid token -> 400 (route reached the service, not a 404 routing miss).
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRegisterUserRoutesExposesTransferOwnership(t *testing.T) {
	router := newUserRoutesTestRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/44/transfer-ownership", strings.NewReader(`{"user_id":8}`))
	req.Header.Set(middleware.SubjectHeader, "301")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	// routeTeamRepo: actor (21) is owner (GetTeamByID returns Team{ID:44} with
	// OwnerUserID 0) — so ownership check fails with 403. Either way the route is
	// mounted and reaches the service (not a 404).
	require.NotEqual(t, http.StatusNotFound, rec.Code)
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
	}}}, nil, nil, nil, nil)

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
