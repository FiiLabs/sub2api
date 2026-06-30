package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterDataPlaneDisabled mounts hard 410 Gone stubs on every data-plane proxy
// path that RegisterGatewayRoutes would otherwise serve. This is intentional and
// UNCONDITIONAL (NOT a runtime flag): in this build sub2api is a pure control
// plane and MUST NOT forward user requests to any upstream third-party account.
// The real gateway proxy handlers remain compiled but have no route pointing at
// them, so they are unreachable dead code. All data-plane traffic must go through
// the TEE gateway (private-ai-gateway).
func RegisterDataPlaneDisabled(r *gin.Engine) {
	gone := func(c *gin.Context) {
		c.JSON(http.StatusGone, gin.H{
			"error": gin.H{
				"code":    "DATA_PLANE_DISABLED",
				"message": "sub2api is control-plane only; route requests through the TEE gateway",
			},
		})
	}
	// Mirror every prefix RegisterGatewayRoutes mounts.
	r.Any("/v1/*path", gone)
	r.Any("/v1beta/*path", gone)
	r.GET("/responses", gone)
	r.POST("/responses", gone)
	r.Any("/responses/*subpath", gone)
	r.Any("/backend-api/codex/*path", gone)
}
