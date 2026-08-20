//go:build unit

// APEXONE-EXT: 双边市场——拒付处理的单元测试。
//
// 这里钉的是三条性质，每一条破了都直接对应一次钱算错：
//
//  1. **副作用只跑一次**。Stripe 对同一个争议会推很多次，而这条路径扣钱。
//     幂等有两道闸（record.SettledAt 与 ClaimForSettlement 的返回值），
//     两道都要有测试。
//  2. **换算的边界**。disputeBasisAmount 决定追回多少，它的每条分支都是钱。
//  3. **不认识的事件不产生副作用**。没有 dispute id、没装配台账、对不上订单，
//     三种情况都必须走到「留痕但不动钱」。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 替身 ---------------------------------------------------------------

type disputeStoreStub struct {
	upserts     []PaymentDisputeUpsert
	record      *PaymentDisputeRecord
	upsertErr   error
	claimed     bool
	claimErr    error
	claimCalls  []string
	settlements []PaymentDisputeSettlement
	settleErr   error
}

func (s *disputeStoreStub) Upsert(_ context.Context, params PaymentDisputeUpsert) (*PaymentDisputeRecord, error) {
	s.upserts = append(s.upserts, params)
	if s.upsertErr != nil {
		return nil, s.upsertErr
	}
	return s.record, nil
}

func (s *disputeStoreStub) ClaimForSettlement(_ context.Context, disputeID string) (bool, error) {
	s.claimCalls = append(s.claimCalls, disputeID)
	// 出错时**照样返回 claimed**，是刻意的：err != nil 时那个 bool 在真实实现里
	// 是未定义的，调用方不许读它。让替身在这里返回 true，正是为了让
	// 「出错却继续往下走」这种写法在测试里立刻现形。
	return s.claimed, s.claimErr
}

func (s *disputeStoreStub) RecordSettlement(_ context.Context, settlement PaymentDisputeSettlement) error {
	s.settlements = append(s.settlements, settlement)
	return s.settleErr
}

type disputeNotifierStub struct {
	notices []PaymentDisputeNotice
}

func (n *disputeNotifierStub) NotifyDispute(notice PaymentDisputeNotice) {
	n.notices = append(n.notices, notice)
}

// withPaymentDisputeStore / withPaymentDisputeNotifier 装配单例并在测试结束时还原。
// 单例是包级状态，不还原会让后面的测试读到上一个测试的替身。
func withPaymentDisputeStore(t *testing.T, store PaymentDisputeStore) {
	t.Helper()
	previous := loadPaymentDisputeStore()
	SetPaymentDisputeStore(store)
	t.Cleanup(func() { SetPaymentDisputeStore(previous) })
}

func withPaymentDisputeNotifier(t *testing.T, notifier PaymentDisputeNotifierPort) {
	t.Helper()
	previous := loadPaymentDisputeNotifier()
	SetPaymentDisputeNotifier(notifier)
	t.Cleanup(func() { SetPaymentDisputeNotifier(previous) })
}

func disputedOrder() *dbent.PaymentOrder {
	return &dbent.PaymentOrder{
		ID:         77,
		UserID:     9,
		UserEmail:  "consumer@example.com",
		OrderType:  payment.OrderTypeSubscription,
		OutTradeNo: "OUT-77",
		Amount:     100,
		PayAmount:  14,
	}
}

// --- 幂等：两道闸各一条 ---------------------------------------------------

// 第一道闸：Upsert 回来的行已经带 settled_at，连 claim 都不该发生。
// 这是重复推送的常态路径（Stripe 一个争议推五次，后四次都走这里）。
func TestApplyDisputeEffects_SkipsWhenAlreadySettled(t *testing.T) {
	settled := time.Now().Add(-time.Hour)
	store := &disputeStoreStub{claimed: true}
	clawback := &clawbackHandlerStub{result: &SupplierClawbackResult{ReversedCredit: 5}}
	withSupplierClawbackHandler(t, clawback)
	notifier := &disputeNotifierStub{}
	withPaymentDisputeNotifier(t, notifier)

	svc := &PaymentService{}
	svc.applyDisputeEffects(context.Background(), store,
		&payment.DisputeNotification{DisputeID: "dp_1", Status: payment.DisputeStatusOpen},
		payment.TypeStripe, disputedOrder(), 100,
		&PaymentDisputeRecord{DisputeID: "dp_1", SettledAt: &settled})

	assert.Empty(t, store.claimCalls, "已结算的争议不该再占坑")
	assert.Empty(t, clawback.calls, "已结算的争议不该再追回一次")
	assert.Empty(t, store.settlements)
	assert.Empty(t, notifier.notices, "重复推送不该重复打扰运营")
}

