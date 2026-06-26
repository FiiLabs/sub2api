package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

func TestControlTokenAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	makeRouter := func(token string) *gin.Engine {
		cfg := &config.Config{}
		cfg.Consult.ControlToken = token

		r := gin.New()
		r.GET("/test", ControlTokenAuth(cfg), func(c *gin.Context) {
			c.Status(200)
		})
		return r
	}

	t.Run("valid bearer token returns 200", func(t *testing.T) {
		r := makeRouter("secret")
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("wrong bearer token returns 401", func(t *testing.T) {
		r := makeRouter("secret")
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("no authorization header returns 401", func(t *testing.T) {
		r := makeRouter("secret")
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("empty control token in config returns 503", func(t *testing.T) {
		r := makeRouter("")
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer anything")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 503 {
			t.Errorf("expected 503, got %d", w.Code)
		}
	})
}
