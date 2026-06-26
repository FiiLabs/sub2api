package routes

import (
	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
)

// RegisterConsultRoutes mounts the TEE control-plane endpoints at the root path
// (the executor calls CONTROL_URL + /consult/pre etc., no /api/v1 prefix),
// guarded by the control-plane bearer token.
func RegisterConsultRoutes(r *gin.Engine, h *handler.Handlers, cfg *config.Config) {
	g := r.Group("/")
	g.Use(middleware.ControlTokenAuth(cfg))
	g.POST("/consult/pre", h.Consult.Pre)
	g.POST("/consult/post", h.Consult.Post)
	g.GET("/models", h.Consult.Models)
}
