// APEXONE-EXT: 双边市场——Stripe 争议（拒付）事件解析。
//
// 与 stripe.go 里的 VerifyNotification 并行的第二条解析路径。单独一个文件而不是
// 往那个 switch 里加 case：那个函数是上游合并的热区，而这一整块（含五种事件类型、
// 八种争议状态、以及「哪几种状态钱其实没动」这个判断）没有任何一行属于上游。
//
// # 一次验签，还是两次
//
// 走到这里时 VerifyNotification 已经对同一份报文做过一次 HMAC 了（它返回
// (nil, nil) 才轮到我们）。这里再算一次，多花的是一个 SHA-256。换来的是
// **两条路径互不知道对方存在**——争议解析不需要 core 那个函数交出中间产物，
// core 那个函数也不需要为我们多返回一个值。上游改动它时，我们这边一行都不用动。
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	stripe "github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
)

// stripeDisputeEventPrefix 匹配 charge.dispute.created / .updated / .closed /
// .funds_withdrawn / .funds_reinstated 全部五种。
//
// 刻意按前缀匹配而不是逐个列出事件名：**五种事件的 data.object 是同一个 Dispute 对象**，
// 而「钱现在在谁手里」这个唯一要回答的问题，答案完全写在 dispute.status 里，
// 与事件名无关。按事件名分派只会让「created 但状态已经是 lost」这类
// 重放 / 补推的顺序问题变成一个需要处理的分支。
const stripeDisputeEventPrefix = "charge.dispute."

// VerifyDisputeNotification 验签并解析一个 Stripe 争议事件。
//
// 非争议事件、或钱没有发生移动的争议事件，一律返回 (nil, nil)。
func (s *Stripe) VerifyDisputeNotification(_ context.Context, rawBody string, headers map[string]string) (*payment.DisputeNotification, error) {
	s.ensureInit()

	webhookSecret := s.config["webhookSecret"]
	if webhookSecret == "" {
		return nil, fmt.Errorf("stripe webhookSecret not configured")
	}
	sig := headers["stripe-signature"]
	if sig == "" {
		return nil, fmt.Errorf("stripe dispute notification missing stripe-signature header")
	}

	event, err := webhook.ConstructEvent([]byte(rawBody), sig, webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("stripe verify dispute notification: %w", err)
	}
	if !strings.HasPrefix(string(event.Type), stripeDisputeEventPrefix) {
		return nil, nil
	}

	var d stripe.Dispute
	if err := json.Unmarshal(event.Data.Raw, &d); err != nil {
		return nil, fmt.Errorf("stripe parse dispute: %w", err)
	}
	if strings.TrimSpace(d.ID) == "" {
		// 没有争议 id 就没有幂等键，而这条路径上的每一个副作用（追回、扣回）
		// 都必须幂等。宁可当作不认识的事件放过去，也不要做一次无法去重的追回。
		return nil, fmt.Errorf("stripe dispute event has no dispute id")
	}

	status, moved := stripeDisputeStatus(d.Status)
	if !moved {
		// 询证（warning_*）与 prevented：Stripe 通知我们有人在问，但**款项一分没动**。
		// 在这里追回等于把还在我们手里的钱从供给者那儿收走一遍。
		return nil, nil
	}

	currency := stripeIntentCurrency(d.Currency, s.currency())
	return &payment.DisputeNotification{
		DisputeID:     d.ID,
		TradeNo:       stripeDisputePaymentIntentID(&d),
		OrderID:       stripeDisputeOrderID(&d),
		DisputeAmount: payment.MinorUnitToAmount(d.Amount, currency),
		Currency:      currency,
		Status:        status,
		Reason:        string(d.Reason),
		RawData:       rawBody,
	}, nil
}

// stripeDisputeStatus 把 Stripe 的八个状态压成我们的三个，
// 第二个返回值是「款项是否已经离开我们的账户」。
//
// 这个布尔是整个文件里最要紧的一处：Stripe 用 `warning_` 前缀区分
// 「询证 / 早期欺诈预警」与「真正的拒付」，前者不扣款、后者立刻扣款。
// 把两者当成一回事，运营看到的现象是「有人问了一句，供给者的钱就没了」。
func stripeDisputeStatus(status stripe.DisputeStatus) (string, bool) {
	switch status {
	case stripe.DisputeStatusNeedsResponse, stripe.DisputeStatusUnderReview:
		return payment.DisputeStatusOpen, true
	case stripe.DisputeStatusWon:
		return payment.DisputeStatusWon, true
	case stripe.DisputeStatusLost:
		return payment.DisputeStatusLost, true
	default:
		// warning_needs_response / warning_under_review / warning_closed / prevented，
		// 以及将来 Stripe 新增的任何状态：默认按「钱没动」处理。
		// 这个方向的错误是漏一次追回（可事后补），反方向是凭空收走供给者的钱。
		return "", false
	}
}

// stripeDisputePaymentIntentID 取被争议的支付意图 id。
//
// Dispute.PaymentIntent 是 Stripe 的 expandable 字段：webhook 报文里它通常是一个
// 裸字符串 id，展开过才是完整对象。stripe-go 的自定义 UnmarshalJSON 两种形态都会
// 把 ID 填上，所以这里只读 .ID 即可，不需要判断它展开没展开。
func stripeDisputePaymentIntentID(d *stripe.Dispute) string {
	if d == nil || d.PaymentIntent == nil {
		return ""
	}
	return strings.TrimSpace(d.PaymentIntent.ID)
}

// stripeDisputeOrderID 尽量从报文里捞出我们的商户订单号。
//
// Dispute 对象自己的 metadata 是空的（我们建 PaymentIntent 时把 orderId 写在
// **PaymentIntent** 的 metadata 上）。只有当 payment_intent 被展开时才读得到，
// 而 webhook 默认不展开——所以这个函数**多数情况下返回空串**，这是预期行为，
// 不是缺陷。service 侧因此以 TradeNo 为主键去查订单，OrderID 只作为兜底。
func stripeDisputeOrderID(d *stripe.Dispute) string {
	if d == nil {
		return ""
	}
	if id := strings.TrimSpace(d.Metadata["orderId"]); id != "" {
		return id
	}
	if d.PaymentIntent != nil {
		return strings.TrimSpace(d.PaymentIntent.Metadata["orderId"])
	}
	return ""
}

// 确保 Stripe 通道满足争议接口。这一行是编译期的守卫：
// 哪天有人把方法签名改了，构建就会红，而不是等到线上第一次拒付才发现没人接。
var _ payment.DisputeAwareProvider = (*Stripe)(nil)
