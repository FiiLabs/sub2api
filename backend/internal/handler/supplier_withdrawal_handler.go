// APEXONE-EXT: 双边市场——提现的 HTTP 层。
//
// 两侧的方法都在这个文件里，但挂在**两个类型**上：供给侧在 *SupplierHandler，
// 管理侧在 *SupplierAdminHandler。理由与 supplier_admin_handler.go 顶部那段一样——
// 路由挂错组时是编译期的事，而不是把一个能改钱的接口露给普通用户。
//
// 供给侧一律不返回 reviewer_id（见 supplierWithdrawalView）：处理人是谁与钱到不到账
// 无关，暴露它只会把一次结算分歧变成对着某个具体运营的私下追问。
package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// supplierWithdrawalView 是给供给者看的提现单。
//
// 相对领域对象少两个字段：user_id（就是他自己，回给他没有意义）和 reviewer_id。
// 写成一个独立结构体而不是给领域对象加 json:"-"，是因为管理端要看到完整的单子——
// 同一个类型不可能同时满足两种可见性。
type supplierWithdrawalView struct {
	ID            int64   `json:"id"`
	Amount        float64 `json:"amount"`
	Status        string  `json:"status"`
	PayoutChannel string  `json:"payout_channel"`
	PayoutAccount string  `json:"payout_account"`
	UserNote      *string `json:"user_note,omitempty"`

	// LedgerID 申请时那条 withdraw 流水，供给者拿它把单子和账单页对上。
	LedgerID *int64 `json:"ledger_id,omitempty"`

	// ReviewNote 运营的处理意见。**要**给供给者看——被拒时这是他唯一能拿到的解释。
	ReviewNote *string `json:"review_note,omitempty"`
	// ExternalRef 打款凭证。给他看是为了对账，也是纠纷时双方共同的锚点。
	ExternalRef *string `json:"external_ref,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

func toSupplierWithdrawalView(w *service.SupplierWithdrawal) supplierWithdrawalView {
	if w == nil {
		return supplierWithdrawalView{}
	}
	return supplierWithdrawalView{
		ID:            w.ID,
		Amount:        w.Amount,
		Status:        w.Status,
		PayoutChannel: w.PayoutChannel,
		PayoutAccount: w.PayoutAccount,
		UserNote:      w.UserNote,
		LedgerID:      w.LedgerID,
		ReviewNote:    w.ReviewNote,
		ExternalRef:   w.ExternalRef,
		CreatedAt:     w.CreatedAt,
		UpdatedAt:     w.UpdatedAt,
		ResolvedAt:    w.ResolvedAt,
	}
}

// withdrawalIDParam 解析路径上的单号。
//
// 与 accountIDParam 同一个脾气：解析失败回 404 而不是 400。「这不是一个 id」和
// 「这个 id 不是你的」对调用方必须是同一个回答，否则单号可枚举。
func withdrawalIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, service.ErrSupplierWithdrawalNotFound)
		return 0, false
	}
	return id, true
}

// GetWithdrawalOptions 读申请表单需要的一切。
// GET /api/v1/user/supply/withdrawals/options
//
// 提现没开的时候也照常返回（available=false + notice），而不是回一个错误：
// 这个接口的作用就是解释「为什么现在提不了」。
func (h *SupplierHandler) GetWithdrawalOptions(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.withdrawalService == nil {
		response.ErrorFrom(c, service.ErrSupplierWithdrawalDisabled)
		return
	}

	options, err := h.withdrawalService.GetOptions(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, options)
}

// SupplierWithdrawalRequestBody 是一次提现申请。
//
// amount 用 *float64：不带 binding:"required"，因为 required 对数字会把 0 当成
// 「没填」，而 0 是一个应该被明确拒绝（"amount must be positive"）的值，
// 不是一个缺失的字段。
type SupplierWithdrawalRequestBody struct {
	Amount        *float64 `json:"amount"`
	PayoutChannel string   `json:"payout_channel"`
	PayoutAccount string   `json:"payout_account"`
	UserNote      string   `json:"user_note"`
}

// RequestWithdrawal 提交一笔提现申请。
// POST /api/v1/user/supply/withdrawals
//
// 钱在这一刻就从可用区扣走（理由见 service/supplier_withdrawal.go 顶部），
// 所以它是供给侧唯一一个动钱的写接口，路由上单独套了一层重限流。
func (h *SupplierHandler) RequestWithdrawal(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.withdrawalService == nil {
		response.ErrorFrom(c, service.ErrSupplierWithdrawalDisabled)
		return
	}

	var body SupplierWithdrawalRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	amount := 0.0
	if body.Amount != nil {
		amount = *body.Amount
	}

	withdrawal, err := h.withdrawalService.Request(c.Request.Context(), userID, service.SupplierWithdrawalRequest{
		Amount:        amount,
		PayoutChannel: body.PayoutChannel,
		PayoutAccount: body.PayoutAccount,
		UserNote:      body.UserNote,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, toSupplierWithdrawalView(withdrawal))
}

// ListWithdrawals 读自己的提现记录。
// GET /api/v1/user/supply/withdrawals?page=&page_size=
//
// 没有 status 过滤：首版这张表对一个人来说就几行，加过滤器不如把它一次列全有用。
func (h *SupplierHandler) ListWithdrawals(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.withdrawalService == nil {
		response.ErrorFrom(c, service.ErrSupplierWithdrawalDisabled)
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	items, total, err := h.withdrawalService.List(c.Request.Context(), userID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	views := make([]supplierWithdrawalView, 0, len(items))
	for i := range items {
		views = append(views, toSupplierWithdrawalView(&items[i]))
	}
	response.Paginated(c, views, total, page, pageSize)
}

// CancelWithdrawal 撤回自己的未决单，钱退回可用区。
// POST /api/v1/user/supply/withdrawals/:id/cancel
//
// 用 POST 而不是 DELETE：单子不会消失，它变成 canceled 并留在列表里。
// 撤回是一次状态推进，不是一次删除。
func (h *SupplierHandler) CancelWithdrawal(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.withdrawalService == nil {
		response.ErrorFrom(c, service.ErrSupplierWithdrawalDisabled)
		return
	}
	id, ok := withdrawalIDParam(c)
	if !ok {
		return
	}

	withdrawal, err := h.withdrawalService.Cancel(c.Request.Context(), userID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, toSupplierWithdrawalView(withdrawal))
}

// ============================================================================
// 管理侧
// ============================================================================

// reviewerID 取当前操作的管理员 id，取不到返回 nil。
//
// 取不到**不**拒绝请求：审批路径上少记一个处理人，比因为一个上下文问题让一张
// 已经扣了钱的单子卡住要好。真正的鉴权在路由组的 adminAuth 上，这里只是记名。
func reviewerID(c *gin.Context) *int64 {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		return nil
	}
	id := subject.UserID
	return &id
}

// ListWithdrawals 读全站提现单。
// GET /api/v1/admin/supply/withdrawals?status=&user_id=&page=&page_size=
//
// status 传了不认识的值会报 400 而不是当成"不筛"（见 service 侧 AdminList）。
func (h *SupplierAdminHandler) ListWithdrawals(c *gin.Context) {
	if h.withdrawalService == nil {
		response.ErrorFrom(c, service.ErrSupplierWithdrawalDisabled)
		return
	}
	page, pageSize := supplyAdminPaging(c)
	userID, _ := strconv.ParseInt(c.DefaultQuery("user_id", "0"), 10, 64)

	items, total, err := h.withdrawalService.AdminList(c.Request.Context(), service.SupplierWithdrawalFilter{
		UserID:   userID,
		Status:   strings.TrimSpace(c.Query("status")),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if items == nil {
		items = []service.SupplierWithdrawal{}
	}
	response.Paginated(c, items, total, page, pageSize)
}

// SupplierWithdrawalResolveBody 是一次审批。
type SupplierWithdrawalResolveBody struct {
	// ExternalRef 打款凭证/交易号，只在 mark-paid 时有意义。
	ExternalRef string `json:"external_ref"`
	// Note 处理意见。拒绝时必填（service 侧强制），打款时可选。
	Note string `json:"note"`
}

// MarkWithdrawalPaid 标记已打款。**不退款**。
// POST /api/v1/admin/supply/withdrawals/:id/paid
func (h *SupplierAdminHandler) MarkWithdrawalPaid(c *gin.Context) {
	h.resolveWithdrawal(c, func(ctx *gin.Context, id int64, body SupplierWithdrawalResolveBody) (*service.SupplierWithdrawal, error) {
		return h.withdrawalService.MarkPaid(ctx.Request.Context(), id, reviewerID(ctx), body.ExternalRef, body.Note)
	})
}

// RejectWithdrawal 拒绝一张单子，钱退回可用区。
// POST /api/v1/admin/supply/withdrawals/:id/reject
func (h *SupplierAdminHandler) RejectWithdrawal(c *gin.Context) {
	h.resolveWithdrawal(c, func(ctx *gin.Context, id int64, body SupplierWithdrawalResolveBody) (*service.SupplierWithdrawal, error) {
		return h.withdrawalService.Reject(ctx.Request.Context(), id, reviewerID(ctx), body.Note)
	})
}

// resolveWithdrawal 把「查服务在不在 → 取单号 → 读 body → 执行」这套壳收在一处。
//
// body 绑定失败**当成空 body** 而不是报 400：打款时 body 本来就可以是空的
// （不是每种渠道都有交易号）。拒绝时空的 note 会在 service 侧被明确拒掉，
// 那个错误消息比一句 "Invalid request body" 有用得多。
func (h *SupplierAdminHandler) resolveWithdrawal(
	c *gin.Context,
	resolve func(*gin.Context, int64, SupplierWithdrawalResolveBody) (*service.SupplierWithdrawal, error),
) {
	if h.withdrawalService == nil {
		response.ErrorFrom(c, service.ErrSupplierWithdrawalDisabled)
		return
	}
	id, ok := withdrawalIDParam(c)
	if !ok {
		return
	}
	var body SupplierWithdrawalResolveBody
	_ = c.ShouldBindJSON(&body)

	withdrawal, err := resolve(c, id, body)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, withdrawal)
}
