// APEXONE-EXT: 双边市场——拒付通知。
//
// 拒付是这条链路上唯一一件**没有任何界面会主动提醒**的事：它不是用户提的工单，
// 不是运营点的按钮，也不会让任何一个列表页多出一行标红。它只是某天 Stripe 推来
// 一条 webhook，然后平台账上少一笔钱。
//
// 而拒付恰恰是最需要人立刻知道的一类事件，理由有三条，每条都有时限：
//
//  1. **应诉窗口只有几天**。证据要上传到 Stripe 后台，过期不候。系统这边
//     做不了这件事，只能把人叫过去。
//  2. **同一个人反复拒付是最强的欺诈信号**。第二封信到达时，收信人应当去看
//     这个 user_id 的前科——那是熔断该不该拉闸的判断依据。
//  3. **uncovered_basis > 0 是冻结窗配短了的现场证据**。它只在这一刻能被观察到，
//     事后翻库要自己 JOIN 一遍冻结区。
//
// # 为什么收件人复用 supply_withdrawal_settings.notify_emails
//
// 本仓的配置doctrine 是「一个 key 对应一个变更理由」。而「运营的收件人变了」
// 与提现通知那个列表**是同一个变更理由**——真到了要改的那天，两处一定一起改。
// 为它单起第七个 settings key，等于制造一个永远与另一个 key 同步变动的 key，
// 那正是这套 doctrine 要避免的东西。
//
// 全程 best-effort、goroutine 内发送、不用请求 ctx，三条约束与
// supplier_withdrawal_notify.go 逐字相同，理由也相同。
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

// PaymentDisputeNotice 是一封拒付通知需要的全部事实。
//
// 刻意做成一个扁平的值类型而不是传 *dbent.PaymentOrder：通知是在 goroutine 里
// 异步发的，传指针意味着发信那一刻读到的可能已经不是触发时那个状态。
type PaymentDisputeNotice struct {
	DisputeID     string
	ProviderKey   string
	Status        string
	Reason        string
	Currency      string
	DisputeAmount float64
	// OrderID 为 0 表示这条争议没对上任何订单——那正是最需要人看的一类。
	OrderID    int64
	OutTradeNo string
	UserID     int64
	UserEmail  string
	OrderType  string
	// BasisAmount 换算到 users.balance 口径后的争议基数。
	BasisAmount float64
	// Settlement 非空表示副作用刚刚执行过，四个金额是这次执行的结果。
	Settlement *PaymentDisputeSettlement
}

// PaymentDisputeNotifierPort 是 HandleDisputeNotification 看得到的唯一出口。
// 用接口是为了让单测塞一个假的进来数信。
type PaymentDisputeNotifierPort interface {
	NotifyDispute(notice PaymentDisputeNotice)
}

type paymentDisputeNotifierBox struct {
	notifier PaymentDisputeNotifierPort
}

var paymentDisputeNotifierHolder atomic.Value

// SetPaymentDisputeNotifier 装配拒付通知出口。
func SetPaymentDisputeNotifier(notifier PaymentDisputeNotifierPort) {
	paymentDisputeNotifierHolder.Store(paymentDisputeNotifierBox{notifier: notifier})
}

func loadPaymentDisputeNotifier() PaymentDisputeNotifierPort {
	box, ok := paymentDisputeNotifierHolder.Load().(paymentDisputeNotifierBox)
	if !ok {
		return nil
	}
	return box.notifier
}

// PaymentDisputeNotifier 是 PaymentDisputeNotifierPort 的邮件实现。
//
// 形态照抄 SupplierWithdrawalNotifier：公开方法只起 goroutine，同步实现是私有的、
// 可直接测的。
type PaymentDisputeNotifier struct {
	email    supplierWithdrawalEmailSender
	settings supplierWithdrawalNotifySettings
}

