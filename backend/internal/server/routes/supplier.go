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
		// APEXONE-EXT: 中转接入（M7）。Heavy 限流与 OAuth 同档：它会向
		// 供给者填的端点发一次真实探测，被脚本刷等于让平台替人打压测。
		supply.POST("/accounts/relay", panelRateLimiter.Heavy(), h.Supplier.SubmitRelayAccount)

		supply.GET("/wallet", h.Supplier.GetWallet)
		supply.GET("/ledger", h.Supplier.ListLedger)

		// 提现。options 与列表是纯读；申请**当场从可用区扣钱**，所以单独套一层
		// 重限流——它是供给侧唯一一个会动余额的写接口。
		//
		// 撤回刻意**不**套 Heavy，理由与 DELETE /accounts/:id 一字不差：把钱拿回来
		// 这个动作排在限流后面，等于在他最急着要回钱的时候让他要不回来。
		// 它也只改一行状态，不打上游、不消耗任何配额。
		// 链上收款地址绑定。读是纯读；绑定与解绑都改的是「钱最终打到哪」，
		// 因此都走审计中间件（这个组已经带着）——出纠纷时要答得出
		// 「那个地址是什么时候、从哪个会话绑上去的」。
		//
		// 绑定套 Heavy 限流：它是唯一一个会往一张带唯一索引的表里写行的供给侧接口，
		// 不限住的话可以拿它枚举「某个地址有没有被人绑过」。
		// 解绑不套，理由与撤回提现、解绑账号一字不差：把自己的东西拿回来这个动作
		// 排在限流后面，等于在他最想改的时候让他改不了。
		supply.GET("/payout-wallets", h.Supplier.GetPayoutWalletOptions)
		supply.PUT("/payout-wallets/:network", panelRateLimiter.Heavy(), h.Supplier.BindPayoutWallet)
		supply.DELETE("/payout-wallets/:network", h.Supplier.UnbindPayoutWallet)

		supply.GET("/withdrawals/options", h.Supplier.GetWithdrawalOptions)
		supply.GET("/withdrawals", h.Supplier.ListWithdrawals)
		supply.POST("/withdrawals", panelRateLimiter.Heavy(), h.Supplier.RequestWithdrawal)
		supply.POST("/withdrawals/:id/cancel", h.Supplier.CancelWithdrawal)

		supply.GET("/accounts", h.Supplier.ListAccounts)
		supply.GET("/accounts/:id", h.Supplier.GetAccount)
		supply.POST("/accounts/:id/pause", h.Supplier.PauseAccount)
		supply.POST("/accounts/:id/resume", h.Supplier.ResumeAccount)
		// 每日共享上限。PUT = 幂等置换（每个号至多一份上限）。
		//
		// 与 pause/resume 一样不套 Heavy 限流：这是供给者把自己的水龙头拧小，
		// 把它排在限流后面，等于在他最想止损的时候让他改不了。它也不打上游、
		// 不消耗任何配额。
		supply.PUT("/accounts/:id/daily-cap", h.Supplier.SetDailyCap)
		// 就地重新授权：凭证失效时把一份新 token 换进**同一个**号，
		// 而不是让供给者走「解绑 → 重新接入」（不可逆、换新 id、观察期重跑、
		// 每日上限丢失）。见 service/supplier_reauth.go。
		//
		// 两条都套 Heavy 限流，与 /oauth/start|complete 同档——它们的代价一模一样：
		// 一条会话行加一次上游授权页跳转，然后是一次真实的 token 交换。
		// 这里与 pause/resume/detach 的区别在于「打不打上游」：那三条不打，这两条打。
		supply.POST("/accounts/:id/reauth/start", panelRateLimiter.Heavy(), h.Supplier.StartReauth)
		supply.POST("/accounts/:id/reauth/complete", panelRateLimiter.Heavy(), h.Supplier.CompleteReauth)
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
//
// 除提现审批外全部是 GET：这一刀不给管理端任何改动供给侧数据的能力
// （见 supplier_admin.go 顶部）。提现的两条 POST 是**唯一**的例外，因为一张
// 已经扣了钱的单子必须有人能推进它——没有这两条，供给者的钱会永远停在 pending。
// 例外的范围被刻意收死：它们只改提现单的状态与退款，碰不到账号、分成或钱包余额
// 之外的任何东西；且都走上面那四层中间件，审批动作全部进审计日志。
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

		supply.GET("/withdrawals", h.Supplier.Admin.ListWithdrawals)
		supply.POST("/withdrawals/:id/paid", h.Supplier.Admin.MarkWithdrawalPaid)
		supply.POST("/withdrawals/:id/reject", h.Supplier.Admin.RejectWithdrawal)

		// 对账导出。是 GET、只读，但与上面四条只读接口有一点本质不同：
		// 响应体是一份**要离开这台机器**的文件，而且提现那份里含收款账号明文。
		// 因此它必须走审计中间件（这个组已经带着）——事后要答得出
		// 「上个月那份带着全部供给者收款方式的表格是谁导的」。
		// 挂在这个组里而不是自起一个组，理由与本函数其余路由一样。
		supply.GET("/export/withdrawals", h.Supplier.Admin.ExportWithdrawals)
		supply.GET("/export/ledger", h.Supplier.Admin.ExportLedger)

		// 供给号失效事件。只读，与上面那几条看板接口同类。
		// summary 在 incidents 之后注册：gin 的路由树里 /incidents/summary 是
		// 一个更长的静态路径，两者不冲突（这里没有 :id 参数段），顺序只是可读性。
		supply.GET("/incidents", h.Supplier.Admin.ListIncidents)
		supply.GET("/incidents/summary", h.Supplier.Admin.GetIncidentSummary)

		// 定价与供给健康度。只读看板，回答「倍率和分成配得对不对」。
		supply.GET("/health", h.Supplier.Admin.GetSupplyMarketHealth)
	}
}
