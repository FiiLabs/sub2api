package middleware

import (
	"crypto/subtle"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

// ControlTokenAuth authenticates the TEE gateway executor via a static bearer
// token (config.Consult.ControlToken). Used only on the consult control-plane routes.
func ControlTokenAuth(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		want := strings.TrimSpace(cfg.Consult.ControlToken)
		if want == "" {
			AbortWithError(c, 503, "CONFIG_ERROR", "control token not configured")
			return
		}
		parts := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") ||
			subtle.ConstantTimeCompare([]byte(strings.TrimSpace(parts[1])), []byte(want)) != 1 {
			AbortWithError(c, 401, "INVALID_CONTROL_TOKEN", "invalid control token")
			return
		}
		c.Next()
	}
}