func NewPaymentDisputeNotifier(email supplierWithdrawalEmailSender, settings supplierWithdrawalNotifySettings) *PaymentDisputeNotifier {
	return &PaymentDisputeNotifier{email: email, settings: settings}
}

// NotifyDispute 异步发出一封拒付通知。
func (n *PaymentDisputeNotifier) NotifyDispute(notice PaymentDisputeNotice) {
	if n == nil || n.email == nil || n.settings == nil {
		return
	}
	go n.notifyDispute(notice)
}

func (n *PaymentDisputeNotifier) notifyDispute(notice PaymentDisputeNotice) {
	ctx, cancel := context.WithTimeout(context.Background(), supplierWithdrawalNotifyTimeout)
	defer cancel()

	settings := n.settings.GetSupplyWithdrawalSettings(ctx)
	if settings == nil || len(settings.NotifyEmails) == 0 {
		// 与提现同款的坏状态：事情发生了，没有人被告知。日志是它唯一的痕迹。
		slog.Warn("[PaymentDisputeNotifier] 拒付无人收到通知：supply_withdrawal_settings.notify_emails 为空",
			"dispute_id", notice.DisputeID, "status", notice.Status,
			"order_id", notice.OrderID, "amount", notice.DisputeAmount)
		return
	}

	siteName := strings.TrimSpace(n.settings.GetSiteName(ctx))
	if siteName == "" {
		siteName = defaultSiteName
	}
	subject := disputeEmailSubject(siteName, notice)
	body := buildPaymentDisputeEmail(siteName, notice)
	for _, to := range settings.NotifyEmails {
		if err := n.email.SendEmail(ctx, to, subject, body); err != nil {
			slog.Error("[PaymentDisputeNotifier] 通知邮件发送失败",
				"to", to, "dispute_id", notice.DisputeID, "error", err)
			continue
		}
		slog.Info("[PaymentDisputeNotifier] 通知邮件已发送", "to", to, "dispute_id", notice.DisputeID)
	}
}

// disputeEmailSubject 三个状态三种标题。
//
// 标题里带订单号与状态，是为了让收件箱那一列在不打开邮件的情况下就能排序和检索——
// 反复拒付的同一个人会在标题里露出同一个 user_id。
func disputeEmailSubject(siteName string, notice PaymentDisputeNotice) string {
	switch notice.Status {
	case payment.DisputeStatusWon:
		return fmt.Sprintf("[%s] 拒付申诉已胜诉 / Dispute won — %s", siteName, notice.DisputeID)
	case payment.DisputeStatusLost:
		return fmt.Sprintf("[%s] 拒付申诉已败诉 / Dispute lost — %s", siteName, notice.DisputeID)
	default:
		return fmt.Sprintf("[%s] 收到拒付 / Chargeback opened — %s", siteName, notice.DisputeID)
	}
}

// buildPaymentDisputeEmail 复用提现那套 HTML 骨架。
//
// 复用一个名字里带 Withdrawal 的构造器确实别扭，但另抄一份 40 行的模板更糟：
// 两份模板会各自漂移，而它们本该长得一模一样。真要改名，是把那个骨架单独提出来，
// 那是一次与本功能无关的重构。
func buildPaymentDisputeEmail(siteName string, notice PaymentDisputeNotice) string {
	rows := withdrawalEmailRow("争议单号 / Dispute", notice.DisputeID) +
		withdrawalEmailRow("状态 / Status", disputeStatusLabel(notice.Status)) +
		withdrawalEmailRow("通道 / Provider", notice.ProviderKey) +
		withdrawalEmailRow("争议金额 / Disputed", fmt.Sprintf("%.2f %s", notice.DisputeAmount, notice.Currency))
	if notice.Reason != "" {
		rows += withdrawalEmailRow("原因 / Reason", notice.Reason)
	}
	if notice.OrderID > 0 {
		rows += withdrawalEmailRow("订单 / Order", fmt.Sprintf("#%d (%s)", notice.OrderID, notice.OrderType))
		rows += withdrawalEmailRow("消费者 / Consumer", disputeConsumerLabel(notice))
		rows += withdrawalEmailRow("折算基数 / Basis", fmt.Sprintf("%.2f", notice.BasisAmount))
	} else if notice.OutTradeNo != "" {
		rows += withdrawalEmailRow("商户单号 / Out trade no", notice.OutTradeNo)
	}
	if s := notice.Settlement; s != nil {
		rows += withdrawalEmailRow("已扣回余额 / Balance reclaimed", fmt.Sprintf("%.2f", s.BalanceDeducted))
		rows += withdrawalEmailRow("已追回分成 / Credit clawed back", fmt.Sprintf("%.2f", s.ClawedCredit))
		rows += withdrawalEmailRow("未覆盖基数 / Uncovered", fmt.Sprintf("%.2f", s.UncoveredBasis))
	}

	return buildSupplierWithdrawalEmail("#ef4444", "#dc2626", siteName,
		disputeEmailHeadline(notice), notice.DisputeID, rows, disputeEmailNotice(notice))
}

