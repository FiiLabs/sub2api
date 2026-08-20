//go:build unit

// APEXONE-EXT: 双边市场——Stripe 争议事件解析的单元测试。
//
// 最要紧的一条在 TestStripeDisputeStatus：**询证类事件必须被当成"钱没动"**。
// 把它认成拒付，运营看到的现象是「有人问了一句，供给者的钱就没了」，
// 而那笔钱当时还稳稳在我们账上。
package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v85"
)

const testDisputeWebhookSecret = "whsec_test_secret"

func newDisputeProvider() *Stripe {
	return &Stripe{
		config: map[string]string{
			"secretKey":     "sk_test",
			"currency":      "USD",
			"webhookSecret": testDisputeWebhookSecret,
		},
		initialized: true,
	}
}

// signStripePayload 按 Stripe 的规则算一个真签名。
// 自己算而不是塞一个假的：验签这一步本身就是这条路径的安全边界，
// 绕过它测出来的"能解析"没有意义。
func signStripePayload(t *testing.T, payload string) map[string]string {
	t.Helper()
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(testDisputeWebhookSecret))
	_, err := fmt.Fprintf(mac, "%d.%s", ts, payload)
	require.NoError(t, err)
	return map[string]string{
		"stripe-signature": fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil))),
	}
}

// 报文里那两个"多余"的字段都是 ConstructEvent 硬性要求的，真实推送一定带着：
//   - "object":"event"：少了它会被当成 thin event notification 直接拒掉。
//   - "api_version"：与 stripe-go 编译进来的版本不一致时同样拒掉。这条约束不是
//     争议路径特有的——core 的 VerifyNotification 用的是同一个 ConstructEvent，
//     版本对不上时整条支付 webhook 就先挂了，轮不到争议解析。两边保持一致即可，
//     不要在这里单独放宽（放宽意味着用错版本的字段去反序列化金额）。
func disputeEventPayload(eventType, disputeID, status string, amount int64) string {
	return fmt.Sprintf(`{"id":"evt_1","object":"event","api_version":%q,"type":%q,"data":{"object":{
        "id":%q,"object":"dispute","amount":%d,"currency":"usd","status":%q,
        "reason":"fraudulent","payment_intent":"pi_9"}}}`,
		stripe.APIVersion, eventType, disputeID, amount, status)
}

func TestStripeDisputeStatus(t *testing.T) {
	cases := []struct {
		status    stripe.DisputeStatus
		want      string
		fundsMove bool
	}{
		{stripe.DisputeStatusNeedsResponse, payment.DisputeStatusOpen, true},
		{stripe.DisputeStatusUnderReview, payment.DisputeStatusOpen, true},
		{stripe.DisputeStatusWon, payment.DisputeStatusWon, true},
		{stripe.DisputeStatusLost, payment.DisputeStatusLost, true},
		// 以下四种：Stripe 通知我们有人在问，款项一分没动。
		{stripe.DisputeStatusWarningNeedsResponse, "", false},
		{stripe.DisputeStatusWarningUnderReview, "", false},
		{stripe.DisputeStatusWarningClosed, "", false},
		{stripe.DisputeStatusPrevented, "", false},
		// 将来 Stripe 新增的任何状态都默认按"钱没动"处理：
		// 漏一次追回可以事后补，凭空收走供给者的钱不能。
		{stripe.DisputeStatus("some_future_status"), "", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			got, moved := stripeDisputeStatus(tc.status)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.fundsMove, moved)
		})
	}
}

func TestVerifyDisputeNotification_ParsesChargeback(t *testing.T) {
	p := newDisputeProvider()
	payload := disputeEventPayload("charge.dispute.created", "dp_1", "needs_response", 1499)

	n, err := p.VerifyDisputeNotification(context.Background(), payload, signStripePayload(t, payload))
	require.NoError(t, err)
	require.NotNil(t, n)

	assert.Equal(t, "dp_1", n.DisputeID)
	assert.Equal(t, payment.DisputeStatusOpen, n.Status)
	assert.Equal(t, "pi_9", n.TradeNo, "payment_intent 是查订单的唯一线索")
	assert.Equal(t, "USD", n.Currency)
	assert.InDelta(t, 14.99, n.DisputeAmount, 1e-9, "报文是分，落库是主单位")
	assert.Equal(t, "fraudulent", n.Reason)
	assert.Equal(t, payload, n.RawData)
}

