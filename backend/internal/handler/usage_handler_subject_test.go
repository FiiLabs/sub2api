package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newUserUsageSubjectTestRouter(repo *userUsageRepoCapture, subject middleware2.AuthSubject) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), subject)
		c.Next()
	})
	router.GET("/usage", handler.List)
	return router
}

func TestUsageListScopesByBillingSubject(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{UserID: 42, BillingSubjectID: 100, TeamID: 7}
	router := newUserUsageSubjectTestRouter(repo, subject)

	req := httptest.NewRequest(http.MethodGet, "/usage", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(100), repo.listFilters.BillingSubjectID)
	require.Equal(t, int64(0), repo.listFilters.UserID)
	require.Equal(t, int64(0), repo.listFilters.ActorUserID)
}

func TestUsageListAppliesActorUserFilter(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{UserID: 42, BillingSubjectID: 100, TeamID: 7}
	router := newUserUsageSubjectTestRouter(repo, subject)

	req := httptest.NewRequest(http.MethodGet, "/usage?actor_user_id=55", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(100), repo.listFilters.BillingSubjectID)
	require.Equal(t, int64(55), repo.listFilters.ActorUserID)
}

func TestUsageListRejectsInvalidActorUser(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{UserID: 42, BillingSubjectID: 100, TeamID: 7}
	router := newUserUsageSubjectTestRouter(repo, subject)

	req := httptest.NewRequest(http.MethodGet, "/usage?actor_user_id=-1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUsageListFallsBackToUserScopeWithoutSubject(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{UserID: 42}
	router := newUserUsageSubjectTestRouter(repo, subject)

	req := httptest.NewRequest(http.MethodGet, "/usage", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(42), repo.listFilters.UserID)
	require.Equal(t, int64(0), repo.listFilters.BillingSubjectID)
}
