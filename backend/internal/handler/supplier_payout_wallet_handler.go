// APEXONE-EXT: 双边市场——链上收款地址绑定的 HTTP 层。
//
// 只有供给侧，没有管理侧：管理员**不该**有改别人收款地址的接口。
// 一个能替别人改收款地址的按钮，与一个能替别人提现的按钮是同一件东西。
// 运营需要看某人绑了什么时，走提现单上的 payout_account 快照（那是审计过的路径）。
//
// 地址一律以小写形态返回。前端要显示 EIP-55 混合大小写自己加，
// 不在这里美化：后端返回什么，前端就该能原样再提交回来，
// 而一个被美化过的地址提交回来会走到"校验和"那道门上。
package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// payoutWalletNetworkParam 取路径上的链标识。
//
// 不认识的链回 400（而不是 404）：链标识是一个封闭的、公开的值集，
// 藏着它没有任何意义，而"你传的这条链我不支持"是调用方能立刻改对的东西。
// 这一点与单号不同——单号回 404 是为了不让它可枚举。
func payoutWalletNetworkParam(c *gin.Context) (string, bool) {
	network := strings.TrimSpace(c.Param("network"))
	if !service.IsSupplierPayoutNetwork(network) {
		response.ErrorFrom(c, service.ErrSupplierPayoutNetworkInvalid)
		return "", false
	}
	return network, true
}

// GetPayoutWalletOptions 读绑定表单需要的一切。
// GET /api/v1/user/supply/payout-wallets
//
// 一次返回「支持哪些链」和「你绑了什么」。没绑过也是 200 + 空数组，不是 404：
// 这个接口的用途是画一个表单，而"还没绑"正是那个表单存在的理由。
func (h *SupplierHandler) GetPayoutWalletOptions(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.payoutWalletService == nil {
		response.ErrorFrom(c, service.ErrSupplierPayoutWalletUnavailable)
		return
	}

	options, err := h.payoutWalletService.GetOptions(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, options)
}

// SupplierPayoutWalletBindBody 是一次绑定/换绑。
//
// 只有地址一个字段：链从路径上取。地址放请求体而不是路径，是因为它会出现在
// 访问日志、代理日志、浏览器历史里——把一个人的链上身份写进那些地方，
// 与把它明文存进数据库是同一类问题（见迁移 234 的文件头）。
type SupplierPayoutWalletBindBody struct {
	Address string `json:"address"`
}

// BindPayoutWallet 绑定或换绑某条链上的收款地址。
// PUT /api/v1/user/supply/payout-wallets/:network
//
// 用 PUT 而不是 POST：每人每链**至多一个**地址，这是一次幂等的置换，
// 不是往一个集合里追加。重复提交同一个地址必须是同一个结果。
func (h *SupplierHandler) BindPayoutWallet(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.payoutWalletService == nil {
		response.ErrorFrom(c, service.ErrSupplierPayoutWalletUnavailable)
		return
	}
	network, ok := payoutWalletNetworkParam(c)
	if !ok {
		return
	}

	var body SupplierPayoutWalletBindBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}

	wallet, err := h.payoutWalletService.Bind(c.Request.Context(), userID, network, body.Address)
	if err != nil {
		// 地址已被别人绑走会走到 409（错误定义在 service 层带的就是 Conflict），
		// 而不是 400：请求本身没有错，是资源冲突。
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, wallet)
}

// UnbindPayoutWallet 解绑某条链上的收款地址。
// DELETE /api/v1/user/supply/payout-wallets/:network
//
// 没绑过时回 404（service 层的 ErrSupplierPayoutWalletNotFound），不静默成功：
// 静默成功会让前端显示"已解绑"，而库里那条绑定还在。
func (h *SupplierHandler) UnbindPayoutWallet(c *gin.Context) {
	userID, ok := h.currentUserID(c)
	if !ok {
		return
	}
	if h.payoutWalletService == nil {
		response.ErrorFrom(c, service.ErrSupplierPayoutWalletUnavailable)
		return
	}
	network, ok := payoutWalletNetworkParam(c)
	if !ok {
		return
	}

	if err := h.payoutWalletService.Unbind(c.Request.Context(), userID, network); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"network": network, "bound": false})
}