// 第二道闸：并发投递下 Upsert 各自读到 settled_at 为空，靠 claim 分出胜负。
// 输的那一方必须一分钱都不动。
func TestApplyDisputeEffects_SkipsWhenClaimLost(t *testing.T) {
	store := &disputeStoreStub{claimed: false}
	clawback := &clawbackHandlerStub{result: &SupplierClawbackResult{ReversedCredit: 5}}
	withSupplierClawbackHandler(t, clawback)
	notifier := &disputeNotifierStub{}
	withPaymentDisputeNotifier(t, notifier)

	svc := &PaymentService{}
	svc.applyDisputeEffects(context.Background(), store,
		&payment.DisputeNotification{DisputeID: "dp_2", Status: payment.DisputeStatusOpen},
		payment.TypeStripe, disputedOrder(), 100,
		&PaymentDisputeRecord{DisputeID: "dp_2"})

	require.Equal(t, []string{"dp_2"}, store.claimCalls)
	assert.Empty(t, clawback.calls, "没占到坑就不能追回")
	assert.Empty(t, store.settlements)
	assert.Empty(t, notifier.notices)
}

// 占坑失败（数据库抖动）同样必须跳过副作用：读不到闸的状态时，
// 「什么都不做」是唯一安全的选择——事后补一次追回是可以的，扣两次不行。
func TestApplyDisputeEffects_SkipsWhenClaimErrors(t *testing.T) {
	store := &disputeStoreStub{claimed: true, claimErr: errors.New("db down")}
	clawback := &clawbackHandlerStub{result: &SupplierClawbackResult{}}
	withSupplierClawbackHandler(t, clawback)

	svc := &PaymentService{}
	svc.applyDisputeEffects(context.Background(), store,
		&payment.DisputeNotification{DisputeID: "dp_3", Status: payment.DisputeStatusOpen},
		payment.TypeStripe, disputedOrder(), 100, nil)

	assert.Empty(t, clawback.calls)
	assert.Empty(t, store.settlements)
}

// --- 占到坑之后：追回、回填、通知 -----------------------------------------

func TestApplyDisputeEffects_ClawsBackAndRecords(t *testing.T) {
	store := &disputeStoreStub{claimed: true}
	clawback := &clawbackHandlerStub{result: &SupplierClawbackResult{
		ReversedCredit: 12.5, ReversedBasis: 80, UncoveredBasis: 20,
	}}
	withSupplierClawbackHandler(t, clawback)
	notifier := &disputeNotifierStub{}
	withPaymentDisputeNotifier(t, notifier)

	svc := &PaymentService{}
	order := disputedOrder()
	svc.applyDisputeEffects(context.Background(), store,
		&payment.DisputeNotification{DisputeID: "dp_4", Status: payment.DisputeStatusOpen, Reason: "fraudulent"},
		payment.TypeStripe, order, 100, nil)

	// 追回按 basis 走，理由串必须同时带争议号与订单号——
	// 同一个订单可能既被拒付又被退款，运营要能在流水里分清是哪一笔。
	require.Len(t, clawback.calls, 1)
	assert.Equal(t, int64(9), clawback.calls[0].consumerUserID)
	assert.InDelta(t, 100.0, clawback.calls[0].refundAmount, 1e-9)
	assert.Equal(t, "dispute dp_4 order:77", clawback.calls[0].reason)

	// 订阅订单不碰余额：撤天数那套在 webhook 上没法安全重试，走人工。
	require.Len(t, store.settlements, 1)
	assert.Zero(t, store.settlements[0].BalanceDeducted)
	assert.InDelta(t, 12.5, store.settlements[0].ClawedCredit, 1e-9)
	assert.InDelta(t, 80.0, store.settlements[0].ClawedBasis, 1e-9)
	assert.InDelta(t, 20.0, store.settlements[0].UncoveredBasis, 1e-9)

	require.Len(t, notifier.notices, 1)
	notice := notifier.notices[0]
	assert.Equal(t, "dp_4", notice.DisputeID)
	assert.Equal(t, int64(77), notice.OrderID)
	assert.Equal(t, "consumer@example.com", notice.UserEmail)
	require.NotNil(t, notice.Settlement)
	assert.InDelta(t, 20.0, notice.Settlement.UncoveredBasis, 1e-9)
}

