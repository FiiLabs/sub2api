// APEXONE-EXT: 双边市场——提现通知。
//
// 提现是整条供给链路上**唯一一个钱先离开、结果后到**的动作：供给者点下申请的
// 那一刻余额就少了一块。链上-only（M6b）后 worker 自动结算，通知的分工变成：
//
//   - 供给者侧：扣款回执（书面凭证）+ 终态通知（到账/被拒）。
//   - 运营侧：**只在打款失败时**被叫来（NotifyPayoutFailed）——failed 单的钱
//     扣着等人工裁决，没人收到这封信，它就卡在一张没人知道的单子上。
//     「新申请」那封已停发：自动结算下每单一封只会训练财务过滤这个发件人。
//
// 因此这里的三封信不是"体验优化"，它们是这个功能能运转的必要条件。
//
// # 三条实现约束
//
//  1. **全程 best-effort，绝不影响主流程**。发信在 goroutine 里，失败只记
//     slog.Error。一笔已经落库的提现不能因为 SMTP 超时而回滚——钱已经扣了，
//     回滚意味着单子没了但流水还在。
//  2. **不用请求的 ctx**。goroutine 的生命周期比 HTTP 请求长，拿着请求 ctx 去
//     发信，客户端一断开连接信就发不出去了，而且这个失败只在日志里。
//  3. **邮件里不放收款账号**。它是 PII，而且邮件会被转发、被搜索、被留在收件箱
//     十年。运营需要它时后台看得到，邮件只需要说"有一张 X 元的单，去处理"。
package service

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"
)

// supplierWithdrawalNotifyTimeout 单封信的发送超时，与 emailSendTimeout 同量级。
const supplierWithdrawalNotifyTimeout = emailSendTimeout

// supplierWithdrawalUserReader 只读用户，用来拿供给者的邮箱和称呼。
//
// 窄接口的理由与本模块其他几处一样：通知不该有能力改用户，也不该有能力
// 顺手读到别人。
type supplierWithdrawalUserReader interface {
	GetByID(ctx context.Context, id int64) (*User, error)
}

// supplierWithdrawalEmailSender 是 *EmailService 的发信子集。
type supplierWithdrawalEmailSender interface {
	SendEmail(ctx context.Context, to, subject, body string) error
}

// supplierWithdrawalNotifySettings 读提现配置（拿运营收件人）与站点名。
type supplierWithdrawalNotifySettings interface {
	GetSupplyWithdrawalSettings(ctx context.Context) *SupplyWithdrawalSettings
	GetSiteName(ctx context.Context) string
}

// SupplierWithdrawalNotifier 是提现三个时刻的通知出口。
//
// 三个 Notify* 方法都立刻返回：它们只负责起一个 goroutine。真正的逻辑在同名的
// 私有方法里，是同步的、可测的。这个拆分照抄 BalanceNotifyService 的
// asyncSendQuotaAlert / sendQuotaAlertEmails。
type SupplierWithdrawalNotifier struct {
	email    supplierWithdrawalEmailSender
	users    supplierWithdrawalUserReader
	settings supplierWithdrawalNotifySettings
}

// NewSupplierWithdrawalNotifier 构造通知器。任一依赖为 nil 时整个通知静默关闭
// （ready() 返回 false）——通知不可用绝不能让提现不可用。
func NewSupplierWithdrawalNotifier(
	emailService *EmailService,
	userRepo UserRepository,
	settingService *SettingService,
) *SupplierWithdrawalNotifier {
	n := &SupplierWithdrawalNotifier{}
	if emailService != nil {
		n.email = emailService
	}
	if userRepo != nil {
		n.users = userRepo
	}
	if settingService != nil {
		n.settings = settingService
	}
	return n
}

func (n *SupplierWithdrawalNotifier) ready() bool {
	return n != nil && n.email != nil && n.settings != nil
}

// withdrawalNeedsResolvedNotice 判断一个终态值得不值得发信。
//
// 单独拆成函数而不是内联在 NotifyResolved 里：那个方法把活儿交给 goroutine 就
// 返回了，从外面断言"它没发信"只能靠 sleep。这个谓词是同步的、可以直接钉住。
func withdrawalNeedsResolvedNotice(status string) bool {
	return status == SupplierWithdrawalStatusPaid || status == SupplierWithdrawalStatusRejected
}

// ============================================================================
// 对外的三个入口。全部异步、全部 nil 安全。
// ============================================================================

