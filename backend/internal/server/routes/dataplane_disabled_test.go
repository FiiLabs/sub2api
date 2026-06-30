//go:build unit

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// 验证数据面所有前缀被无条件 410（写死，无需任何配置）。
func TestRegisterDataPlaneDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterDataPlaneDisabled(r)

	cases := []struct{ method, path string }{
		{http.MethodPost, "/v1/messages"},
		{http.MethodPost, "/v1/chat/completions"},
		{http.MethodGet, "/v1/models"},
		{http.MethodPost, "/v1beta/models/gemini:generateContent"},
		{http.MethodPost, "/responses"},
		{http.MethodGet, "/responses"},
		{http.MethodPost, "/responses/foo"},
		{http.MethodPost, "/backend-api/codex/foo"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusGone, w.Code)
			assert.Contains(t, w.Body.String(), "DATA_PLANE_DISABLED")
		})
	}
}
