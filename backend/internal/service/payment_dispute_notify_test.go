//go:build unit

// APEXONE-EXT: 双边市场——拒付通知的单元测试。
//
// 与提现通知同样的做法：测同步的私有方法，不测那个起 goroutine 的导出方法。
//
// 这里最要紧的一条不是"信长什么样"，而是**信里必须写清楚收信人现在该做什么**。
// 拒付的应诉窗口只有几天，一封没写"去支付服务商后台上传证据"的通知，
// 与没发是一样的。
package service

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDisputeNotifier(
	email supplierWithdrawalEmailSender,
	settings supplierWithdrawalNotifySettings,
) *PaymentDisputeNotifier {
	return &PaymentDisputeNotifier{email: email, settings: settings}
}

func testDisputeNotice() PaymentDisputeNotice {
	return PaymentDisputeNotice{
		DisputeID:     "dp_1",
		ProviderKey:   payment.TypeStripe,
		Status:        payment.DisputeStatusOpen,
		Reason:        "fraudulent",
		Currency:      "USD",
		DisputeAmount: 14.99,
		OrderID:       77,
		OutTradeNo:    "OUT-77",
		UserID:        9,
		UserEmail:     "consumer@example.com",
		OrderType:     payment.OrderTypeBalance,
		BasisAmount:   100,
		Settlement: &PaymentDisputeSettlement{
			DisputeID: "dp_1", BalanceDeducted: 40, ClawedCredit: 12.5,
			ClawedBasis: 80, UncoveredBasis: 20,
		},
	}
}

// 收件人复用提现那份列表：同一个变更理由用同一个 key（见文件头 doctrine）。
func TestNotifyDisputeUsesWithdrawalRecipients(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestDisputeNotifier(email, notifySettings("ops@example.com", "finance@example.com"))

	n.notifyDispute(testDisputeNotice())

	assert.ElementsMatch(t, []string{"ops@example.com", "finance@example.com"}, email.recipients())
}

// 收件人为空是一种坏状态：钱少了，没有人知道。至少得有日志，且绝不能 panic。
//
// 这里必须真的去断言那条日志，不能只断言"没发出邮件"：收件人列表为空时
// 发信循环本来就是空转，去掉那句前置判断照样一封不发。也就是说这个分支
// **唯一**的产物就是那条 WARN——不钉它，等于这段代码没有测试。
func TestNotifyDisputeWithNoRecipientsIsSilent(t *testing.T) {
	logs := captureSlog(t)
	email := &withdrawalEmailSenderStub{}
	n := newTestDisputeNotifier(email, notifySettings())

	assert.NotPanics(t, func() { n.notifyDispute(testDisputeNotice()) })
	assert.Empty(t, email.sent)

	line := logs.String()
	assert.Contains(t, line, "notify_emails 为空", "这条 WARN 是这次拒付留下的唯一痕迹")
	assert.Contains(t, line, "dp_1", "日志里必须带 dispute_id，否则运营无从查起")
}

// captureSlog 把默认 logger 换成写进 buffer 的那个，测完还回去。
// 与包内既有做法一致（见 grok_quota_service_test.go）。
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

// 依赖没配齐时整个通知静默关闭，而不是在 webhook 的 goroutine 里 nil panic。
func TestNotifyDisputeNilDepsAreSafe(t *testing.T) {
	cases := map[string]*PaymentDisputeNotifier{
		"nil 通知器": nil,
		"没有邮件服务":  newTestDisputeNotifier(nil, notifySettings("ops@example.com")),
		"没有设置":    newTestDisputeNotifier(&withdrawalEmailSenderStub{}, nil),
		"两个都没有":   newTestDisputeNotifier(nil, nil),
		"构造函数拿到空": NewPaymentDisputeNotifier(nil, nil),
	}
	for name, notifier := range cases {
		t.Run(name, func(t *testing.T) {
			assert.NotPanics(t, func() { notifier.NotifyDispute(testDisputeNotice()) })
		})
	}
}