// NotifyRequested 新申请到达：给供给者发一封扣款回执。
//
// M6b 之前这里还给运营发一封「有单要处理」——那是人工打款时代的命脉：
// 钱在提交时已扣，没人被叫来就永远没人处理。链上-only 后 worker 几分钟内
// 自动结算，每单一封「新申请」只会训练财务过滤这个发件人。运营现在唯一
// 需要被叫来的时刻是打款失败（NotifyPayoutFailed），用的是同一份收件人。
// 供给者的回执不变：他的可用余额在这一刻少了一块，这封信是唯一的书面凭证。
func (n *SupplierWithdrawalNotifier) NotifyRequested(w *SupplierWithdrawal) {
	if !n.ready() || w == nil {
		return
	}
	snapshot := *w
	go func() {
		n.notifyRequested(&snapshot)
	}()
}

// NotifyResolved 单子进入终态（已打款 / 被拒绝）：通知供给者。
//
// **撤回不走这里**——那是供给者自己刚点的按钮，界面上已经有确认框和 toast，
// 再补一封邮件只是噪音。运营侧也不通知：一张被本人撤回的单不需要任何人跟进。
func (n *SupplierWithdrawalNotifier) NotifyResolved(w *SupplierWithdrawal) {
	if !n.ready() || w == nil || !withdrawalNeedsResolvedNotice(w.Status) {
		return
	}
	snapshot := *w
	go func() {
		n.notifyResolved(&snapshot)
	}()
}

// NotifyPayoutFailed 链上打款没成（M4 worker 停靠到 failed）：只叫运营。
//
// **不给供给者发**：failed 不是终态，钱还扣着、结果还没定，一封「打款失败」
// 会让他立刻来问退款——而正确答案可能是运营核实后标记已打款。供给者收到的
// 下一封信永远是终态那封（paid 或 rejected）。
func (n *SupplierWithdrawalNotifier) NotifyPayoutFailed(w *SupplierWithdrawal) {
	if !n.ready() || w == nil {
		return
	}
	snapshot := *w
	go func() {
		n.notifyPayoutFailed(&snapshot)
	}()
}

// ============================================================================
// 同步实现。测试直接调这两个。
// ============================================================================

func (n *SupplierWithdrawalNotifier) notifyRequested(w *SupplierWithdrawal) {
	ctx, cancel := context.WithTimeout(context.Background(), supplierWithdrawalNotifyTimeout)
	defer cancel()

	siteName := n.siteName(ctx)

	// 只发供给者的扣款回执（运营那封已停发，见 NotifyRequested 顶部）。
	email, name := n.supplierContact(ctx, w.UserID)
	if email == "" {
		return
	}
	subject := fmt.Sprintf("[%s] 提现申请已受理 / Withdrawal request received", siteName)
	body := buildSupplierWithdrawalReceiptEmail(siteName, name, w)
	n.send(ctx, []string{email}, subject, body, "withdrawal_id", w.ID)
}

func (n *SupplierWithdrawalNotifier) notifyResolved(w *SupplierWithdrawal) {
	ctx, cancel := context.WithTimeout(context.Background(), supplierWithdrawalNotifyTimeout)
	defer cancel()

	email, name := n.supplierContact(ctx, w.UserID)
	if email == "" {
		return
	}
	siteName := n.siteName(ctx)

	var subject string
	if w.Status == SupplierWithdrawalStatusPaid {
		subject = fmt.Sprintf("[%s] 提现已打款 / Withdrawal paid", siteName)
	} else {
		subject = fmt.Sprintf("[%s] 提现申请未通过 / Withdrawal rejected", siteName)
	}
	body := buildSupplierWithdrawalResolvedEmail(siteName, name, w)
	n.send(ctx, []string{email}, subject, body, "withdrawal_id", w.ID, "status", w.Status)
}

func (n *SupplierWithdrawalNotifier) notifyPayoutFailed(w *SupplierWithdrawal) {
	ctx, cancel := context.WithTimeout(context.Background(), supplierWithdrawalNotifyTimeout)
	defer cancel()

	settings := n.settings.GetSupplyWithdrawalSettings(ctx)
	if settings == nil || len(settings.NotifyEmails) == 0 {
		// 与新申请那边同一条规矩：这正是「有单卡着但没人被叫来」的坏状态，
		// 日志是它唯一的痕迹。
		slog.Warn("[SupplierWithdrawalNotifier] 链上打款失败无人收到通知：supply_withdrawal_settings.notify_emails 为空",
			"withdrawal_id", w.ID, "amount", w.Amount)
		return
	}
	siteName := n.siteName(ctx)
	subject := fmt.Sprintf("[%s] 链上打款需要人工处理 / On-chain payout needs attention #%d", siteName, w.ID)
	body := buildSupplierWithdrawalPayoutFailedEmail(siteName, w)
	n.send(ctx, settings.NotifyEmails, subject, body, "withdrawal_id", w.ID)
}

