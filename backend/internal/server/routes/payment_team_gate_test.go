//go:build unit

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRequireTeamBillingManageSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		subject middleware.AuthSubject
		want    int
	}{
		{"personal allowed", middleware.AuthSubject{UserID: 1, SubjectType: domain.BillingSubjectTypeUser}, http.StatusOK},
		{"team with billing manage", middleware.AuthSubject{UserID: 1, SubjectType: domain.BillingSubjectTypeTeam, Permissions: map[string]bool{domain.TeamPermissionManageBilling: true}}, http.StatusOK},
		{"team without billing manage", middleware.AuthSubject{UserID: 1, SubjectType: domain.BillingSubjectTypeTeam, Permissions: map[string]bool{domain.TeamPermissionViewUsage: true}}, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), tt.subject)
				c.Next()
			})
			router.Use(requireTeamBillingManageSubject())
			router.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			router.ServeHTTP(rec, req)
			require.Equal(t, tt.want, rec.Code)
		})
	}
}
