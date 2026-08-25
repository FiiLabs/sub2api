//go:build unit

// APEXONE-EXT: 双边市场——提现通知的单元测试。
//
// 测的是同步的私有方法（notifyRequested / notifyResolved），不是那两个起
// goroutine 的导出方法：异步的东西断言起来只能靠 sleep，而 sleep 会让这个
// 文件在 CI 上偶发失败，然后被人加 -skip。
//
// 三类断言：**收件人对不对**（错发是把供给者的金额和渠道发给了别人）、
// **正文里有没有不该有的东西**（收款账号是 PII）、**依赖坏掉时会不会拖垮提现**。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sentEmail struct {
	to      string
	subject string
	body    string
}

type withdrawalEmailSenderStub struct {
	sent []sentEmail
	err  error
}

func (s *withdrawalEmailSenderStub) SendEmail(_ context.Context, to, subject, body string) error {
	s.sent = append(s.sent, sentEmail{to: to, subject: subject, body: body})
	return s.err
}

func (s *withdrawalEmailSenderStub) recipients() []string {
	out := make([]string, 0, len(s.sent))
	for _, item := range s.sent {
		out = append(out, item.to)
	}
	return out
}

func (s *withdrawalEmailSenderStub) bodyTo(to string) string {
	for _, item := range s.sent {
		if item.to == to {
			return item.body
		}
	}
	return ""
}

type withdrawalUserReaderStub struct {
	user *User
	err  error
}

