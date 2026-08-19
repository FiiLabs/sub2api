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
		// 协议在接入之前读、在接入之前签。同意是一次会被当成证据的动作，
		// 因此和其余写接口一样走审计中间件；但不套 Heavy 限流——它只写一行，
		// 而把它限住等于让一个想接入的人卡在同意书上。
		supply.GET("/agreement", h.Supplier.GetAgreement)
		supply.POST("/agreement/accept", h.Supplier.AcceptAgreement)
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
		// 解绑是不可逆的（凭证被抹掉，号被摘掉），但**不**套 Heavy 限流：
		// 撤回自己的授权是供给者最该畅通无阻的一个动作，把它排在限流后面，
		// 等于在他最想退出的时候让他退不出去。它本身也不打上游、不消耗任何配额。
		supply.DELETE("/accounts/:id", h.Supplier.DetachAccount)
	}
}

// registerSupplyMarketRoutes 注册管理端供给侧运营视图（只读）。
//
// 由 routes/admin.go 的管理组调用，一行。定义放在这个文件而不是 admin.go 里，
// 理由与整个文件一样：admin.go 是上游合并热区，那边只留调用的那一行。
//
// 传进来的 admin 组已经带着 adminAuth + 面板限流 + 审计 + AdminComplianceGuard
// 四层中间件——这四层就是这些接口的全部鉴权，handler 里不再查一遍。
// 全部是 GET：这一刀不给管理端任何改动供给侧数据的能力（见 supplier_admin.go 顶部）。
func registerSupplyMarketRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	if h == nil || h.Supplier == nil || h.Supplier.Admin == nil {
		return
	}

	supply := admin.Group("/supply")
	{
		supply.GET("/overview", h.Supplier.Admin.GetOverview)
		supply.GET("/suppliers", h.Supplier.Admin.ListSuppliers)
		supply.GET("/accounts", h.Supplier.Admin.ListAccounts)
		supply.GET("/ledger", h.Supplier.Admin.ListLedger)
	}
}
