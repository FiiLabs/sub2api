// APEXONE-EXT: 双边市场——供给者自助接入的 HTTP 层。
//
// 全部挂在已登录用户的路由组下（/api/v1/user/supply/*），没有单独的鉴权：
// 供给者就是普通用户，只是他名下多了几个账号。用户 id 一律从 JWT 里取，
// **绝不**从请求体里读——请求体里的 user_id 等于让任何人指定把账号挂到谁名下。
package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// SupplierHandler 处理供给者自助接入与收益查询请求。
type SupplierHandler struct {
	onboardingService *service.SupplierOnboardingService
	creditService     *service.SupplierCreditService

	// Admin 是管理端运营视图的入口，挂在这里只是为了不动 AdminHandlers 那三处
	// 合并热区（理由见 supplier_admin_handler.go 顶部）。它是一个**独立类型**：
	// 本结构体上没有任何管理端方法，也就不可能把全站流水误挂到 /user/supply 下。
	Admin *SupplierAdminHandler
}

// NewSupplierHandler 构造供给者接入 handler。
func NewSupplierHandler(
	onboardingService *service.SupplierOnboardingService,
	creditService *service.SupplierCreditService,
	adminService *service.SupplierAdminService,
) *SupplierHandler {
	return &SupplierHandler{
		onboardingService: onboardingService,
		creditService:     creditService,
		Admin:             NewSupplierAdminHandler(adminService),
	}
}

// currentUserID 取当前登录用户，取不到就直接把 401 写回去。
//
// 返回 ok=false 时响应已经写完，调用方直接 return 即可。
func (h *SupplierHandler) currentUserID(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	return subject.UserID, true
}

// SupplierStatusResponse 是供给侧的功能开关快照。
//
// 两个开关分开报，因为它们真的可以处于不同状态：接入开着而结算没开，
// 意味着「可以挂号，但现在挂了不算钱」——前端必须能把这句话讲给用户，
// 而不是笼统显示一个「功能可用」。
type SupplierStatusResponse struct {
	// Enabled 自助接入是否开放（供给池分组是否配好）。
	Enabled bool `json:"enabled"`
	// SettlementEnabled 结算是否开启（挂上来的号产生的用量是否入账）。
	SettlementEnabled bool `json:"settlement_enabled"`
}

// GetStatus 返回自助接入是否开放。
// GET /api/v1/user/supply/status
//
// 前端用它决定要不要显示「连接我的订阅」入口。刻意不返回分组 id 之类的内部配置——
// 前端需要知道的只是「能不能点」。
func (h *SupplierHandler) GetStatus(c *gin.Context) {
	if _, ok := h.currentUserID(c); !ok {
		return
	}
	resp := SupplierStatusResponse{}
	if h.onboardingService != nil {
		resp.Enabled = h.onboardingService.IsEnabled(c.Request.Context())
	}
	if h.creditService != nil {
		resp.SettlementEnabled = h.creditService.IsEnabled(c.Request.Context())
	}
	response.Success(c, resp)
}