// supplierContact 取供给者的收件邮箱与称呼。查不到用户、或用户没绑邮箱时返回空串。
func (n *SupplierWithdrawalNotifier) supplierContact(ctx context.Context, userID int64) (email, name string) {
	if n.users == nil || userID <= 0 {
		return "", ""
	}
	user, err := n.users.GetByID(ctx, userID)
	if err != nil || user == nil {
		slog.Warn("[SupplierWithdrawalNotifier] 读取供给者失败，跳过通知", "user_id", userID, "error", err)
		return "", ""
	}
	email = strings.TrimSpace(user.Email)
	if email == "" {
		return "", ""
	}
	name = strings.TrimSpace(user.Username)
	if name == "" {
		name = email
	}
	return email, name
}

func (n *SupplierWithdrawalNotifier) siteName(ctx context.Context) string {
	name := strings.TrimSpace(n.settings.GetSiteName(ctx))
	if name == "" {
		return defaultSiteName
	}
	return name
}

// send 逐个发信，失败只记日志。
func (n *SupplierWithdrawalNotifier) send(ctx context.Context, recipients []string, subject, body string, logAttrs ...any) {
	for _, to := range recipients {
		if err := n.email.SendEmail(ctx, to, subject, body); err != nil {
			attrs := append([]any{"to", to, "subject", subject, "error", err}, logAttrs...)
			slog.Error("[SupplierWithdrawalNotifier] 通知邮件发送失败", attrs...)
			continue
		}
		slog.Info("[SupplierWithdrawalNotifier] 通知邮件已发送", append([]any{"to", to, "subject", subject}, logAttrs...)...)
	}
}

// ============================================================================
// 邮件正文。三封信，都走同一个骨架。
// ============================================================================

// supplierWithdrawalEmailTemplate 格式参数：headerColorFrom, headerColorTo,
// siteName, headline, subHeadline, rowsHTML, noticeHTML。
const supplierWithdrawalEmailTemplate = `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #f5f5f5; margin: 0; padding: 20px; }
        .container { max-width: 600px; margin: 0 auto; background-color: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 2px 8px rgba(0,0,0,0.1); }
        .header { background: linear-gradient(135deg, %s 0%%, %s 100%%); color: white; padding: 30px; text-align: center; }
        .header h1 { margin: 0; font-size: 24px; }
        .content { padding: 40px 30px; }
        .metric { display: flex; justify-content: space-between; padding: 12px 0; border-bottom: 1px solid #eee; }
        .metric-label { color: #666; }
        .metric-value { font-weight: bold; color: #333; }
        .info { color: #666; font-size: 14px; line-height: 1.6; margin-top: 20px; }
        .footer { background-color: #f8f9fa; padding: 20px; text-align: center; color: #999; font-size: 12px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header"><h1>%s</h1></div>
        <div class="content">
            <p style="font-size: 18px; color: #333; text-align: center;">%s</p>
            <p style="color: #666; text-align: center;">%s</p>
            %s
            <div class="info">%s</div>
        </div>
        <div class="footer"><p>此邮件由系统自动发送，请勿回复。 / Automated message, do not reply.</p></div>
    </div>
</body>
</html>`

// withdrawalEmailRow 拼一行「标签 / 值」。两边都转义：payout_channel 与
// review_note 都是人填的自由文本，直接插进 HTML 就是一个发给自己的 XSS——
// 收件人是运营，而运营的邮箱客户端可能会渲染它。
func withdrawalEmailRow(label, value string) string {
	return fmt.Sprintf(`<div class="metric"><span class="metric-label">%s</span><span class="metric-value">%s</span></div>`,
		html.EscapeString(label), html.EscapeString(value))
}

func buildSupplierWithdrawalEmail(colorFrom, colorTo, siteName, headline, subHeadline, rows, notice string) string {
	return fmt.Sprintf(supplierWithdrawalEmailTemplate,
		colorFrom, colorTo, html.EscapeString(siteName), html.EscapeString(headline), html.EscapeString(subHeadline), rows, notice)
}

// buildSupplierWithdrawalReceiptEmail 供给者收到的扣款回执。
func buildSupplierWithdrawalReceiptEmail(siteName, userName string, w *SupplierWithdrawal) string {
	rows := withdrawalEmailRow("单号 / Request", fmt.Sprintf("#%d", w.ID)) +
		withdrawalEmailRow("金额 / Amount", fmt.Sprintf("%.2f", w.Amount)) +
		withdrawalEmailRow("收款方式 / Channel", w.PayoutChannel) +
		withdrawalEmailRow("申请时间 / Submitted", w.CreatedAt.Format("2006-01-02 15:04:05 MST"))
	notice := `<p><strong>该金额已从你的可用余额中扣除</strong>，而不是等审核通过才扣。</p>
            <p><strong>The amount has already been deducted</strong> from your available balance, not on approval.</p>
            <p>若申请未通过或你自行撤回，这笔钱会原路退回可用余额。</p>
            <p>If the request is rejected or you cancel it, the amount returns to your available balance.</p>
            <p>打款由人工处理，到账时间以站点公告为准。</p>
            <p>Payouts are processed manually; see the site notice for timing.</p>`
	return buildSupplierWithdrawalEmail("#3b82f6", "#2563eb", siteName,
		userName+"，你的提现申请已受理", "Your withdrawal request has been received", rows, notice)
}

