// APEXONE-EXT: 双边市场——争议（拒付）事件的 webhook 分派。
//
// 挂在 handleNotify 里 `notification == nil` 那个分支上，而不是新开一条路由。
//
// # 为什么不新开路由
//
// Stripe 后台一个 endpoint 收全部事件类型，争议事件与支付事件走的是**同一个 URL**。
// 想让它们分流，得让运营在 Stripe 后台再配一个 endpoint 并勾对事件类型——
// 那是一件不做也不会报错、做错了也不会报错的运维动作，而它错了的表现是
// 「拒付静默地不被处理」，与这个功能上线前的状态一模一样。挂在既有路径上，
// 只要支付 webhook 是通的，争议就一定是通的。
//
// # 为什么恰好挂在 notification == nil 上
//
// 那个分支的含义是「验签通过了，但这不是一个我们认识的支付事件」——
// 争议事件正是从那里掉下去的。挂在这里有一个性质：**支付主链路一行不动**。
// 认得的支付事件根本走不到这里，所以这段代码不可能影响到收款。
//
// # 为什么同步执行
//
// 争议处理会写库、会扣钱，全程通常在几十毫秒内。放 goroutine 里能让 ack 更快，
// 但代价是任何失败都发生在请求结束之后，日志里那条 error 与这次推送对不上号。
// Stripe 的超时是 20 秒，够用得多。
package handler

import (
	"context"
	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

// handleDisputeNotify 在一组通道实例里找出认得争议事件的那个，解析并交给 service。
//
// 全程不返回错误：调用方唯一能做的响应是 5xx，而对 Stripe 来说 5xx 意味着重推，
// 重推会再次走到这里、再次失败，除了日志刷屏什么也改变不了。真正的兜底是
// payment_disputes 表里那一行——它在副作用之前就落库了。
func (h *PaymentWebhookHandler) handleDisputeNotify(ctx context.Context, providers []payment.Provider, rawBody string, headers map[string]string) {
	if h == nil {
		return
	}
	// 刻意**不**在这里判 h.paymentService == nil：HandleDisputeNotification 自己
	// 对 nil 接收者是安全的（它第一行就判）。多一道判断会让「没装配 service」
	// 与「没有通道认这个事件」变成同一种沉默，而前者是配置错误、后者是常态。
	for _, provider := range providers {
		aware, ok := provider.(payment.DisputeAwareProvider)
		if !ok {
			// 绝大多数通道都不实现这个接口（目前只有 Stripe）。这不是异常，
			// 是「这条通道没有争议事件」的正常表达，所以连日志都不打。
			continue
		}
		notification, err := aware.VerifyDisputeNotification(ctx, rawBody, headers)
		if err != nil {
			// 验签失败在这里是**预期内的常事**：一组实例里只有一个持有对的
			// webhookSecret，其余都会在这一步失败。继续试下一个。
			slog.Debug("[Payment Webhook] dispute verify failed",
				"provider", provider.ProviderKey(), "error", err)
			continue
		}
		if notification == nil {
			// 验签过了，但不是争议事件（或是询证类、款项未移动）。
			// 这条通道就是对的那条，不必再试别的。
			return
		}
		h.paymentService.HandleDisputeNotification(ctx, notification, provider.ProviderKey())
		return
	}
}
