//go:build unit

// APEXONE-EXT: 双边市场——拒付追回接缝的单元测试。
//
// 这里测的不是"追回算得对不对"（那在 repository 层，对着 SQL 测），
// 而是**退款路径上那一行永远不会成为一种新的失败方式**——
// 这条性质一旦破了，一次供给侧的数据库抖动就能把已经在支付通道成功的退款卡住。
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withSupplierClawbackHandler 装配一个追回入口并在测试结束时还原。
// 单例是包级状态，不还原会让后面的测试读到上一个测试的 handler。
func withSupplierClawbackHandler(t *testing.T, handler SupplierClawbackHandler) {
	t.Helper()
	previous := loadSupplierClawbackHandler()
	SetSupplierClawbackHandler(handler)
	t.Cleanup(func() { SetSupplierClawbackHandler(previous) })
}

type clawbackHandlerStub struct {
	calls  []clawbackHandlerCall
	result *SupplierClawbackResult
	err    error
}

type clawbackHandlerCall struct {
	consumerUserID int64
	refundAmount   float64
	reason         string
}

func (s *clawbackHandlerStub) ClawbackForRefund(_ context.Context, consumerUserID int64, refundAmount float64, reason string) (*SupplierClawbackResult, error) {
	s.calls = append(s.calls, clawbackHandlerCall{consumerUserID, refundAmount, reason})
	return s.result, s.err
}

// 幸福路径：退款金额原样当基数传下去。
func TestClawbackSupplierCreditOnRefundForwardsRefundAmountAsBasis(t *testing.T) {
	handler := &clawbackHandlerStub{result: &SupplierClawbackResult{Entries: 2, ReversedCredit: 8.4}}
	withSupplierClawbackHandler(t, handler)

	clawbackSupplierCreditOnRefund(context.Background(), 9, 12.5, "refund order:77")

	require.Len(t, handler.calls, 1)
	assert.Equal(t, int64(9), handler.calls[0].consumerUserID)
	assert.InDelta(t, 12.5, handler.calls[0].refundAmount, 1e-9)
	assert.Equal(t, "refund order:77", handler.calls[0].reason)
}

// 追回报错必须被吞掉：走到这一行时钱已经在支付通道退出去了，
// 让错误往上冒只会把一笔成功的退款回滚成"钱退了、系统说没退"。
func TestClawbackSupplierCreditOnRefundSwallowsHandlerError(t *testing.T) {
	handler := &clawbackHandlerStub{err: errors.New("database is on fire")}
	withSupplierClawbackHandler(t, handler)

	assert.NotPanics(t, func() {
		clawbackSupplierCreditOnRefund(context.Background(), 9, 12.5, "refund order:77")
	})
	require.Len(t, handler.calls, 1)
}

// 没装配追回入口（单测、或供给侧整个没编进去）时静默返回，不 panic。
func TestClawbackSupplierCreditOnRefundNoopWithoutHandler(t *testing.T) {
	withSupplierClawbackHandler(t, nil)

	assert.NotPanics(t, func() {
		clawbackSupplierCreditOnRefund(context.Background(), 9, 12.5, "refund order:77")
	})
}

// 无消费者 / 零金额 / 负金额一律不调用 handler：
// 订阅赠送、零元订单这类退款不该在供给侧留下任何痕迹。
func TestClawbackSupplierCreditOnRefundSkipsMeaninglessInput(t *testing.T) {
	cases := []struct {
		name           string
		consumerUserID int64
		refundAmount   float64
	}{
		{"no consumer", 0, 12.5},
		{"zero amount", 9, 0},
		{"negative amount", 9, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := &clawbackHandlerStub{}
			withSupplierClawbackHandler(t, handler)

			clawbackSupplierCreditOnRefund(context.Background(), tc.consumerUserID, tc.refundAmount, "refund order:77")
			assert.Empty(t, handler.calls)
		})
	}
}

// ---------------------------------------------------------------------------
// SupplierClawbackService
// ---------------------------------------------------------------------------

func TestSupplierClawbackServicePassesRefundAmountAsBasis(t *testing.T) {
	repo := &supplierCreditRepoStub{clawbackResult: &SupplierClawbackResult{Entries: 1}}
	svc := NewSupplierClawbackService(repo)

	result, err := svc.ClawbackForRefund(context.Background(), 9, 12.5, "refund order:77")
	require.NoError(t, err)
	require.Equal(t, 1, result.Entries)

	calls := repo.snapshotClawbackCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, int64(9), calls[0].ConsumerUserID)
	assert.InDelta(t, 12.5, calls[0].BasisAmount, 1e-9)
	assert.Equal(t, "refund order:77", calls[0].Reason)
	// MaxEntries 留零，由仓储层套默认上限——上限属于"锁多少行"的实现细节，
	// 编排层替它拍一个数只会让两处各有一份真相。
	assert.Zero(t, calls[0].MaxEntries)
}

// 边界输入在编排层就短路，不打库。
func TestSupplierClawbackServiceSkipsMeaninglessInput(t *testing.T) {
	repo := &supplierCreditRepoStub{}
	svc := NewSupplierClawbackService(repo)

	for _, tc := range []struct {
		name           string
		consumerUserID int64
		refundAmount   float64
	}{
		{"no consumer", 0, 1},
		{"zero amount", 9, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := svc.ClawbackForRefund(context.Background(), tc.consumerUserID, tc.refundAmount, "r")
			require.NoError(t, err)
			assert.Nil(t, result)
		})
	}
	assert.Empty(t, repo.snapshotClawbackCalls())
}

// 追回不看结算总开关：关掉开关之后，冻结区里仍躺着开着时产生的入账，
// 那些钱照样该在拒付时被追回。按 enabled 短路等于给"先关开关再退款"开一条套利路径。
func TestSupplierClawbackServiceIgnoresSettlementToggle(t *testing.T) {
	repo := &supplierCreditRepoStub{clawbackResult: &SupplierClawbackResult{Entries: 1}}
	svc := NewSupplierClawbackService(repo)

	// 构造里根本没有 SettingService 的位置——这条性质由类型钉死，不是由分支钉死。
	_, err := svc.ClawbackForRefund(context.Background(), 9, 1.0, "r")
	require.NoError(t, err)
	require.Len(t, repo.snapshotClawbackCalls(), 1)
}

// 两条退款收敛路径写进 remark 的字符串必须逐字相同，
// 否则运营按订单号搜流水时只能搜到其中一条。
func TestRefundClawbackReasonFormatIsStable(t *testing.T) {
	assert.Equal(t, "refund order:77", refundClawbackReason(77))
}