// 基数为 0（对不上订单金额）时不追回。宁可不追，不可乱追：
// 一次基数错误的追回，收走的是供给者真实赚到的钱。
func TestApplyDisputeEffects_ZeroBasisSkipsClawback(t *testing.T) {
	store := &disputeStoreStub{claimed: true}
	clawback := &clawbackHandlerStub{result: &SupplierClawbackResult{ReversedCredit: 7}}
	withSupplierClawbackHandler(t, clawback)
	withPaymentDisputeNotifier(t, &disputeNotifierStub{})

	svc := &PaymentService{}
	svc.applyDisputeEffects(context.Background(), store,
		&payment.DisputeNotification{DisputeID: "dp_5", Status: payment.DisputeStatusOpen},
		payment.TypeStripe, disputedOrder(), 0, nil)

	assert.Empty(t, clawback.calls)
	require.Len(t, store.settlements, 1)
	assert.Zero(t, store.settlements[0].ClawedCredit)
}

// 追回本身失败不能中断流程：坑已经占了，账必须回填，运营必须收到信。
func TestApplyDisputeEffects_ClawbackErrorStillRecordsAndNotifies(t *testing.T) {
	store := &disputeStoreStub{claimed: true}
	withSupplierClawbackHandler(t, &clawbackHandlerStub{err: errors.New("credit repo down")})
	notifier := &disputeNotifierStub{}
	withPaymentDisputeNotifier(t, notifier)

	svc := &PaymentService{}
	svc.applyDisputeEffects(context.Background(), store,
		&payment.DisputeNotification{DisputeID: "dp_6", Status: payment.DisputeStatusOpen},
		payment.TypeStripe, disputedOrder(), 100, nil)

	require.Len(t, store.settlements, 1)
	assert.Zero(t, store.settlements[0].ClawedCredit)
	require.Len(t, notifier.notices, 1)
}

// --- 入口的三条「留痕但不动钱」路径 ---------------------------------------

func TestHandleDisputeNotification_NoDisputeIDDoesNothing(t *testing.T) {
	store := &disputeStoreStub{claimed: true}
	withPaymentDisputeStore(t, store)

	svc := &PaymentService{}
	svc.HandleDisputeNotification(context.Background(),
		&payment.DisputeNotification{DisputeID: "  ", Status: payment.DisputeStatusOpen}, payment.TypeStripe)

	assert.Empty(t, store.upserts, "没有幂等键的事件不该落库")
	assert.Empty(t, store.claimCalls)
}

// 台账没装配（wire 剪掉了、或供给侧没编进去）时：一行都不写，但也绝不 panic。
func TestHandleDisputeNotification_NoStoreIsSafe(t *testing.T) {
	withPaymentDisputeStore(t, nil)
	notifier := &disputeNotifierStub{}
	withPaymentDisputeNotifier(t, notifier)

	svc := &PaymentService{}
	assert.NotPanics(t, func() {
		svc.HandleDisputeNotification(context.Background(),
			&payment.DisputeNotification{DisputeID: "dp_7", Status: payment.DisputeStatusOpen}, payment.TypeStripe)
	})
	assert.Empty(t, notifier.notices)
}

// 对不上订单：行要落库、运营要收到信，但一分钱都不能动。
// 多套环境共用一个 Stripe 账户时这是常态，不是故障。
func TestHandleDisputeNotification_NoOrderPersistsAndNotifiesOnly(t *testing.T) {
	store := &disputeStoreStub{claimed: true}
	withPaymentDisputeStore(t, store)
	clawback := &clawbackHandlerStub{result: &SupplierClawbackResult{ReversedCredit: 3}}
	withSupplierClawbackHandler(t, clawback)
	notifier := &disputeNotifierStub{}
	withPaymentDisputeNotifier(t, notifier)

	// entClient 为 nil ⇒ findDisputedOrder 必然返回 nil，正是"对不上订单"。
	svc := &PaymentService{}
	svc.HandleDisputeNotification(context.Background(), &payment.DisputeNotification{
		DisputeID: "dp_8", Status: payment.DisputeStatusOpen,
		TradeNo: "pi_x", DisputeAmount: 14, Currency: "USD",
	}, payment.TypeStripe)

	require.Len(t, store.upserts, 1)
	assert.Equal(t, "dp_8", store.upserts[0].DisputeID)
	assert.True(t, store.upserts[0].OrderID == nil)
	assert.True(t, store.upserts[0].UserID == nil)
	assert.Zero(t, store.upserts[0].BasisAmount, "没有订单就没有基数")

	assert.Empty(t, store.claimCalls, "对不上订单不该占坑")
	assert.Empty(t, clawback.calls)
	require.Len(t, notifier.notices, 1)
	assert.Zero(t, notifier.notices[0].OrderID)
	assert.True(t, notifier.notices[0].Settlement == nil)
}

