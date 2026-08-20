// APEXONE-EXT: 双边市场——支付争议（拒付 / chargeback）的通道抽象。
//
// # 为什么需要它
//
// 本仓原有的 Provider 接口只认两件事：付成功了、付失败了。**拒付是第三件事**，
// 而且是唯一一件「钱已经到过账、随后又被拿走」的事——持卡人绕过我们直接找发卡行
// 撤销这笔交易，我们既没发起退款，也不会收到任何一个 payment_intent 事件。
//
// 在这个文件出现之前，`charge.dispute.created` 走到 Stripe provider 的
// VerifyNotification 里会命中 `return nil, nil`（不认识的事件类型），
// webhook 回 200，然后**什么都不发生**：
//
//   - 消费者账上那笔余额还在，他可以继续花；
//   - 供给者冻结区里那笔分成照常解冻，我们照常付给他；
//   - 平台自吃全部亏损，且没有任何一条日志说这件事发生过。
//
// 也就是说，「冻结窗 ≥ 拒付窗」这条不变量的**运营那一半**是空转的：冻结窗配得再准，
// 没有人在拒付发生时去冻结区把钱拿回来，供给者拿到的仍旧是消费者没付的钱。
//
// # 为什么是一个独立的可选接口，而不是给 PaymentNotification 加一个状态值
//
// PaymentNotification 的 Status 只有 success / failed 两个值，它一路喂进
// HandlePaymentNotification —— 那条路径的语义是「按订单金额交付或不交付」，
// 全程假设自己在处理一次**正向**支付。往里塞第三个值，等于让每一个
// `switch status` 的地方都多一条必须正确处理的分支，而它们全部位于 core
// 的合并热区。争议事件的形状也确实不同：它有自己的 id、自己的生命周期
// （open → won / lost）、以及一个与订单金额未必相等的争议金额。
//
// 于是走本仓已有的可选接口套路（CancelableProvider / RefundQueryProvider 同款）：
// 不实现的通道一切照旧，实现了的通道多一条并行的解析路径。
package payment

import "context"

// 争议在我们这边只有三个状态。刻意不照搬 Stripe 那十来个
// （warning_needs_response / needs_response / under_review / charge_refunded ...）：
// 那些区别决定的是「运营现在该不该上传证据」，而那件事发生在 Stripe 后台，
// 不发生在这里。我们只需要回答一个问题——**钱现在在谁手里**。
const (
	// DisputeStatusOpen 争议已发起。Stripe 在创建争议的同时就把款项从我们账上扣走，
	// 所以这个状态的含义是「钱已经不在我们手里」，不是「有人提了个申诉」。
	// 追回与扣回都挂在这个状态上，不等到 closed —— 等 60 天等来的是一个
	// 早已解冻、早已提现的冻结区。
	DisputeStatusOpen = "open"
	// DisputeStatusWon 我们赢了，款项被 Stripe 退回。
	DisputeStatusWon = "won"
	// DisputeStatusLost 我们输了（或没应诉），款项确定不再回来。
	DisputeStatusLost = "lost"
)

// DisputeNotification 是一个已验签的争议事件。
//
// 金额刻意分成两个字段：DisputeAmount 是**通道币种**的争议金额（Stripe 传来的原值），
// 而追回与扣回需要的是 users.balance 口径的金额。两者之间的换算需要订单信息
// （PayAmount ↔ Amount 的比例），只有 service 层拿得到——所以这里只搬运，不换算。
type DisputeNotification struct {
	// DisputeID 通道侧的争议 id（Stripe 的 `dp_...`）。它是幂等键：
	// 同一个争议会在 created / updated / closed 时各推一次，我们按它去重。
	DisputeID string
	// TradeNo 被争议的支付意图 id，对应 payment_orders.payment_trade_no。
	TradeNo string
	// OrderID 我们的商户订单号（取自 PaymentIntent metadata），可能为空——
	// Stripe 的 dispute 对象本身不带它，需要额外拉一次 PaymentIntent 才有。
	// 为空时由 service 侧退回按 TradeNo 查订单。
	OrderID string
	// DisputeAmount 争议金额，通道币种的主单位（不是分）。
	DisputeAmount float64
	// Currency 争议金额的币种。
	Currency string
	// Status 见上面三个常量。
	Status string
	// Reason 通道给出的争议原因（`fraudulent` / `product_not_received` / ...）。
	// 只用于记录和通知运营，不参与任何判定——判定依据只有 Status。
	Reason string
	// RawData 原始报文，留证用。
	RawData string
}

// DisputeAwareProvider 是支持争议事件的通道。
//
// 未实现的通道（支付宝、微信、易支付……）不受影响：那几条通道的拒付走的是
// 平台方后台的人工流程，没有 webhook 可挂，只能由运营手工发起退款——而退款路径
// 上早就有追回那一行了。
type DisputeAwareProvider interface {
	Provider
	// VerifyDisputeNotification 验签并解析一个争议事件。
	//
	// 与 VerifyNotification 同样的契约：**不是争议事件就返回 (nil, nil)**，
	// 调用方据此静默跳过。返回 error 只表示验签失败或报文损坏。
	VerifyDisputeNotification(ctx context.Context, rawBody string, headers map[string]string) (*DisputeNotification, error)
}
