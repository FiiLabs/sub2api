package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
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

func newUsageGetByIDTestRouter(repo *userUsageRepoCapture, subject middleware2.AuthSubject) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), subject)
		c.Next()
	})
	router.GET("/usage/:id", handler.GetByID)
	return router
}

// 团队成员无 view.all 权限，ActorUserID != self → 403
func TestUsageGetByIDTeamMemberForbiddenForOtherActorRecord(t *testing.T) {
	otherUserID := int64(7)
	repo := &userUsageRepoCapture{
		getByIDResult: &service.UsageLog{
			ID:               5,
			UserID:           otherUserID,
			BillingSubjectID: 100,
			ActorUserID:      &otherUserID,
		},
	}
	subject := middleware2.AuthSubject{
		UserID:           42,
		BillingSubjectID: 100,
		SubjectType:      domain.BillingSubjectTypeTeam,
		TeamID:           7,
		Permissions:      map[string]bool{domain.TeamPermissionViewUsage: true},
	}
	router := newUsageGetByIDTestRouter(repo, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/5", nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// 团队成员无 view.all 权限，ActorUserID == self → 200
func TestUsageGetByIDTeamMemberAllowedForOwnActorRecord(t *testing.T) {
	selfUserID := int64(42)
	repo := &userUsageRepoCapture{
		getByIDResult: &service.UsageLog{
			ID:               5,
			UserID:           selfUserID,
			BillingSubjectID: 100,
			ActorUserID:      &selfUserID,
		},
	}
	subject := middleware2.AuthSubject{
		UserID:           42,
		BillingSubjectID: 100,
		SubjectType:      domain.BillingSubjectTypeTeam,
		TeamID:           7,
		Permissions:      map[string]bool{domain.TeamPermissionViewUsage: true},
	}
	router := newUsageGetByIDTestRouter(repo, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/5", nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

// 团队 owner 有 view.all 权限，任意 actor 的记录 → 200
func TestUsageGetByIDTeamOwnerAllowedForAnyTeamRecord(t *testing.T) {
	otherUserID := int64(7)
	repo := &userUsageRepoCapture{
		getByIDResult: &service.UsageLog{
			ID:               5,
			UserID:           otherUserID,
			BillingSubjectID: 100,
			ActorUserID:      &otherUserID,
		},
	}
	subject := middleware2.AuthSubject{
		UserID:           1,
		BillingSubjectID: 100,
		SubjectType:      domain.BillingSubjectTypeTeam,
		TeamID:           7,
		Permissions:      map[string]bool{domain.TeamPermissionViewUsageAll: true},
	}
	router := newUsageGetByIDTestRouter(repo, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/5", nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

// 个人用户无法访问他人的记录 → 403
func TestUsageGetByIDPersonalForbiddenForOtherUserRecord(t *testing.T) {
	otherUserID := int64(99)
	repo := &userUsageRepoCapture{
		getByIDResult: &service.UsageLog{
			ID:     5,
			UserID: otherUserID,
		},
	}
	subject := middleware2.AuthSubject{UserID: 42}
	router := newUsageGetByIDTestRouter(repo, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/5", nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// 团队记录属于不同计费主体 → 403
func TestUsageGetByIDTeamForbiddenForDifferentBillingSubject(t *testing.T) {
	selfID := int64(42)
	repo := &userUsageRepoCapture{
		getByIDResult: &service.UsageLog{
			ID:               5,
			UserID:           selfID,
			BillingSubjectID: 999, // 不同主体
			ActorUserID:      &selfID,
		},
	}
	subject := middleware2.AuthSubject{
		UserID:           42,
		BillingSubjectID: 100,
		SubjectType:      domain.BillingSubjectTypeTeam,
		TeamID:           7,
		Permissions:      map[string]bool{domain.TeamPermissionViewUsageAll: true},
	}
	router := newUsageGetByIDTestRouter(repo, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/5", nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
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

// ── api_key_id 所有权检查：team 感知 ──────────────────────────────────────────

// usageAPIKeyRepoStub 是一个最小的 APIKeyRepository 桩，只实现 GetByID。
// 所有其他方法通过嵌入 service.APIKeyRepository 接口保持 nil（调用会 panic，
// 但测试中不会到达那些方法）。
type usageAPIKeyRepoStub struct {
	service.APIKeyRepository
	key *service.APIKey
	err error
}

func (r *usageAPIKeyRepoStub) GetByID(_ context.Context, _ int64) (*service.APIKey, error) {
	return r.key, r.err
}

// newUsageHandlerWithAPIKeyStub 构造一个 UsageHandler，其 apiKeyService 从给定的
// APIKey 桩返回固定结果。usageRepo 用于注入 UsageService。
func newUsageHandlerWithAPIKeyStub(usageRepo service.UsageLogRepository, apiKeyStub *usageAPIKeyRepoStub) *UsageHandler {
	usageSvc := service.NewUsageService(usageRepo, nil, nil, nil)
	apiKeySvc := service.NewAPIKeyService(apiKeyStub, nil, nil, nil, nil, nil, &config.Config{})
	return NewUsageHandler(usageSvc, apiKeySvc, nil, nil)
}

// newUsageListStatsRouter 构造一个同时注册 List 和 Stats 路由的路由器。
func newUsageListStatsRouter(handler *UsageHandler, subject middleware2.AuthSubject) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), subject)
		c.Next()
	})
	router.GET("/usage", handler.List)
	router.GET("/usage/stats", handler.Stats)
	return router
}

// ── List: api_key_id 团队所有权检查 ──────────────────────────────────────────

// TestUsageListAPIKeyOwnKeyTeamMember – 团队成员查询自己的 key → 不应 403（通过所有权门）
func TestUsageListAPIKeyOwnKeyTeamMember(t *testing.T) {
	apiKeyStub := &usageAPIKeyRepoStub{key: &service.APIKey{
		ID:               10,
		UserID:           42,
		BillingSubjectID: 100,
	}}
	subject := middleware2.AuthSubject{
		UserID: 42, BillingSubjectID: 100,
		SubjectType: domain.BillingSubjectTypeTeam, TeamID: 7,
		Permissions: map[string]bool{domain.TeamPermissionViewUsage: true},
	}
	h := newUsageHandlerWithAPIKeyStub(&userUsageRepoCapture{}, apiKeyStub)
	router := newUsageListStatsRouter(h, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/usage?api_key_id=%d", apiKeyStub.key.ID), nil))
	require.NotEqual(t, http.StatusForbidden, rec.Code, "团队成员查询自己的 key 不应被 403")
}

// TestUsageListAPIKeyOtherMemberKeyForbidden – 团队成员查询他人 key → 403
func TestUsageListAPIKeyOtherMemberKeyForbidden(t *testing.T) {
	apiKeyStub := &usageAPIKeyRepoStub{key: &service.APIKey{
		ID:               10,
		UserID:           99, // 他人 key
		BillingSubjectID: 100,
	}}
	subject := middleware2.AuthSubject{
		UserID: 42, BillingSubjectID: 100,
		SubjectType: domain.BillingSubjectTypeTeam, TeamID: 7,
		Permissions: map[string]bool{domain.TeamPermissionViewUsage: true},
	}
	h := newUsageHandlerWithAPIKeyStub(&userUsageRepoCapture{}, apiKeyStub)
	router := newUsageListStatsRouter(h, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/usage?api_key_id=%d", apiKeyStub.key.ID), nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestUsageListAPIKeyOwnerViewAllPasses – owner 有 view.all，查询他人 key 同团队 → 不应 403
func TestUsageListAPIKeyOwnerViewAllPasses(t *testing.T) {
	apiKeyStub := &usageAPIKeyRepoStub{key: &service.APIKey{
		ID:               10,
		UserID:           99, // 他人的 key
		BillingSubjectID: 100,
	}}
	subject := middleware2.AuthSubject{
		UserID: 1, BillingSubjectID: 100,
		SubjectType: domain.BillingSubjectTypeTeam, TeamID: 7,
		Permissions: map[string]bool{domain.TeamPermissionViewUsageAll: true},
	}
	h := newUsageHandlerWithAPIKeyStub(&userUsageRepoCapture{}, apiKeyStub)
	router := newUsageListStatsRouter(h, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/usage?api_key_id=%d", apiKeyStub.key.ID), nil))
	require.NotEqual(t, http.StatusForbidden, rec.Code, "owner/admin 有 view.all 查询同团队他人 key 不应 403")
}

// TestUsageListAPIKeyDifferentSubjectForbidden – key 属于不同计费主体 → 403
func TestUsageListAPIKeyDifferentSubjectForbidden(t *testing.T) {
	apiKeyStub := &usageAPIKeyRepoStub{key: &service.APIKey{
		ID:               10,
		UserID:           1, // 即使是 owner 本人
		BillingSubjectID: 999, // 不同主体
	}}
	subject := middleware2.AuthSubject{
		UserID: 1, BillingSubjectID: 100,
		SubjectType: domain.BillingSubjectTypeTeam, TeamID: 7,
		Permissions: map[string]bool{domain.TeamPermissionViewUsageAll: true},
	}
	h := newUsageHandlerWithAPIKeyStub(&userUsageRepoCapture{}, apiKeyStub)
	router := newUsageListStatsRouter(h, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/usage?api_key_id=%d", apiKeyStub.key.ID), nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// ── Stats: api_key_id 团队所有权检查 ─────────────────────────────────────────

// TestUsageStatsAPIKeyOwnKeyTeamMember – 团队成员查询自己的 key → 不应 403
func TestUsageStatsAPIKeyOwnKeyTeamMember(t *testing.T) {
	apiKeyStub := &usageAPIKeyRepoStub{key: &service.APIKey{
		ID:               10,
		UserID:           42,
		BillingSubjectID: 100,
	}}
	subject := middleware2.AuthSubject{
		UserID: 42, BillingSubjectID: 100,
		SubjectType: domain.BillingSubjectTypeTeam, TeamID: 7,
		Permissions: map[string]bool{domain.TeamPermissionViewUsage: true},
	}
	h := newUsageHandlerWithAPIKeyStub(&userUsageRepoCapture{}, apiKeyStub)
	router := newUsageListStatsRouter(h, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/usage/stats?api_key_id=%d", apiKeyStub.key.ID), nil))
	require.NotEqual(t, http.StatusForbidden, rec.Code, "团队成员查询自己的 key stats 不应被 403")
}

// TestUsageStatsAPIKeyOtherMemberKeyForbidden – 团队成员查询他人 key stats → 403
func TestUsageStatsAPIKeyOtherMemberKeyForbidden(t *testing.T) {
	apiKeyStub := &usageAPIKeyRepoStub{key: &service.APIKey{
		ID:               10,
		UserID:           99,
		BillingSubjectID: 100,
	}}
	subject := middleware2.AuthSubject{
		UserID: 42, BillingSubjectID: 100,
		SubjectType: domain.BillingSubjectTypeTeam, TeamID: 7,
		Permissions: map[string]bool{domain.TeamPermissionViewUsage: true},
	}
	h := newUsageHandlerWithAPIKeyStub(&userUsageRepoCapture{}, apiKeyStub)
	router := newUsageListStatsRouter(h, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/usage/stats?api_key_id=%d", apiKeyStub.key.ID), nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestUsageStatsAPIKeyOwnerViewAllPasses – owner 有 view.all，查询他人 key stats → 不应 403
func TestUsageStatsAPIKeyOwnerViewAllPasses(t *testing.T) {
	apiKeyStub := &usageAPIKeyRepoStub{key: &service.APIKey{
		ID:               10,
		UserID:           99,
		BillingSubjectID: 100,
	}}
	subject := middleware2.AuthSubject{
		UserID: 1, BillingSubjectID: 100,
		SubjectType: domain.BillingSubjectTypeTeam, TeamID: 7,
		Permissions: map[string]bool{domain.TeamPermissionViewUsageAll: true},
	}
	h := newUsageHandlerWithAPIKeyStub(&userUsageRepoCapture{}, apiKeyStub)
	router := newUsageListStatsRouter(h, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/usage/stats?api_key_id=%d", apiKeyStub.key.ID), nil))
	require.NotEqual(t, http.StatusForbidden, rec.Code, "owner/admin 有 view.all 查询同团队他人 key stats 不应 403")
}

// TestUsageStatsAPIKeyDifferentSubjectForbidden – key 属于不同计费主体 → 403
func TestUsageStatsAPIKeyDifferentSubjectForbidden(t *testing.T) {
	apiKeyStub := &usageAPIKeyRepoStub{key: &service.APIKey{
		ID:               10,
		UserID:           1,
		BillingSubjectID: 999,
	}}
	subject := middleware2.AuthSubject{
		UserID: 1, BillingSubjectID: 100,
		SubjectType: domain.BillingSubjectTypeTeam, TeamID: 7,
		Permissions: map[string]bool{domain.TeamPermissionViewUsageAll: true},
	}
	h := newUsageHandlerWithAPIKeyStub(&userUsageRepoCapture{}, apiKeyStub)
	router := newUsageListStatsRouter(h, subject)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/usage/stats?api_key_id=%d", apiKeyStub.key.ID), nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
}
