// APEXONE-EXT: 双边市场——供给者自助接入路由。
//
// 单起一个文件而不是往 routes/user.go 里插一段：那个文件是上游的合并热区，
// 每次同步上游都要在里面手工调解冲突。独立文件加 router.go 里一行注册，
// 把这次扩展在路由层的 core 侵入压到一行。
package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterSupplierRoutes 注册供给者自助接入路由（需要登录）。
//
// 中间件与 RegisterUserRoutes 一字不差：同一批人、同一个面板、同一套限流与审计。
// 接入与下线都是会改变账号归属和调度状态的动作，必须进审计日志。
func RegisterSupplierRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settingService *service.SettingService,
	panelRateLimiter *middleware.PanelRateLimiter,
) {
	if h == nil || h.Supplier == nil {
		return
	}

	authenticated := v1.Group("")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(middleware.BackendModeUserGuard(settingService))
	authenticated.Use(panelRateLimiter.Global())
	authenticated.Use(gin.HandlerFunc(auditLog))

	supply := authenticated.Group("/user/supply")
	{
		supply.GET("/status", h.Supplier.GetStatus)
		// 发起授权会写一行会话并向上游拿一个链接，算重操作：单独套一层重限流，
		// 免得有人用它刷会话表。
		supply.POST("/oauth/start", panelRateLimiter.Heavy(), h.Supplier.StartOAuth)
		supply.POST("/oauth/complete", panelRateLimiter.Heavy(), h.Supplier.CompleteOAuth)

		supply.GET("/wallet", h.Supplier.GetWallet)
		supply.GET("/ledger", h.Supplier.ListLedger)

		supply.GET("/accounts", h.Supplier.ListAccounts)
		supply.GET("/accounts/:id", h.Supplier.GetAccount)
		supply.POST("/accounts/:id/pause", h.Supplier.PauseAccount)
		supply.POST("/accounts/:id/resume", h.Supplier.ResumeAccount)
	}
}