// open 那封信里必须有"应诉有时限、证据得去后台传"这句话——那是收信人唯一
// 必须立刻做的动作，而系统代劳不了。
func TestDisputeEmailTellsOperatorWhatToDo(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestDisputeNotifier(email, notifySettings("ops@example.com"))

	n.notifyDispute(testDisputeNotice())

	require.Len(t, email.sent, 1)
	body := email.sent[0].body
	assert.Contains(t, body, "应诉有时限")
	assert.Contains(t, body, "历史拒付次数", "反复拒付是最强的欺诈信号，得提醒人去查")
}

// 胜诉那封信必须说清楚"系统不会自动回补"，否则运营会以为钱已经还回去了，
// 而实际上消费者的余额和供给者的分成都还扣着。
func TestWonDisputeEmailSaysNoAutoRestore(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestDisputeNotifier(email, notifySettings("ops@example.com"))

	notice := testDisputeNotice()
	notice.Status = payment.DisputeStatusWon
	notice.Settlement = nil
	n.notifyDispute(notice)

	require.Len(t, email.sent, 1)
	assert.Contains(t, email.sent[0].subject, "胜诉")
	assert.Contains(t, email.sent[0].body, "系统不会自动回补")
}

// 败诉那封信要点出 uncovered——它是 freeze_hours 配短了的直接证据。
func TestLostDisputeEmailMentionsUncoveredBasis(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestDisputeNotifier(email, notifySettings("ops@example.com"))

	notice := testDisputeNotice()
	notice.Status = payment.DisputeStatusLost
	n.notifyDispute(notice)

	require.Len(t, email.sent, 1)
	assert.Contains(t, email.sent[0].subject, "败诉")
	assert.Contains(t, email.sent[0].body, "freeze_hours")
	assert.Contains(t, email.sent[0].body, "20.00", "未覆盖基数要摆在信里")
}

// 对不上订单那封信要说清"系统什么都没动"，并把人指到支付后台去认领。
// 这是多套环境共用一个支付账户时的常态，看信的人必须知道不用慌。
func TestOrphanDisputeEmailSaysNothingWasTouched(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestDisputeNotifier(email, notifySettings("ops@example.com"))

	notice := testDisputeNotice()
	notice.OrderID = 0
	notice.Settlement = nil
	n.notifyDispute(notice)

	require.Len(t, email.sent, 1)
	body := email.sent[0].body
	assert.Contains(t, body, "找不到对应订单")
	assert.Contains(t, body, "没有对任何余额或分成做过改动")
	assert.NotContains(t, body, "consumer@example.com", "订单都没对上，不该把某个用户扯进来")
}

// 三个金额要出现在信里：不出现的话，运营得自己去翻库才能知道亏了多少。
func TestDisputeEmailCarriesSettlementNumbers(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	n := newTestDisputeNotifier(email, notifySettings("ops@example.com"))

	n.notifyDispute(testDisputeNotice())

	require.Len(t, email.sent, 1)
	body := email.sent[0].body
	assert.Contains(t, body, "40.00", "已扣回余额")
	assert.Contains(t, body, "12.50", "已追回分成")
	assert.Contains(t, body, "20.00", "未覆盖基数")
	assert.Contains(t, body, "dp_1")
	assert.Contains(t, body, "#77")
}

// 站点名读不到时用默认值，而不是发一封主题是 "[] 收到拒付" 的邮件。
func TestDisputeEmailFallsBackToDefaultSiteName(t *testing.T) {
	email := &withdrawalEmailSenderStub{}
	settings := notifySettings("ops@example.com")
	settings.siteName = "   "
	n := newTestDisputeNotifier(email, settings)

	n.notifyDispute(testDisputeNotice())

	require.Len(t, email.sent, 1)
	assert.True(t, strings.HasPrefix(email.sent[0].subject, "["+defaultSiteName+"]"),
		"主题: %s", email.sent[0].subject)
}

// 一封发不出去不能让第二个收件人也收不到。
func TestDisputeNotifySurvivesSMTPFailure(t *testing.T) {
	email := &withdrawalEmailSenderStub{err: errors.New("smtp: connection refused")}
	n := newTestDisputeNotifier(email, notifySettings("ops@example.com", "finance@example.com"))

	assert.NotPanics(t, func() { n.notifyDispute(testDisputeNotice()) })
	assert.Len(t, email.sent, 2, "一封失败不能中断后面的收件人")
}