// buildSupplierWithdrawalResolvedEmail 终态通知。已打款与被拒是两套配色和两段说明。
func buildSupplierWithdrawalResolvedEmail(siteName, userName string, w *SupplierWithdrawal) string {
	rows := withdrawalEmailRow("单号 / Request", fmt.Sprintf("#%d", w.ID)) +
		withdrawalEmailRow("金额 / Amount", fmt.Sprintf("%.2f", w.Amount)) +
		withdrawalEmailRow("收款方式 / Channel", w.PayoutChannel)

	if w.Status == SupplierWithdrawalStatusPaid {
		if w.ExternalRef != nil && strings.TrimSpace(*w.ExternalRef) != "" {
			rows += withdrawalEmailRow("打款凭证 / Reference", strings.TrimSpace(*w.ExternalRef))
		}
		notice := `<p>这笔提现已标记为已打款。到账时间取决于收款渠道本身。</p>
            <p>This withdrawal has been marked as paid. Settlement time depends on your payout channel.</p>
            <p>若长时间未到账，请带上单号与打款凭证联系我们。</p>
            <p>If it does not arrive, contact us with the request number and reference above.</p>`
		if note := withdrawalReviewNoteHTML(w); note != "" {
			notice = note + notice
		}
		return buildSupplierWithdrawalEmail("#10b981", "#059669", siteName,
			userName+"，你的提现已打款", "Your withdrawal has been paid", rows, notice)
	}

	notice := `<p><strong>金额已退回你的可用余额</strong>，可以再次申请。</p>
            <p><strong>The amount has been returned to your available balance</strong>; you may submit a new request.</p>`
	if note := withdrawalReviewNoteHTML(w); note != "" {
		notice = note + notice
	}
	return buildSupplierWithdrawalEmail("#ef4444", "#dc2626", siteName,
		userName+"，你的提现申请未通过", "Your withdrawal request was rejected", rows, notice)
}

// buildSupplierWithdrawalPayoutFailedEmail 运营收到的「链上打款没成」告警。
//
// 与新申请那封不同，这封必须带上 last_error 与 tx_hash：收件人接下来要做的
// 第一件事就是拿哈希去区块浏览器核实——尤其是「结果不明」那一种失败，
// 核实之前退款可能是双付。收款账号依旧不进邮件（后台可见）。
func buildSupplierWithdrawalPayoutFailedEmail(siteName string, w *SupplierWithdrawal) string {
	rows := withdrawalEmailRow("单号 / Request", fmt.Sprintf("#%d", w.ID)) +
		withdrawalEmailRow("金额 / Amount", fmt.Sprintf("%.2f", w.Amount)) +
		withdrawalEmailRow("收款方式 / Channel", w.PayoutChannel)
	if w.TxHash != nil && strings.TrimSpace(*w.TxHash) != "" {
		rows += withdrawalEmailRow("交易哈希 / Tx hash", strings.TrimSpace(*w.TxHash))
	}
	if w.LastError != nil && strings.TrimSpace(*w.LastError) != "" {
		rows += withdrawalEmailRow("失败原因 / Reason", strings.TrimSpace(*w.LastError))
	}
	notice := `<p><strong>钱仍扣在这张单子上，没有退回供给者。</strong></p>
            <p><strong>The amount is still held on this request; nothing has been refunded.</strong></p>
            <p>请先按交易哈希在区块浏览器上核实这笔交易的真实状态，再决定「标记已打款」或「拒绝退款」。</p>
            <p>Verify the transaction on a block explorer first, then either mark it paid or reject with a refund.</p>
            <p>在核实之前退款可能构成双付。</p>
            <p>Refunding before verifying may double-pay.</p>`
	return buildSupplierWithdrawalEmail("#ef4444", "#b91c1c", siteName,
		"链上打款需要人工处理", "On-chain payout needs attention", rows, notice)
}

// withdrawalReviewNoteHTML 把运营的处理意见放在最前面。
//
// 被拒时这段是必有的（Reject 强制要求 note），而它是供给者最需要先看到的一句话——
// 排在"钱已退回"后面的话，他得先读完两行系统说明才知道为什么被拒。
func withdrawalReviewNoteHTML(w *SupplierWithdrawal) string {
	if w.ReviewNote == nil {
		return ""
	}
	note := strings.TrimSpace(*w.ReviewNote)
	if note == "" {
		return ""
	}
	return fmt.Sprintf(`<p><strong>处理说明 / Note:</strong> %s</p>`, html.EscapeString(note))
}
