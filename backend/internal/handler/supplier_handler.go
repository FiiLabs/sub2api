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
	onboardingService   *service.SupplierOnboardingService
	creditService       *service.SupplierCreditService
	withdrawalService   *service.SupplierWithdrawalService
	payoutWalletService *service.SupplierPayoutWalletService

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
	withdrawalService *service.SupplierWithdrawalService,
	exportService *service.SupplierExportService,
	incidentService *service.SupplierIncidentService,
	payoutWalletService *service.SupplierPayoutWalletService,
	healthService *service.SupplyMarketHealthService,
) *SupplierHandler {
	return &SupplierHandler{
		onboardingService:   onboardingService,
		creditService:       creditService,
		withdrawalService:   withdrawalService,
		payoutWalletService: payoutWalletService,
		Admin:               NewSupplierAdminHandler(adminService, withdrawalService, exportService, incidentService, healthService),
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
	// RelayEnabled 「URL + API Key」中转接入开没开（M7）。前端靠它决定
	// 接入卡画不画第二个标签页。
	RelayEnabled bool `json:"relay_enabled"`
	// ShareRatio 当前分成比例（0.8 = 80%）。0 = 读不到，界面据此不显示。
	//
	// 下发而不是让前端写死：它是运营随时可改的设置，而写死的那个数会在
	// 运营改配置的那一刻变成一句谎话——对着一个正在决定要不要挂号的人。
	ShareRatio float64 `json:"share_ratio"`
	// AccountCount 这个人名下还挂着几个供给账号。
	//
	// 前端靠它**自动判定**该进哪种控制台模式（有号 = 共享者，进共享模式），
	// 这样绝大多数人永远不需要手动选一次身份。给的是数量而不是布尔：
	// 「0 个」和「有几个」在供给侧仪表盘上本来就要显示，多一个布尔等于
	// 让同一件事有两个来源。
	AccountCount int `json:"account_count"`
}

// GetStatus 返回自助接入是否开放。
// GET /api/v1/user/supply/status
//
// 前端用它决定要不要显示「连接我的订阅」入口。刻意不返回分组 id 之类的内部配置——
// 前端需要知道的只是「能不能点」。
func (h *SupplierHandler) GetStatus(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	resp := SupplierStatusResponse{}
	if h.onboardingService != nil {
		resp.Enabled = h.onboardingService.IsEnabled(c.Request.Context())
		resp.RelayEnabled = h.onboardingService.RelayEnabled(c.Request.Context())
		resp.AccountCount = h.onboardingService.CountAccounts(c.Request.Context(), userID)
	}
	if h.creditService != nil {
		resp.SettlementEnabled = h.creditService.IsEnabled(c.Request.Context())
		resp.ShareRatio = h.creditService.ShareRatio(c.Request.Context())
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

	// IP 在这一层取，理由同 AcceptAgreement：它是 HTTP 层的事实。
	auth, err := h.onboardingService.StartOAuth(c.Request.Context(), userID, c.ClientIP())
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
		// 刻意不从请求体读：来源 IP 是限额的判据，让客户端自己填等于让它自己
		// 决定算在哪个网络头上——那道闸就不存在了。
		ClientIP: c.ClientIP(),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}

// SupplierStartReauthResponse 是发起重新授权的响应。
//
// 与 SupplierStartOAuthResponse 同形，刻意**不**复用同一个类型：两条路径回答的是
// 不同的问题（「挂一个新号」与「修这一个号」），响应将来很可能分叉——比如带上
// 「必须用哪个邮箱去授权」的提示。共用一个类型时，那种分叉会变成一个
// 「加一个字段，另一条路径顺带也有了」的既成事实。
type SupplierStartReauthResponse struct {
	AuthURL   string `json:"auth_url"`
	SessionID string `json:"session_id"`
}

// StartReauth 为某个已有的供给号发起一次就地重新授权。
// POST /api/v1/user/supply/accounts/:id/reauth/start
func (h *SupplierHandler) StartReauth(c *gin.Context) {
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

	auth, err := h.onboardingService.StartReauth(c.Request.Context(), userID, accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, SupplierStartReauthResponse{
		AuthURL:   auth.AuthURL,
		SessionID: auth.SessionID,
	})
}

// SupplierCompleteReauthRequest 是兑换一次重新授权的请求。
//
// 刻意**没有** Name：重新授权不改名字。改名是另一件事（而且目前没有那个接口），
// 顺手在这里支持它，会让一个「修凭证」的操作悄悄具备改展示名的能力。
type SupplierCompleteReauthRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Code      string `json:"code" binding:"required"`
}

// CompleteReauth 兑换授权码并把新凭证换进那个号。
// POST /api/v1/user/supply/accounts/:id/reauth/complete
//
// 请求体严格绑定（畸形就 400），理由同 SetDailyCap 而不是 PauseAccount：
// 一个畸形的重新授权请求没有安全的默认值可退，而静默忽略它会让供给者对着一个
// 仍然坏着的号看到「成功」——比直接报错糟得多。
func (h *SupplierHandler) CompleteReauth(c *gin.Context) {
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

	var req SupplierCompleteReauthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}

	view, err := h.onboardingService.CompleteReauth(c.Request.Context(), &service.CompleteReauthInput{
		UserID:    userID,
		AccountID: accountID,
		SessionID: req.SessionID,
		Code:      req.Code,
		// 同 CompleteOAuth：来源 IP 是 HTTP 层的事实，不从请求体读。
		// 这条路径上它只进日志（不建号，也就没有按 IP 数号的那道闸）。
		ClientIP: c.ClientIP(),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
}

// SupplierSubmitRelayRequest 是中转接入的请求体（M7）。
type SupplierSubmitRelayRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Name    string `json:"name"`
}

// SubmitRelayAccount 供给者提交一个 Anthropic 兼容中转端点。
// POST /api/v1/user/supply/accounts/relay
//
// 与 CompleteOAuth 同一副骨架：身份从 JWT、来源 IP 从连接，请求体里
// 只有他真正要交的三样东西。API Key 在这条路径上只进内存与 credentials，
// 不进任何日志（探测失败的日志里也只有状态码与响应片段）。
func (h *SupplierHandler) SubmitRelayAccount(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.onboardingService == nil {
		response.ErrorFrom(c, service.ErrSupplierOnboardingDisabled)
		return
	}

	var req SupplierSubmitRelayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}

	view, err := h.onboardingService.SubmitRelay(c.Request.Context(), &service.SupplierRelaySubmission{
		UserID:   userID,
		BaseURL:  req.BaseURL,
		APIKey:   req.APIKey,
		Name:     req.Name,
		ClientIP: c.ClientIP(),
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

// SupplierDailyCapRequest 设置每日共享上限。
//
// 两个字段都是指针，语义是三态：nil = 这一项不改，0 = 取消这一项的上限，
// 正数 = 设成这个值。用值类型的话「把金额上限清成 0」和「我只想改 token 上限」
// 会发出同一个请求体，而这两件事的结果完全相反。
type SupplierDailyCapRequest struct {
	DailyCostLimitUSD *float64 `json:"daily_cost_limit_usd"`
	DailyTokenLimit   *int64   `json:"daily_token_limit"`
}

// SetDailyCap 设置某个供给账号的每日共享上限。
// PUT /api/v1/user/supply/accounts/:id/daily-cap
//
// 用 PUT 而不是 POST：每个号至多一份上限，这是一次幂等置换而非追加
// （与 BindPayoutWallet 同一个理由）。
//
// **刻意不照抄 PauseAccount 的 `_ = c.ShouldBindJSON`。** 那里忽略绑定错误是对的：
// 下线的本质语义是「停」，不该让一个畸形请求体卡住一个想紧急下线的人。这里相反——
// 请求体坏掉时没有任何安全的默认值可取，而「界面显示已保存、实际什么都没设」
// 恰恰是供给者永远不会发现的那种失败。所以绑定失败就报 400。
func (h *SupplierHandler) SetDailyCap(c *gin.Context) {
	var req SupplierDailyCapRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account id")
		return
	}
	if h.onboardingService == nil {
		response.ErrorFrom(c, service.ErrSupplierOnboardingDisabled)
		return
	}
	view, err := h.onboardingService.SetDailyCap(c.Request.Context(), userID, accountID, req.DailyCostLimitUSD, req.DailyTokenLimit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, view)
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