func (s *withdrawalUserReaderStub) GetByID(_ context.Context, _ int64) (*User, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

type withdrawalNotifySettingsStub struct {
	settings *SupplyWithdrawalSettings
	siteName string
}

func (s *withdrawalNotifySettingsStub) GetSupplyWithdrawalSettings(context.Context) *SupplyWithdrawalSettings {
	if s.settings == nil {
		return DefaultSupplyWithdrawalSettings()
	}
	return s.settings
}

func (s *withdrawalNotifySettingsStub) GetSiteName(context.Context) string { return s.siteName }

func newTestNotifier(
	email supplierWithdrawalEmailSender,
	user supplierWithdrawalUserReader,
	settings supplierWithdrawalNotifySettings,
) *SupplierWithdrawalNotifier {
	return &SupplierWithdrawalNotifier{email: email, users: user, settings: settings}
}

func testWithdrawal() *SupplierWithdrawal {
	return &SupplierWithdrawal{
		ID:            42,
		UserID:        7,
		Amount:        250.5,
		Status:        SupplierWithdrawalStatusPending,
		PayoutChannel: "USDT-TRC20",
		PayoutAccount: "TXyz000SecretPayoutAddress999",
		CreatedAt:     time.Unix(1700000000, 0).UTC(),
	}
}

func supplierUser() *User {
	return &User{ID: 7, Email: "supplier@example.com", Username: "小李"}
}

func notifySettings(emails ...string) *withdrawalNotifySettingsStub {
	return &withdrawalNotifySettingsStub{
		settings: &SupplyWithdrawalSettings{Enabled: true, MaxPending: 1, Channels: []string{"USDT-TRC20"}, NotifyEmails: emails},
		siteName: "ApexOne",
	}
}

// ============================================================================
// 新申请：只有供给者的扣款回执（M6b 起运营那封停发）
// ============================================================================

// 运营收件人配了也**不**收「新申请」——自动结算下那封信只会被过滤。
// 运营被叫来的唯一时刻是打款失败（TestNotifyPayoutFailed* 那组）。
func TestNotifyRequestedOnlyReceiptsTheSupplier(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestNotifier(email, &withdrawalUserReaderStub{user: supplierUser()}, notifySettings("ops@example.com", "finance@example.com"))

	n.notifyRequested(testWithdrawal())

	assert.Equal(t, []string{"supplier@example.com"}, email.recipients(),
		"运营收到了新申请邮件——每单一封的下场是财务把这个发件人整个过滤掉")
}

// 收款账号是 PII，而邮件会被转发、被搜索、被留在收件箱十年。运营需要它时后台看得到。
func TestNotifyRequestedNeverLeaksPayoutAccount(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestNotifier(email, &withdrawalUserReaderStub{user: supplierUser()}, notifySettings("ops@example.com"))

	w := testWithdrawal()
	n.notifyRequested(w)

	require.NotEmpty(t, email.sent)
	for _, item := range email.sent {
		assert.NotContains(t, item.body, w.PayoutAccount,
			"收款账号不能出现在任何一封邮件里（收件人: %s）", item.to)
	}
}

// 供给者那封必须写明"钱已经扣了"。这是这个功能最容易被误解的一点，
// 而邮件是他唯一的书面凭证。
func TestNotifyRequestedReceiptExplainsDeduction(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestNotifier(email, &withdrawalUserReaderStub{user: supplierUser()}, notifySettings("ops@example.com"))

	n.notifyRequested(testWithdrawal())

	body := email.bodyTo("supplier@example.com")
	require.NotEmpty(t, body)
	assert.Contains(t, body, "已从你的可用余额中扣除")
	assert.Contains(t, body, "退回可用余额", "必须同时说明什么情况下退回，否则只有坏消息")
}

// 供给者没绑邮箱（或查不到）时，一封信都不发、也不报错——
// 回执是尽力而为的凭证，不是建单的前置条件。
func TestNotifyRequestedSendsNothingWhenSupplierUnreachable(t *testing.T) {
	cases := []struct {
		name  string
		users supplierWithdrawalUserReader
	}{
		{"查用户报错", &withdrawalUserReaderStub{err: errors.New("db down")}},
		{"用户没绑邮箱", &withdrawalUserReaderStub{user: &User{ID: 7, Username: "小李"}}},
		{"用户不存在", &withdrawalUserReaderStub{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			email := &withdrawalEmailSenderStub{}
			n := newTestNotifier(email, tc.users, notifySettings("ops@example.com"))
			n.notifyRequested(testWithdrawal())
			assert.Empty(t, email.recipients())
		})
	}
}

// ============================================================================
// 终态：只通知供给者
// ============================================================================

func TestNotifyResolvedPaidIncludesReference(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestNotifier(email, &withdrawalUserReaderStub{user: supplierUser()}, notifySettings("ops@example.com"))

	ref := "TX-998877"
	w := testWithdrawal()
	w.Status = SupplierWithdrawalStatusPaid
	w.ExternalRef = &ref
	n.notifyResolved(w)

	require.Equal(t, []string{"supplier@example.com"}, email.recipients(), "终态只通知供给者，运营刚点完按钮")
	assert.Contains(t, email.sent[0].body, ref, "打款凭证是他去自己渠道对账的唯一抓手")
}

// 被拒时理由必须在正文里。一笔被拒的提现不给理由，等于让他重新提交一次
// 同样会被拒的单。
func TestNotifyResolvedRejectedCarriesReason(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestNotifier(email, &withdrawalUserReaderStub{user: supplierUser()}, notifySettings())

	note := "收款账号与实名信息不一致"
	w := testWithdrawal()
	w.Status = SupplierWithdrawalStatusRejected
	w.ReviewNote = &note
	n.notifyResolved(w)

	require.Len(t, email.sent, 1)
	assert.Contains(t, email.sent[0].body, note)
	assert.Contains(t, email.sent[0].body, "已退回你的可用余额")
}

// 运营填的处理意见是自由文本，直接插进 HTML 就是一封发给供给者的 XSS。
func TestNotifyResolvedEscapesReviewNote(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestNotifier(email, &withdrawalUserReaderStub{user: supplierUser()}, notifySettings())

	note := `<script>alert(1)</script>`
	w := testWithdrawal()
	w.Status = SupplierWithdrawalStatusRejected
	w.ReviewNote = &note
	n.notifyResolved(w)

	require.Len(t, email.sent, 1)
	assert.NotContains(t, email.sent[0].body, "<script>")
	assert.Contains(t, email.sent[0].body, "&lt;script&gt;")
}

// 渠道名同样是运营填的自由文本，走的是另一个拼接函数，单独钉一次。
func TestNotifyRequestedEscapesPayoutChannel(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestNotifier(email, &withdrawalUserReaderStub{user: supplierUser()}, notifySettings("ops@example.com"))

	w := testWithdrawal()
	w.PayoutChannel = `<img src=x onerror=alert(1)>`
	n.notifyRequested(w)

	require.NotEmpty(t, email.sent)
	for _, item := range email.sent {
		assert.NotContains(t, item.body, "<img src=x")
	}
}

// 只有已打款和被拒才发终态信。撤回必须是空操作——服务层已经不调它了，但这一层
// 再挡一次：将来有人给撤回也接上通知时，会先撞到这条测试。
//
// 断言的是同步谓词而不是 NotifyResolved 的副作用：后者把活儿交给 goroutine 就
// 返回了，"email.sent 是空的"在那种写法下永远成立——哪怕守卫被整个删掉。
func TestWithdrawalNeedsResolvedNotice(t *testing.T) {
	cases := map[string]bool{
		SupplierWithdrawalStatusPaid:     true,
		SupplierWithdrawalStatusRejected: true,
		SupplierWithdrawalStatusPending:  false,
		SupplierWithdrawalStatusCanceled: false,
		"":                               false,
		"weird":                          false,
	}
	for status, want := range cases {
		t.Run("status="+status, func(t *testing.T) {
			assert.Equal(t, want, withdrawalNeedsResolvedNotice(status))
		})
	}
}

// ============================================================================
// 坏掉的依赖不能拖垮提现
// ============================================================================

// SMTP 挂了只能是日志里的一行。一封发不出去的信不该让第二个收件人也收不到。
func TestNotifySurvivesSMTPFailure(t *testing.T) {
	email := &withdrawalEmailSenderStub{err: errors.New("smtp: connection refused")}
	n := newTestNotifier(email, &withdrawalUserReaderStub{user: supplierUser()}, notifySettings("ops@example.com", "finance@example.com"))

	assert.NotPanics(t, func() { n.notifyRequested(testWithdrawal()) })
	// 多收件人的"一封失败不中断后面"由 failed 告警那条路覆盖（它发给整个
	// 运营收件人列表）；新申请现在只有供给者一封。
	failed := testWithdrawal()
	failed.Status = SupplierWithdrawalStatusFailed
	assert.NotPanics(t, func() { n.notifyPayoutFailed(failed) })
	assert.Len(t, email.sent, 3, "一封失败不能中断后面的收件人")
}

// 依赖没配齐时整个通知静默关闭，而不是 panic 在提现的主路径上。
//
// 主断言是 ready() 为 false 而不是"没 panic"：Notify* 起 goroutine 后立刻返回，
// 那个 panic 会发生在测试断言之后的某个时刻，抓不到。ready() 是同步的闸门。
func TestNotifierNotReadyIsSilent(t *testing.T) {
	cases := []struct {
		name string
		n    *SupplierWithdrawalNotifier
	}{
		{"nil 通知器", nil},
		{"没有邮件服务", newTestNotifier(nil, &withdrawalUserReaderStub{user: supplierUser()}, notifySettings("ops@example.com"))},
		{"没有设置", newTestNotifier(&withdrawalEmailSenderStub{}, &withdrawalUserReaderStub{user: supplierUser()}, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.False(t, tc.n.ready(), "依赖不全时必须关闸，否则 goroutine 里会 nil panic")
			assert.NotPanics(t, func() {
				tc.n.NotifyRequested(testWithdrawal())
				tc.n.NotifyResolved(testWithdrawal())
			})
		})
	}
}

// 构造函数拿到 nil 依赖时 ready() 必须是 false，而不是留下一个装着 nil 指针的
// 非 nil 接口——那种接口每一个方法调用都是 panic。
func TestNewNotifierWithNilDepsIsNotReady(t *testing.T) {
	n := NewSupplierWithdrawalNotifier(nil, nil, nil)
	require.NotNil(t, n)
	assert.False(t, n.ready())
	assert.NotPanics(t, func() { n.NotifyRequested(testWithdrawal()) })
}

// 站点名读不到时用默认值，而不是发出一封主题是 "[] 新的提现申请" 的邮件。
func TestNotifyFallsBackToDefaultSiteName(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	settings := notifySettings("ops@example.com")
	settings.siteName = "   "
	n := newTestNotifier(email, &withdrawalUserReaderStub{user: supplierUser()}, settings)

	n.notifyRequested(testWithdrawal())

	require.NotEmpty(t, email.sent)
	assert.True(t, strings.HasPrefix(email.sent[0].subject, "["+defaultSiteName+"]"), "主题: %s", email.sent[0].subject)
}

// 用户没设用户名时用邮箱当称呼，正文里不能出现一个空荡荡的开头。
func TestNotifyUsesEmailAsNameWhenUsernameEmpty(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestNotifier(email, &withdrawalUserReaderStub{user: &User{ID: 7, Email: "supplier@example.com"}}, notifySettings())

	n.notifyRequested(testWithdrawal())

	require.Len(t, email.sent, 1)
	assert.Contains(t, email.sent[0].body, "supplier@example.com，你的提现申请已受理")
}