// GetAgreement 读当前协议与本人的同意状态。
// GET /api/v1/user/supply/agreement
//
// 未登录之外没有任何前置条件——供给池没开、协议没发布，这个接口都照常回答，
// 因为它的作用就是解释「为什么现在不能接入」。
func (h *SupplierHandler) GetAgreement(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.onboardingService == nil {
		response.ErrorFrom(c, service.ErrSupplierOnboardingDisabled)
		return
	}

	view, err := h.onboardingService.GetAgreement(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}

// SupplierAcceptAgreementRequest 是同意协议的请求。
//
// version 是必填的，且必须是他看到的那一版：服务端不会"自动取当前版本"帮他补上。
// 理由见 service 侧 AcceptAgreement 的注释——那样记下来的证据只能证明他点了一下。
type SupplierAcceptAgreementRequest struct {
	Version string `json:"version" binding:"required"`
}

// AcceptAgreement 记一次同意。
// POST /api/v1/user/supply/agreement/accept
//
// IP 与 UA 在这里取（而不是让 service 从 ctx 里挖）：它们是 HTTP 层的事实，
// 让 service 去 gin.Context 里翻，等于把这一层的细节漏进领域逻辑，也让单测得造一个
// 假的请求上下文才能测同意本身。
func (h *SupplierHandler) AcceptAgreement(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.onboardingService == nil {
		response.ErrorFrom(c, service.ErrSupplierOnboardingDisabled)
		return
	}

	var req SupplierAcceptAgreementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}

	view, err := h.onboardingService.AcceptAgreement(c.Request.Context(), userID,
		req.Version, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}

// SupplierStartOAuthResponse 是发起授权的响应。
//
// 只回 auth_url 和 session_id。state 与 code_verifier 留在服务端——前端拿不到，
// 也就没法把它们喂给别的流程。
type SupplierStartOAuthResponse struct {
	AuthURL   string `json:"auth_url"`
	SessionID string `json:"session_id"`
}

// StartOAuth 发起一次授权。
// POST /api/v1/user/supply/oauth/start
func (h *SupplierHandler) StartOAuth(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.onboardingService == nil {
		response.ErrorFrom(c, service.ErrSupplierOnboardingDisabled)
		return
	}

	auth, err := h.onboardingService.StartOAuth(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, SupplierStartOAuthResponse{
		AuthURL:   auth.AuthURL,
		SessionID: auth.SessionID,
	})
}

// SupplierCompleteOAuthRequest 是兑换授权码的请求。
type SupplierCompleteOAuthRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Code      string `json:"code" binding:"required"`
	Name      string `json:"name"`
}

// CompleteOAuth 兑换授权码并建号。
// POST /api/v1/user/supply/oauth/complete
func (h *SupplierHandler) CompleteOAuth(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.onboardingService == nil {
		response.ErrorFrom(c, service.ErrSupplierOnboardingDisabled)
		return
	}

	var req SupplierCompleteOAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}

	view, err := h.onboardingService.CompleteOAuth(c.Request.Context(), &service.CompleteOAuthInput{
		UserID:    userID,
		SessionID: req.SessionID,
		Code:      req.Code,
		Name:      req.Name,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}

// ListAccounts 列出当前供给者名下的账号。
// GET /api/v1/user/supply/accounts
func (h *SupplierHandler) ListAccounts(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.onboardingService == nil {
		response.Success(c, gin.H{"accounts": []any{}})
		return
	}

	views, err := h.onboardingService.ListAccounts(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"accounts": views})
}

// accountIDParam 解析路径上的账号 id。
//
// 解析失败回 404 而不是 400：对调用方来说「这不是一个 id」和「这个 id 不是你的」
// 应当是同一个回答，理由与 ErrSupplierAccountNotFound 合并四种情况一致。
func (h *SupplierHandler) accountIDParam(c *gin.Context) (int64, bool) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.ErrorFrom(c, service.ErrSupplierAccountNotFound)
		return 0, false
	}
	return accountID, true
}