// 五种 charge.dispute.* 事件都要认，因为它们带的是同一个 Dispute 对象，
// 而"钱在谁手里"完全写在 status 里，与事件名无关。
func TestVerifyDisputeNotification_AcceptsAllDisputeEventTypes(t *testing.T) {
	p := newDisputeProvider()
	for _, eventType := range []string{
		"charge.dispute.created", "charge.dispute.updated", "charge.dispute.closed",
		"charge.dispute.funds_withdrawn", "charge.dispute.funds_reinstated",
	} {
		t.Run(eventType, func(t *testing.T) {
			payload := disputeEventPayload(eventType, "dp_2", "lost", 1000)
			n, err := p.VerifyDisputeNotification(context.Background(), payload, signStripePayload(t, payload))
			require.NoError(t, err)
			require.NotNil(t, n)
			assert.Equal(t, payment.DisputeStatusLost, n.Status)
		})
	}
}

// 询证：验签过、事件类型也对，但必须返回 (nil, nil)——一分钱都不动。
func TestVerifyDisputeNotification_IgnoresInquiry(t *testing.T) {
	p := newDisputeProvider()
	payload := disputeEventPayload("charge.dispute.created", "dp_3", "warning_needs_response", 1499)

	n, err := p.VerifyDisputeNotification(context.Background(), payload, signStripePayload(t, payload))
	require.NoError(t, err)
	assert.True(t, n == nil, "询证不是拒付，款项还在我们手里")
}

// 非争议事件从这里安静地掉出去：webhook 是共用的，绝大多数事件都不是争议。
func TestVerifyDisputeNotification_IgnoresNonDisputeEvent(t *testing.T) {
	p := newDisputeProvider()
	payload := fmt.Sprintf(`{"id":"evt_2","object":"event","api_version":%q,`+
		`"type":"payment_intent.succeeded","data":{"object":{"id":"pi_1"}}}`, stripe.APIVersion)

	n, err := p.VerifyDisputeNotification(context.Background(), payload, signStripePayload(t, payload))
	require.NoError(t, err)
	assert.True(t, n == nil)
}

// 没有争议 id 就没有幂等键，而这条路径的副作用是扣钱。宁可报错。
func TestVerifyDisputeNotification_RejectsMissingDisputeID(t *testing.T) {
	p := newDisputeProvider()
	payload := disputeEventPayload("charge.dispute.created", "", "needs_response", 1499)

	n, err := p.VerifyDisputeNotification(context.Background(), payload, signStripePayload(t, payload))
	require.Error(t, err)
	assert.True(t, n == nil)
}

func TestVerifyDisputeNotification_RejectsBadSignature(t *testing.T) {
	p := newDisputeProvider()
	payload := disputeEventPayload("charge.dispute.created", "dp_4", "needs_response", 1499)
	headers := signStripePayload(t, payload+"tampered")

	n, err := p.VerifyDisputeNotification(context.Background(), payload, headers)
	require.Error(t, err)
	assert.True(t, n == nil)
}

func TestVerifyDisputeNotification_RequiresSecretAndSignature(t *testing.T) {
	payload := disputeEventPayload("charge.dispute.created", "dp_5", "needs_response", 1499)

	// 两条都会被 ConstructEvent 自己挡下来，所以这里断言的不只是"报错"，
	// 还有**报的是哪个错**：少了这两句前置检查，运维看到的是一句关于签名格式的
	// 通用错误，而真正的原因是配置没填。
	noSecret := &Stripe{config: map[string]string{"secretKey": "sk_test"}, initialized: true}
	_, err := noSecret.VerifyDisputeNotification(context.Background(), payload, signStripePayload(t, payload))
	require.Error(t, err, "没配 webhookSecret 时不能当作验签通过")
	assert.Contains(t, err.Error(), "webhookSecret not configured")

	_, err = newDisputeProvider().VerifyDisputeNotification(context.Background(), payload, map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing stripe-signature")
}

// 报文里带了我们的商户订单号（payment_intent 被展开过）时要捞出来当兜底。
func TestStripeDisputeOrderID(t *testing.T) {
	assert.Equal(t, "", stripeDisputeOrderID(nil))
	assert.Equal(t, "", stripeDisputeOrderID(&stripe.Dispute{}))
	assert.Equal(t, "OUT-1", stripeDisputeOrderID(&stripe.Dispute{Metadata: map[string]string{"orderId": "OUT-1"}}))
	assert.Equal(t, "OUT-2", stripeDisputeOrderID(&stripe.Dispute{
		PaymentIntent: &stripe.PaymentIntent{Metadata: map[string]string{"orderId": "OUT-2"}},
	}))
}