func disputeStatusLabel(status string) string {
	switch status {
	case payment.DisputeStatusWon:
		return "已胜诉 / won"
	case payment.DisputeStatusLost:
		return "已败诉 / lost"
	default:
		return "进行中，款项已被扣走 / open (funds withdrawn)"
	}
}

func disputeConsumerLabel(notice PaymentDisputeNotice) string {
	if notice.UserEmail != "" {
		return fmt.Sprintf("#%d %s", notice.UserID, notice.UserEmail)
	}
	return fmt.Sprintf("#%d", notice.UserID)
}

func disputeEmailHeadline(notice PaymentDisputeNotice) string {
	if notice.OrderID <= 0 {
		return "收到一条对不上订单的拒付"
	}
	switch notice.Status {
	case payment.DisputeStatusWon:
		return "拒付申诉胜诉，款项已退回"
	case payment.DisputeStatusLost:
		return "拒付申诉败诉，款项确定不再回来"
	default:
		return "收到拒付，款项已被通道扣走"
	}
}

// disputeEmailNotice 是每封信里最要紧的那段——**收信人现在该做什么**。
func disputeEmailNotice(notice PaymentDisputeNotice) string {
	if notice.OrderID <= 0 {
		return `<p>这条争议在本环境找不到对应订单。常见原因是多套环境共用同一个支付账户，
            争议属于另一套部署；也可能是订单被清理过。</p>
            <p>请到支付服务商后台按争议单号确认它属于谁。在确认之前，本系统没有对任何余额或分成做过改动。</p>`
	}
	switch notice.Status {
	case payment.DisputeStatusWon:
		return `<p>款项已由通道退回。<strong>系统不会自动回补</strong>已扣回的消费者余额与已追回的供给者分成——
            回补分成是一条会重复入账的写路径，风险高于人工处理。</p>
            <p>请按上面的两个数字在后台手工回补，或确认无需回补。</p>`
	case payment.DisputeStatusLost:
		return `<p>这笔款项确定不再回来。若「未覆盖基数」大于 0，说明拒付发生时对应的供给分成已经解冻，
            那部分由平台承担——它同时是冻结窗（freeze_hours）配短了的直接证据。</p>
            <p>请核对该消费者的历史拒付次数，必要时处置账号。</p>`
	default:
		return `<p><strong>应诉有时限</strong>，证据需要在支付服务商后台上传，本系统代劳不了。请尽快过去处理。</p>
            <p>系统已经做的两件事：从消费者余额扣回争议基数、从供给者冻结区追回对应分成。两者都可能扣不满——
            扣不满的部分意味着钱已经被花掉或已经解冻，由平台承担。</p>
            <p>请一并核对该消费者的历史拒付次数：同一个人反复拒付是最强的欺诈信号。</p>`
	}
}
