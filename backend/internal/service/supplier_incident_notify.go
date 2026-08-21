// APEXONE-EXT: 双边市场——供给账号失效通知。
//
// # 与提现通知的三点不同
//
//  1. **同步**。提现那边的三个入口都是「起一个 goroutine 就返回」，因为调用方是一条
//     HTTP 请求路径，不能等 SMTP。这里的调用方是后台扫描，它必须知道信到底发出去
//     没有——`notified_at` 只有在发信成功时才该写下去（见 supplier_incident_service.go
//     的 notifyPending）。异步在这里等于把那一列变成「我们试过发」。
//  2. **只发给供给者，不发给运营**。一个号坏了是那个人的损失，不是运营的待办；
//     运营需要的是趋势（封禁率报表），不是每个号一封邮件。真到了该惊动运营的量级，
//     那是告警的事，不是收件箱的事。
//  3. **不把上游的错误原文放进信里**。error_message 可能带 token 片段、内部地址、
//     或者一整段上游 JSON。供给者需要的是「你的哪个号、什么时候、大概是什么问题、
//     去哪里修」；原文留在管理端报表里给运营看。
package service

import (
	"context"
	"fmt"
	"html"
	"strings"
)

// supplierIncidentNotifyTimeout 单封信的发送超时。
const supplierIncidentNotifyTimeout = emailSendTimeout

// SupplierIncidentNotifier 给供给者发「你的号停了」。
//
// 依赖与提现通知器同形（发信、读用户、读站点名），但**不读提现设置**：
// 这封信没有运营收件人。
type SupplierIncidentNotifier struct {
	email    supplierWithdrawalEmailSender
	users    supplierWithdrawalUserReader
	settings supplierIncidentNotifySettings
}

// supplierIncidentNotifySettings 只用来取站点名。
type supplierIncidentNotifySettings interface {
	GetSiteName(ctx context.Context) string
}

// NewSupplierIncidentNotifier 构造通知器。
//
// 缺发信能力或缺用户读取能力时返回 nil：调用方（NewSupplierIncidentService）
// 据此把通知整块关掉。返回一个「什么都做不了但不是 nil」的通知器会让扫描每轮
// 都走一遍发信循环、每条事件都失败一次、日志里刷满一模一样的错误。
func NewSupplierIncidentNotifier(
	emailService *EmailService,
	userRepo UserRepository,
	settingService *SettingService,
) *SupplierIncidentNotifier {
	if emailService == nil || userRepo == nil {
		return nil
	}
	n := &SupplierIncidentNotifier{email: emailService, users: userRepo}
	if settingService != nil {
		n.settings = settingService
	}
	return n
}

// NotifyIncident 发一封「你的供给账号已停止接单」。
//
// 返回 error 的语义是「这封信没发出去」。收件人查不到或者没有邮箱**不算失败**：
// 那不是一个重试能解决的问题，报错会让这条事件在每一轮扫描里都被重试一次、
// 永远发不出去也永远不被标记。返回 nil 让它被记成"已通知"，事情就此了结。
func (n *SupplierIncidentNotifier) NotifyIncident(ctx context.Context, incident *SupplierAccountIncident) error {
	if n == nil || n.email == nil || incident == nil {
		return fmt.Errorf("incident notifier unavailable")
	}

	sendCtx, cancel := context.WithTimeout(ctx, supplierIncidentNotifyTimeout)
	defer cancel()

	email, name := n.contact(sendCtx, incident.UserID)
	if email == "" {
		return nil
	}
	siteName := n.siteName(sendCtx)
	subject := fmt.Sprintf("[%s] 你的供给账号已停止接单 / Your supply account has stopped", siteName)
	return n.email.SendEmail(sendCtx, email, subject, buildSupplierIncidentEmail(siteName, name, incident))
}

func (n *SupplierIncidentNotifier) contact(ctx context.Context, userID int64) (email, name string) {
	if n.users == nil || userID <= 0 {
		return "", ""
	}
	user, err := n.users.GetByID(ctx, userID)
	if err != nil || user == nil {
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

func (n *SupplierIncidentNotifier) siteName(ctx context.Context) string {
	if n.settings == nil {
		return defaultSiteName
	}
	name := strings.TrimSpace(n.settings.GetSiteName(ctx))
	if name == "" {
		return defaultSiteName
	}
	return name
}

// ============================================================================
// 正文
// ============================================================================

// supplierIncidentStatusHint 把一个上游状态翻译成一句人话。
//
// # 为什么要翻译
//
// 收件人是把自己的订阅挂上来的普通人，`error` 对他没有意义。更要紧的是这两种
// 状态**要做的事完全不同**：凭证失效要他重新授权一次，被停用则是我们这边动的手、
// 他该来问我们。一封只写着"你的号 status=error"的邮件等于没写。
//
// `accounts.status` 目前只有 active / error / disabled 三个取值（domain/constants.go），
// 限流不走这一列（它由 schedulable 与冷却时刻表达），所以一次限流不会开出事件——
// 那是对的：限流会自己好，不值得惊动任何人。
//
// 认不出的状态给一句中性的兜底而不是把原值印上去：上游随时会加新状态，
// 而一个印着自己看不懂的英文单词的通知只会让人来问客服。
func supplierIncidentStatusHint(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusError:
		return "账号凭证可能已失效或被上游拒绝，通常需要重新授权一次。 / " +
			"The credentials may have expired or been rejected upstream; re-authorising usually fixes it."
	case StatusDisabled:
		return "账号已被停用。若不是你自己操作的，请联系我们。 / " +
			"The account has been disabled. If you did not do this, please contact us."
	default:
		return "账号当前不可用。到供给页看一眼，必要时重新授权。 / " +
			"The account is currently unavailable. Check the supply page and re-authorise if needed."
	}
}

// buildSupplierIncidentEmail 拼正文。复用提现通知那套模板与转义助手——
// 同一个站点发出去的信长得一样，是件好事。
func buildSupplierIncidentEmail(siteName, userName string, incident *SupplierAccountIncident) string {
	accountName := strings.TrimSpace(incident.AccountName)
	if accountName == "" {
		accountName = fmt.Sprintf("#%d", incident.AccountID)
	}
	rows := withdrawalEmailRow("账号 / Account", accountName) +
		withdrawalEmailRow("平台 / Platform", incident.Platform) +
		withdrawalEmailRow("发现时间 / Detected", incident.DetectedAt.Format("2006-01-02 15:04:05 MST"))

	notice := fmt.Sprintf(`<p><strong>这个号已经停止接单，期间不会产生任何收益。</strong></p>
            <p><strong>It has stopped serving requests and is not earning while in this state.</strong></p>
            <p>%s</p>
            <p>恢复之后它会自动重新开始接单，你不需要再通知我们。</p>
            <p>Once it recovers it resumes automatically; no need to tell us.</p>
            <p>已经入账的收益不受影响。 / Credits you have already earned are unaffected.</p>`,
		html.EscapeString(supplierIncidentStatusHint(incident.Status)))

	return buildSupplierWithdrawalEmail("#ef4444", "#dc2626", siteName,
		userName+"，你的一个供给账号停了", "One of your supply accounts has stopped", rows, notice)
}
