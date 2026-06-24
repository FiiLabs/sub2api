//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// platformQuotaRepoStub 内嵌接口，仅覆写本测试用到的两个方法（其余方法不会被调用）。
type platformQuotaRepoStub struct {
	service.UserPlatformQuotaRepository
	subjectCalls []int64
	userCalls    []int64
}

func (s *platformQuotaRepoStub) ListBySubject(_ context.Context, subjectID int64) ([]service.UserPlatformQuotaRecord, error) {
	s.subjectCalls = append(s.subjectCalls, subjectID)
	return nil, nil
}

func (s *platformQuotaRepoStub) ListByUser(_ context.Context, userID int64) ([]service.UserPlatformQuotaRecord, error) {
	s.userCalls = append(s.userCalls, userID)
	return nil, nil
}

func newPlatformQuotaHandler(scoped bool, repo service.UserPlatformQuotaRepository) *UserHandler {
	cfg := &config.Config{}
	cfg.Billing.QuotaSubjectScoped = scoped
	us := service.NewUserService(nil, nil, nil, nil, nil, cfg)
	return NewUserHandler(us, nil, nil, nil, nil, repo)
}

func doGetQuotas(t *testing.T, h *UserHandler, subject middleware2.AuthSubject) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), subject)
		c.Next()
	})
	router.GET("/user/platform-quotas", h.GetMyPlatformQuotas)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/user/platform-quotas", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestGetMyPlatformQuotas_ScopedTeamUsesListBySubject(t *testing.T) {
	repo := &platformQuotaRepoStub{}
	h := newPlatformQuotaHandler(true, repo)
	doGetQuotas(t, h, middleware2.AuthSubject{UserID: 9, BillingSubjectID: 900, SubjectType: domain.BillingSubjectTypeTeam})
	require.Equal(t, []int64{900}, repo.subjectCalls)
	require.Empty(t, repo.userCalls)
}

func TestGetMyPlatformQuotas_FlagOffUsesListByUser(t *testing.T) {
	repo := &platformQuotaRepoStub{}
	h := newPlatformQuotaHandler(false, repo)
	doGetQuotas(t, h, middleware2.AuthSubject{UserID: 9, BillingSubjectID: 900, SubjectType: domain.BillingSubjectTypeTeam})
	require.Equal(t, []int64{9}, repo.userCalls)
	require.Empty(t, repo.subjectCalls)
}
