package routes

import (
	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
)

// RegisterConsultRoutes mounts the TEE control-plane endpoints under /api/control,
// guarded by the control-plane bearer token. The /api/ prefix is required so the
// embedded-frontend middleware (internal/web) treats these as API paths and does
// NOT intercept them with the SPA index.html (it only passes through /api/, /v1/,
// etc.). The TEE executor must set CONTROL_URL=<base>/api/control so its
// CONTROL_URL + "/consult/pre" / "/consult/post" / "/models" resolve here.
func RegisterConsultRoutes(r *gin.Engine, h *handler.Handlers, cfg *config.Config) {
	g := r.Group("/api/control")
	g.Use(middleware.ControlTokenAuth(cfg))
	g.POST("/consult/pre", h.Consult.Pre)
	g.POST("/consult/post", h.Consult.Post)
	g.GET("/models", h.Consult.Models)
}
