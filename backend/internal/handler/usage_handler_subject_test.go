package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
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

func TestUsageListForcesActorToSelfForMember(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{
		UserID: 42, BillingSubjectID: 100, SubjectType: domain.BillingSubjectTypeTeam, TeamID: 7,
		Permissions: map[string]bool{domain.TeamPermissionViewUsage: true},
	}
	router := newUserUsageSubjectTestRouter(repo, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(42), repo.listFilters.ActorUserID)
}

func TestUsageListRejectsForeignActorForMember(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{
		UserID: 42, BillingSubjectID: 100, SubjectType: domain.BillingSubjectTypeTeam, TeamID: 7,
		Permissions: map[string]bool{domain.TeamPermissionViewUsage: true},
	}
	router := newUserUsageSubjectTestRouter(repo, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage?actor_user_id=99", nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestUsageListAllowsForeignActorForOwner(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{
		UserID: 1, BillingSubjectID: 100, SubjectType: domain.BillingSubjectTypeTeam, TeamID: 7,
		Permissions: map[string]bool{domain.TeamPermissionViewUsageAll: true},
	}
	router := newUserUsageSubjectTestRouter(repo, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage?actor_user_id=99", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(99), repo.listFilters.ActorUserID)
}

// GetSubjectStatsAggregated stub — captures billingSubjectID and actorUserID for assertions.
func (s *userUsageRepoCapture) GetSubjectStatsAggregated(_ context.Context, billingSubjectID, actorUserID int64, _, _ time.Time) (*usagestats.UsageStats, error) {
	s.statsSubjectID = billingSubjectID
	s.statsActorID = actorUserID
	return &usagestats.UsageStats{}, nil
}

func TestUsageStatsScopesBySubjectAndSelfForMember(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{
		UserID: 42, BillingSubjectID: 100, SubjectType: domain.BillingSubjectTypeTeam, TeamID: 7,
		Permissions: map[string]bool{domain.TeamPermissionViewUsage: true},
	}
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(middleware2.ContextKeyUser), subject); c.Next() })
	router.GET("/usage/stats", handler.Stats)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/stats", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(100), repo.statsSubjectID)
	require.Equal(t, int64(42), repo.statsActorID) // member forced to self
}

// GetDashboardStatsBySubject stub — captures billingSubjectID and actorUserID for assertions.
func (s *userUsageRepoCapture) GetDashboardStatsBySubject(_ context.Context, billingSubjectID, actorUserID int64) (*usagestats.UserDashboardStats, error) {
	s.dashSubjectID = billingSubjectID
	s.dashActorID = actorUserID
	return &usagestats.UserDashboardStats{}, nil
}

func TestDashboardStatsScopesBySubjectForMember(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{
		UserID: 42, BillingSubjectID: 100, SubjectType: domain.BillingSubjectTypeTeam,
		Permissions: map[string]bool{domain.TeamPermissionViewUsage: true},
	}
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(middleware2.ContextKeyUser), subject); c.Next() })
	router.GET("/usage/dashboard/stats", handler.DashboardStats)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/dashboard/stats", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(100), repo.dashSubjectID)
	require.Equal(t, int64(42), repo.dashActorID)
}

// GetSubjectUsageTrend stub — captures billingSubjectID and actorUserID for assertions.
func (s *userUsageRepoCapture) GetSubjectUsageTrend(_ context.Context, billingSubjectID, actorUserID int64, _, _ time.Time, _ string) ([]usagestats.TrendDataPoint, error) {
	s.trendSubjectID = billingSubjectID
	s.trendActorID = actorUserID
	return []usagestats.TrendDataPoint{}, nil
}

// GetSubjectModelStats stub — captures billingSubjectID and actorUserID for assertions.
func (s *userUsageRepoCapture) GetSubjectModelStats(_ context.Context, billingSubjectID, actorUserID int64, _, _ time.Time) ([]usagestats.ModelStat, error) {
	s.modelSubjectID = billingSubjectID
	s.modelActorID = actorUserID
	return []usagestats.ModelStat{}, nil
}

func TestDashboardTrendScopesBySubjectForMember(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{
		UserID: 42, BillingSubjectID: 100, SubjectType: domain.BillingSubjectTypeTeam,
		Permissions: map[string]bool{domain.TeamPermissionViewUsage: true},
	}
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(middleware2.ContextKeyUser), subject); c.Next() })
	router.GET("/usage/dashboard/trend", handler.DashboardTrend)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/dashboard/trend", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(100), repo.trendSubjectID)
	require.Equal(t, int64(42), repo.trendActorID) // member forced to self
}

func TestDashboardTrendScopesBySubjectForOwner(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{
		UserID: 1, BillingSubjectID: 100, SubjectType: domain.BillingSubjectTypeTeam,
		Permissions: map[string]bool{domain.TeamPermissionViewUsageAll: true},
	}
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(middleware2.ContextKeyUser), subject); c.Next() })
	router.GET("/usage/dashboard/trend", handler.DashboardTrend)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/dashboard/trend", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(100), repo.trendSubjectID)
	require.Equal(t, int64(0), repo.trendActorID) // owner gets all actors
}

func TestDashboardModelsScopesBySubjectForMember(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{
		UserID: 42, BillingSubjectID: 100, SubjectType: domain.BillingSubjectTypeTeam,
		Permissions: map[string]bool{domain.TeamPermissionViewUsage: true},
	}
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(middleware2.ContextKeyUser), subject); c.Next() })
	router.GET("/usage/dashboard/models", handler.DashboardModels)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/dashboard/models", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(100), repo.modelSubjectID)
	require.Equal(t, int64(42), repo.modelActorID) // member forced to self
}

func TestDashboardModelsScopesBySubjectForOwner(t *testing.T) {
	repo := &userUsageRepoCapture{}
	subject := middleware2.AuthSubject{
		UserID: 1, BillingSubjectID: 100, SubjectType: domain.BillingSubjectTypeTeam,
		Permissions: map[string]bool{domain.TeamPermissionViewUsageAll: true},
	}
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(middleware2.ContextKeyUser), subject); c.Next() })
	router.GET("/usage/dashboard/models", handler.DashboardModels)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/dashboard/models", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(100), repo.modelSubjectID)
	require.Equal(t, int64(0), repo.modelActorID) // owner gets all actors
}
