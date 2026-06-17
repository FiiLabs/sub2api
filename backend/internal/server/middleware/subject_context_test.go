package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSubjectContextDefaultsToPersonalWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyUser), AuthSubject{UserID: 11, Concurrency: 3})
		c.Next()
	})
	router.Use(SubjectContextMiddleware(subjectTeamServiceAdapter{workspaces: []service.WorkspaceSubject{
		{
			BillingSubjectID: 101,
			Type:             domain.BillingSubjectTypeUser,
			UserID:           11,
			Name:             "Personal",
			Role:             domain.TeamRoleOwner,
			Permissions:      domain.TeamRolePermissions(domain.TeamRoleOwner),
			Balance:          9.5,
		},
	}}))
	router.GET("/probe", func(c *gin.Context) {
		subject, ok := GetAuthSubjectFromContext(c)
		require.True(t, ok)
		require.Equal(t, int64(101), subject.BillingSubjectID)
		require.Equal(t, domain.BillingSubjectTypeUser, subject.SubjectType)
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestSubjectContextRejectsMalformedSubjectHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	probeCalled := false
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyUser), AuthSubject{UserID: 11, Concurrency: 3})
		c.Next()
	})
	router.Use(SubjectContextMiddleware(subjectTeamServiceAdapter{workspaces: []service.WorkspaceSubject{
		{
			BillingSubjectID: 101,
			Type:             domain.BillingSubjectTypeUser,
			UserID:           11,
			Name:             "Personal",
			Role:             domain.TeamRoleOwner,
			Permissions:      domain.TeamRolePermissions(domain.TeamRoleOwner),
			Balance:          9.5,
		},
	}}))
	router.GET("/probe", func(c *gin.Context) {
		probeCalled = true
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set(SubjectHeader, "not-a-number")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, probeCalled)
}

func TestSubjectContextSelectsTeamByHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyUser), AuthSubject{UserID: 11, Concurrency: 3})
		c.Next()
	})
	router.Use(SubjectContextMiddleware(subjectTeamServiceAdapter{workspaces: []service.WorkspaceSubject{
		{
			BillingSubjectID: 101,
			Type:             domain.BillingSubjectTypeUser,
			UserID:           11,
			Name:             "Personal",
			Role:             domain.TeamRoleOwner,
			Permissions:      domain.TeamRolePermissions(domain.TeamRoleOwner),
		},
		{
			BillingSubjectID: 202,
			Type:             domain.BillingSubjectTypeTeam,
			TeamID:           22,
			Name:             "Team",
			Role:             domain.TeamRoleDeveloper,
			Permissions:      domain.TeamRolePermissions(domain.TeamRoleDeveloper),
		},
	}}))
	router.GET("/probe", func(c *gin.Context) {
		subject, ok := GetAuthSubjectFromContext(c)
		require.True(t, ok)
		require.Equal(t, int64(202), subject.BillingSubjectID)
		require.Equal(t, int64(22), subject.TeamID)
		require.Equal(t, domain.TeamRoleDeveloper, subject.TeamRole)
		require.True(t, subject.Permissions[domain.TeamPermissionManageKeys])
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set(TeamHeader, "22")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
}

type subjectTeamServiceAdapter struct {
	workspaces []service.WorkspaceSubject
}

func (a subjectTeamServiceAdapter) ListWorkspaces(_ context.Context, _ int64) ([]service.WorkspaceSubject, error) {
	return a.workspaces, nil
}
