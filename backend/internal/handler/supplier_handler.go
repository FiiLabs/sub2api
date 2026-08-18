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

// SupplierHandler 处理供给者自助接入请求。
type SupplierHandler struct {
	onboardingService *service.SupplierOnboardingService
}

// NewSupplierHandler 构造供给者接入 handler。
func NewSupplierHandler(onboardingService *service.SupplierOnboardingService) *SupplierHandler {
	return &SupplierHandler{onboardingService: onboardingService}
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

// GetStatus 返回自助接入是否开放。
// GET /api/v1/user/supply/status
//
// 前端用它决定要不要显示「连接我的订阅」入口。刻意不返回分组 id 之类的内部配置——
// 前端需要知道的只是「能不能点」。
func (h *SupplierHandler) GetStatus(c *gin.Context) {
	if _, ok := h.currentUserID(c); !ok {
		return
	}
	if h.onboardingService == nil {
		response.Success(c, gin.H{"enabled": false})
		return
	}
	response.Success(c, gin.H{"enabled": h.onboardingService.IsEnabled(c.Request.Context())})
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

// PauseAccount 下线一个供给账号。
// POST /api/v1/user/supply/accounts/:id/pause
func (h *SupplierHandler) PauseAccount(c *gin.Context) {
	h.mutateAccount(c, func(ctx *gin.Context, userID, accountID int64) error {
		return h.onboardingService.PauseAccount(ctx.Request.Context(), userID, accountID)
	})
}

// ResumeAccount 把已下线的账号重新挂回来（回到观察期，不直接入池）。
// POST /api/v1/user/supply/accounts/:id/resume
func (h *SupplierHandler) ResumeAccount(c *gin.Context) {
	h.mutateAccount(c, func(ctx *gin.Context, userID, accountID int64) error {
		return h.onboardingService.ResumeAccount(ctx.Request.Context(), userID, accountID)
	})
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
