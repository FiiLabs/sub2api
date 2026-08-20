//go:build unit

// APEXONE-EXT: 双边市场——争议 webhook 分派的单元测试。
//
// 这里钉的是分派本身，不是解析（那在 provider 侧）也不是副作用（那在 service 侧）：
//
//   - 不实现争议接口的通道要被跳过，而不是让整个循环停在第一个通道上；
//   - 一组同类型实例里只有一个持有对的 webhookSecret，其余验签失败是常态，
//     必须继续往下试；
//   - 认出来之后就地停手，不能让同一条报文被第二个实例再解析一遍。
package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainProviderStub 只实现 payment.Provider，不认争议事件——绝大多数通道的形态。
type plainProviderStub struct {
	key string
}

func (p *plainProviderStub) Name() string        { return p.key }
func (p *plainProviderStub) ProviderKey() string { return p.key }
func (p *plainProviderStub) SupportedTypes() []payment.PaymentType {
	return nil
}
func (p *plainProviderStub) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	return nil, errors.New("not implemented")
}
func (p *plainProviderStub) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	return nil, nil
}
func (p *plainProviderStub) QueryOrder(context.Context, string) (*payment.QueryOrderResponse, error) {
	return nil, errors.New("not implemented")
}
func (p *plainProviderStub) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, errors.New("not implemented")
}

// disputeProviderStub 额外实现 DisputeAwareProvider。
type disputeProviderStub struct {
	plainProviderStub
	calls        int
	notification *payment.DisputeNotification
	err          error
}

func (p *disputeProviderStub) VerifyDisputeNotification(context.Context, string, map[string]string) (*payment.DisputeNotification, error) {
	p.calls++
	return p.notification, p.err
}

func TestHandleDisputeNotify_SkipsProvidersWithoutDisputeSupport(t *testing.T) {
	aware := &disputeProviderStub{plainProviderStub: plainProviderStub{key: payment.TypeStripe}}
	h := &PaymentWebhookHandler{}

	assert.NotPanics(t, func() {
		h.handleDisputeNotify(context.Background(),
			[]payment.Provider{&plainProviderStub{key: payment.TypeAlipay}, aware},
			"{}", map[string]string{})
	})
	// 不认争议的通道被跳过之后，循环必须走到认的那个。
	assert.Equal(t, 1, aware.calls)
}

// 一组同类型实例里只有一个持有对的 webhookSecret：前面的验签失败是**预期**，
// 停在那里等于让这条争议永远不被处理。
func TestHandleDisputeNotify_ContinuesPastVerifyFailure(t *testing.T) {
	failing := &disputeProviderStub{
		plainProviderStub: plainProviderStub{key: payment.TypeStripe},
		err:               errors.New("signature mismatch"),
	}
	succeeding := &disputeProviderStub{plainProviderStub: plainProviderStub{key: payment.TypeStripe}}
	h := &PaymentWebhookHandler{}

	h.handleDisputeNotify(context.Background(),
		[]payment.Provider{failing, succeeding}, "{}", map[string]string{})

	assert.Equal(t, 1, failing.calls)
	assert.Equal(t, 1, succeeding.calls, "验签失败必须继续试下一个实例")
}

// 验签通过但不是争议事件（或是询证）：这条通道就是对的那条，
// 不能再让别的实例把同一份报文解析一遍。
func TestHandleDisputeNotify_StopsAfterVerifiedNonDisputeEvent(t *testing.T) {
	first := &disputeProviderStub{plainProviderStub: plainProviderStub{key: payment.TypeStripe}}
	second := &disputeProviderStub{plainProviderStub: plainProviderStub{key: payment.TypeStripe}}
	h := &PaymentWebhookHandler{}

	h.handleDisputeNotify(context.Background(),
		[]payment.Provider{first, second}, "{}", map[string]string{})

	assert.Equal(t, 1, first.calls)
	assert.Zero(t, second.calls, "已经确定是哪条通道了，不该再解析一遍")
}

// 解析出一条真争议之后同样要停手。这条比"非争议事件停手"更要紧：
// 继续循环意味着同一个 dispute_id 被交给 service 两次，幂等闸挡得住扣钱，
// 但挡不住第二封给运营的信。
func TestHandleDisputeNotify_StopsAfterHandlingDispute(t *testing.T) {
	first := &disputeProviderStub{
		plainProviderStub: plainProviderStub{key: payment.TypeStripe},
		notification:      &payment.DisputeNotification{DisputeID: "dp_1", Status: payment.DisputeStatusOpen},
	}
	second := &disputeProviderStub{plainProviderStub: plainProviderStub{key: payment.TypeStripe}}
	h := &PaymentWebhookHandler{}

	h.handleDisputeNotify(context.Background(),
		[]payment.Provider{first, second}, "{}", map[string]string{})

	assert.Equal(t, 1, first.calls)
	assert.Zero(t, second.calls, "已经处理过的争议不该被第二个实例再解析一遍")
}

// 这条路径挂在 webhook 上，一次 panic 就是一次 502。两种 nil 都要安全：
// handler 自己是 nil，以及 paymentService 没装配。
func TestHandleDisputeNotify_NilIsSafe(t *testing.T) {
	notification := &payment.DisputeNotification{DisputeID: "dp_1", Status: payment.DisputeStatusOpen}

	skipped := &disputeProviderStub{
		plainProviderStub: plainProviderStub{key: payment.TypeStripe},
		notification:      notification,
	}
	var nilHandler *PaymentWebhookHandler
	require.NotPanics(t, func() {
		nilHandler.handleDisputeNotify(context.Background(), []payment.Provider{skipped}, "{}", map[string]string{})
	})
	assert.Zero(t, skipped.calls)

	// service 没装配时解析照跑——那一步没有依赖；真正被 nil 接住的是 service 内部。
	reached := &disputeProviderStub{
		plainProviderStub: plainProviderStub{key: payment.TypeStripe},
		notification:      notification,
	}
	h := &PaymentWebhookHandler{}
	require.NotPanics(t, func() {
		h.handleDisputeNotify(context.Background(), []payment.Provider{reached}, "{}", map[string]string{})
	})
	assert.Equal(t, 1, reached.calls)
}