// 落库失败就此打住：台账是幂等的唯一依据，写不进去就无法保证只扣一次。
func TestHandleDisputeNotification_UpsertErrorStopsEverything(t *testing.T) {
	store := &disputeStoreStub{upsertErr: errors.New("db down"), claimed: true}
	withPaymentDisputeStore(t, store)
	clawback := &clawbackHandlerStub{result: &SupplierClawbackResult{}}
	withSupplierClawbackHandler(t, clawback)
	notifier := &disputeNotifierStub{}
	withPaymentDisputeNotifier(t, notifier)

	svc := &PaymentService{}
	svc.HandleDisputeNotification(context.Background(),
		&payment.DisputeNotification{DisputeID: "dp_9", Status: payment.DisputeStatusOpen}, payment.TypeStripe)

	assert.Empty(t, store.claimCalls)
	assert.Empty(t, clawback.calls)
	assert.Empty(t, notifier.notices)
}

// --- 换算 -----------------------------------------------------------------

func TestDisputeBasisAmount(t *testing.T) {
	// 订单没有 provider 快照时 PaymentOrderCurrency 回落到默认币种，
	// 容差因此走默认那一档——这正是绝大多数订单的形态。
	order := func(amount, payAmount float64) *dbent.PaymentOrder {
		return &dbent.PaymentOrder{Amount: amount, PayAmount: payAmount}
	}
	cases := []struct {
		name   string
		order  *dbent.PaymentOrder
		notify *payment.DisputeNotification
		want   float64
	}{
		{"对不上订单返回零", nil, &payment.DisputeNotification{DisputeAmount: 10}, 0},
		{"订单金额不可用返回零", order(0, 14), &payment.DisputeNotification{DisputeAmount: 14}, 0},
		{"整笔被拒直接取订单金额", order(100, 14), &payment.DisputeNotification{DisputeAmount: 14}, 100},
		{"拿不到争议金额按整笔算", order(100, 14), &payment.DisputeNotification{DisputeAmount: 0}, 100},
		{"拿不到实付额按整笔算", order(100, 0), &payment.DisputeNotification{DisputeAmount: 14}, 100},
		// 通道回来的金额与实付额差一点点（分位舍入）时仍按整笔算：
		// 不短路的话，最常见的那条路径会被除法带进尾差。
		{"容差内的差额仍按整笔算", order(100, 14), &payment.DisputeNotification{DisputeAmount: 13.995}, 100},
		{"负数订单金额返回零", order(-5, 14), &payment.DisputeNotification{DisputeAmount: 14}, 0},
		{"部分争议按比例折算", order(100, 20), &payment.DisputeNotification{DisputeAmount: 5}, 25},
		// 汇率浮动 / 通道部分退款都可能让争议金额略大于实付额。
		// 夹住是必须的：不夹就会追回超过这笔订单本身产生的分成。
		{"争议金额大于实付额夹在订单金额", order(100, 10), &payment.DisputeNotification{DisputeAmount: 30}, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.want, disputeBasisAmount(tc.order, tc.notify), 1e-9)
		})
	}
}

func TestDisputeAuditActionAndClawbackReason(t *testing.T) {
	assert.Equal(t, "DISPUTE_WON", disputeAuditAction(payment.DisputeStatusWon))
	assert.Equal(t, "DISPUTE_LOST", disputeAuditAction(payment.DisputeStatusLost))
	assert.Equal(t, "DISPUTE_OPENED", disputeAuditAction(payment.DisputeStatusOpen))
	assert.Equal(t, "DISPUTE_OPENED", disputeAuditAction("something-new-from-stripe"))

	assert.Equal(t, "dispute dp_1 order:5", disputeClawbackReason("dp_1", 5))
	assert.Equal(t, "dispute dp_1", disputeClawbackReason("dp_1", 0))
}

// buildDisputeUpsert 在报文没带商户订单号时要用订单自己的补上——
// 那一列是运营对账时唯一能和支付后台对上的字段。
func TestBuildDisputeUpsert_FillsFromOrder(t *testing.T) {
	params := buildDisputeUpsert(&payment.DisputeNotification{
		DisputeID: " dp_10 ", TradeNo: " pi_1 ", Status: payment.DisputeStatusLost,
	}, payment.TypeStripe, disputedOrder(), 100)

	assert.Equal(t, "dp_10", params.DisputeID)
	assert.Equal(t, "pi_1", params.TradeNo)
	assert.Equal(t, "OUT-77", params.OutTradeNo)
	require.NotNil(t, params.OrderID)
	assert.Equal(t, int64(77), *params.OrderID)
	require.NotNil(t, params.UserID)
	assert.Equal(t, int64(9), *params.UserID)
	assert.InDelta(t, 100.0, params.BasisAmount, 1e-9)
}
