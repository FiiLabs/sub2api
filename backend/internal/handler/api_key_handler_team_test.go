package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyHandlerListUsesActiveTeamSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &apiKeyHandlerTeamRepoStub{
		keys: []service.APIKey{{
			ID:               5,
			UserID:           42,
			BillingSubjectID: 900,
			TeamID:           ptrInt64(77),
			CreatedByUserID:  ptrInt64(42),
			Key:              "sk-team",
			Name:             "team key",
			Status:           service.StatusActive,
		}},
	}
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{})
	h := NewAPIKeyHandler(svc)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{
			UserID:           42,
			BillingSubjectID: 900,
			SubjectType:      domain.BillingSubjectTypeTeam,
			TeamID:           77,
			Permissions:      map[string]bool{domain.TeamPermissionManageKeys: true},
		})
		c.Next()
	})
	router.GET("/api/v1/api-keys", h.List)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/api-keys", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{900}, repo.listSubjectIDs)
	require.Contains(t, rec.Body.String(), `"billing_subject_id":900`)
	require.Contains(t, rec.Body.String(), `"team_id":77`)
	require.Contains(t, rec.Body.String(), `"created_by_user_id":42`)
}

type apiKeyHandlerTeamRepoStub struct {
	service.APIKeyRepository
	keys               []service.APIKey
	listSubjectIDs     []int64
	listCreatorFilters []*int64
}

func (r *apiKeyHandlerTeamRepoStub) ListByBillingSubjectID(_ context.Context, billingSubjectID int64, params pagination.PaginationParams, filters service.APIKeyListFilters) ([]service.APIKey, *pagination.PaginationResult, error) {
	r.listCreatorFilters = append(r.listCreatorFilters, filters.CreatedByUserID)
	r.listSubjectIDs = append(r.listSubjectIDs, billingSubjectID)
	keys := append([]service.APIKey(nil), r.keys...)
	return keys, &pagination.PaginationResult{
		Total:    int64(len(keys)),
		Page:     params.Page,
		PageSize: params.PageSize,
		Pages:    1,
	}, nil
}

func (r *apiKeyHandlerTeamRepoStub) GetByIDForBillingSubjectID(context.Context, int64, int64) (*service.APIKey, error) {
	panic("unexpected GetByIDForBillingSubjectID")
}

func (r *apiKeyHandlerTeamRepoStub) UpdateLastUsed(context.Context, int64, time.Time) error {
	panic("unexpected UpdateLastUsed")
}

func TestAPIKeyHandlerListScopesByCreatorForMember(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &apiKeyHandlerTeamRepoStub{keys: []service.APIKey{}}
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{})
	h := NewAPIKeyHandler(svc)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{
			UserID: 42, BillingSubjectID: 900, SubjectType: domain.BillingSubjectTypeTeam, TeamID: 77,
			Permissions: map[string]bool{domain.TeamPermissionManageKeys: true},
		})
		c.Next()
	})
	router.GET("/api/v1/api-keys", h.List)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/api-keys", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, repo.listCreatorFilters, 1)
	require.NotNil(t, repo.listCreatorFilters[0])
	require.Equal(t, int64(42), *repo.listCreatorFilters[0])
}

func TestAPIKeyHandlerListNoCreatorFilterForOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &apiKeyHandlerTeamRepoStub{keys: []service.APIKey{}}
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{})
	h := NewAPIKeyHandler(svc)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{
			UserID: 1, BillingSubjectID: 900, SubjectType: domain.BillingSubjectTypeTeam, TeamID: 77,
			Permissions: map[string]bool{domain.TeamPermissionManageKeys: true, domain.TeamPermissionManageKeysAll: true},
		})
		c.Next()
	})
	router.GET("/api/v1/api-keys", h.List)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/api-keys", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, repo.listCreatorFilters, 1)
	require.Nil(t, repo.listCreatorFilters[0])
}

func ptrInt64(v int64) *int64 {
	return &v
}
