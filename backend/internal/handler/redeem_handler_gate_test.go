//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRedeemDeniedForTeamMemberWithoutBillingManage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 零值 RedeemService：被拒路径在调用 service 前就返回，故不会触达其 nil 依赖。
	h := NewRedeemHandler(&service.RedeemService{})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{
			UserID:           42,
			BillingSubjectID: 900,
			SubjectType:      domain.BillingSubjectTypeTeam,
			Permissions:      map[string]bool{domain.TeamPermissionViewUsage: true}, // 无 billing.manage
		})
		c.Next()
	})
	router.POST("/redeem", h.Redeem)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/redeem", strings.NewReader(`{"code":"ABC"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}