// GetAccount 读单个供给账号。
// GET /api/v1/user/supply/accounts/:id
func (h *SupplierHandler) GetAccount(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.onboardingService == nil {
		response.ErrorFrom(c, service.ErrSupplierAccountNotFound)
		return
	}
	accountID, ok := h.accountIDParam(c)
	if !ok {
		return
	}

	view, err := h.onboardingService.GetAccount(c.Request.Context(), userID, accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}

// PauseAccountRequest 下线请求。
type PauseAccountRequest struct {
	// Mode 下线通道：graceful（默认，可反悔的排空窗）或 immediate（直接终态）。
	Mode string `json:"mode"`
}

// PauseAccount 下线一个供给账号。
// POST /api/v1/user/supply/accounts/:id/pause
//
// body 可以为空——那时走默认的 graceful。绑定失败也不报错、同样走 graceful：
// 这个接口唯一的必要语义是「停止接新单」，因为一个畸形的 body 就把它整个拒掉，
// 会让一个想紧急下线的供给者卡在一个参数问题上。通道选错的代价（多等一个排空窗、
// 期间可以取消）远小于下不了线的代价。
func (h *SupplierHandler) PauseAccount(c *gin.Context) {
	var req PauseAccountRequest
	_ = c.ShouldBindJSON(&req)
	h.mutateAccount(c, func(ctx *gin.Context, userID, accountID int64) error {
		return h.onboardingService.PauseAccount(ctx.Request.Context(), userID, accountID, req.Mode)
	})
}

// ResumeAccount 撤销下线：排空窗内取消则回到原状态，已下线则回到观察期。
// POST /api/v1/user/supply/accounts/:id/resume
func (h *SupplierHandler) ResumeAccount(c *gin.Context) {
	h.mutateAccount(c, func(ctx *gin.Context, userID, accountID int64) error {
		return h.onboardingService.ResumeAccount(ctx.Request.Context(), userID, accountID)
	})
}

// DetachAccount 彻底解绑一个供给账号：停调度、抹掉凭证、摘掉号。
// DELETE /api/v1/user/supply/accounts/:id
//
// 不走 mutateAccount：那个壳做完动作会回读账号视图，而这里账号已经不在了，
// 回读只会把一次成功的解绑变成 404。改回一句确认。
//
// 响应里带 upstream_revoke_hint 而不是只回 success：平台这边的凭证没了，但供给者
// 在上游那边的授权记录还在（Anthropic 没有可调用的撤销端点，见 service 侧注释）。
// 不把这句话说出来，他会以为解绑等于上游也撤销了。
func (h *SupplierHandler) DetachAccount(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.onboardingService == nil {
		response.ErrorFrom(c, service.ErrSupplierAccountNotFound)
		return
	}
	accountID, ok := h.accountIDParam(c)
	if !ok {
		return
	}

	if err := h.onboardingService.DetachAccount(c.Request.Context(), userID, accountID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"detached": true,
		// 前端按这个布尔决定要不要弹「还要去上游撤销」的提示。做成字段而不是让前端
		// 写死，是为了将来上游真的有了撤销端点、后端接上之后，能一次性把提示关掉。
		"upstream_revoke_required": true,
	})
}

// GetWallet 读当前供给者的赚取钱包。
// GET /api/v1/user/supply/wallet
//
// 结算关闭时也照常返回（多半是一个全零的钱包）：关掉开关不该让已经攒下的余额
// 从页面上消失——那看起来像钱丢了。要不要在界面上强调「当前未在计费」由前端
// 按 status.settlement_enabled 决定。
func (h *SupplierHandler) GetWallet(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.creditService == nil {
		response.ErrorFrom(c, service.ErrSupplierOnboardingDisabled)
		return
	}

	wallet, err := h.creditService.GetWallet(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, wallet)
}

// ListLedger 分页读当前供给者的钱包流水。
// GET /api/v1/user/supply/ledger?page=&page_size=&action=
//
// 过滤条件里**没有** user_id：它只能来自 JWT。account_id 之类的过滤先不做——
// 首版仪表盘要的是一条时间线，加过滤器不如加分页有用。
func (h *SupplierHandler) ListLedger(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.creditService == nil {
		response.ErrorFrom(c, service.ErrSupplierOnboardingDisabled)
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	// 越界值交给 service 侧夹回去（那里有唯一的一份上下限），这里只负责别把
	// 解析失败的 0 当成用户的意图。
	entries, total, err := h.creditService.ListLedger(c.Request.Context(), service.SupplierCreditLedgerFilter{
		UserID:   userID,
		Action:   c.Query("action"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if entries == nil {
		entries = []service.SupplierCreditLedgerEntry{}
	}
	response.Paginated(c, entries, total, page, pageSize)
}

// mutateAccount 把「取用户 → 取账号 id → 执行 → 回读最新视图」这套壳收在一处。
//
// 回读而不是回一个 {"success": true}：状态变更的接口回一个空响应，前端就只能靠
// 自己猜新状态或者再发一次请求。这两个操作都会改 supply_state，让响应直接带上它。
func (h *SupplierHandler) mutateAccount(c *gin.Context, mutate func(*gin.Context, int64, int64) error) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.onboardingService == nil {
		response.ErrorFrom(c, service.ErrSupplierAccountNotFound)
		return
	}
	accountID, ok := h.accountIDParam(c)
	if !ok {
		return
	}

	if err := mutate(c, userID, accountID); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	view, err := h.onboardingService.GetAccount(c.Request.Context(), userID, accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}
