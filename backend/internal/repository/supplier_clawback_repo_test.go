// APEXONE-EXT: 双边市场——拒付追回的 SQL 层单测。
//
// 复用 supplier_credit_repo_test.go 里的 newSupplierCreditMock / normalizeSQL / int64Ptr，
// 刻意不另起一套夹具：两边测的是同一张表上的同一组不变量。
package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 候选筛选是整个追回功能的正确性所在：多筛一条就是从供给者手里拿走不该拿的钱，
// 少筛一条就是平台自吃本可追回的亏损。四个条件逐条钉死。
func TestSupplierClawbackCandidatesSQLOnlyTouchesReversibleAccruals(t *testing.T) {
	normalized := normalizeSQL(supplierCreditClawbackCandidatesSQL)

	// 只撤入账，不撤 spend/thaw。
	require.Contains(t, normalized, "WHERE a.action = 'accrue'")
	// 只找这个消费者产生的入账——供给者从别人那里赚的钱与本次退款无关。
	require.Contains(t, normalized, "a.source_user_id = $1")
	// 只动冻结区。这是不变量 2 的实现：已解冻 = 拒付安全。
	require.Contains(t, normalized, "a.frozen_until IS NOT NULL")
	// 一条入账最多撤一次。
	require.Contains(t, normalized,
		"NOT EXISTS ( SELECT 1 FROM supplier_credit_ledger c WHERE c.action = 'clawback' AND c.request_id = a.request_id )")
	// 锁住候选，挡住并发退款与后台解冻任务。
	require.Contains(t, normalized, "FOR UPDATE OF a")
	// 先撤最近的：越近越可能还在冻结区，UncoveredBasis 因此最小。
	require.Contains(t, normalized, "ORDER BY a.id DESC LIMIT $2")
}

// 摘出冻结队列这一步漏了的话，解冻任务会把已经扣走的钱再往可用区搬一次，
// 供给者的可用余额凭空变多——比"没追回"更糟。
func TestSupplierClawbackMarkSQLRemovesRowFromThawQueue(t *testing.T) {
	normalized := normalizeSQL(supplierCreditClawbackMarkSQL)
	require.Contains(t, normalized, "SET frozen_until = NULL")
	require.Contains(t, normalized, "WHERE id = $1 AND action = 'accrue' AND frozen_until IS NOT NULL")
}

// 追回只减冻结区，绝不碰 history_credit：
// history_credit = SUM(accrue.amount) 是全部对账的锚点，减了它历史就无法从流水重算。
func TestSupplierClawbackWalletSQLLeavesHistoryUntouched(t *testing.T) {
	normalized := normalizeSQL(supplierCreditClawbackWalletSQL)
	require.Contains(t, normalized, "SET frozen_credit = GREATEST(frozen_credit - $2, 0)")
	require.NotContains(t, normalized, "SET history_credit")
	require.NotContains(t, normalized, "history_credit =")
	// 可用区也不动：追回的是还没释放的钱，动可用区等于追已经释放的钱。
	require.NotContains(t, normalized, "available_credit =")
}

// 幸福路径：撤两条入账凑够基数，每条走完「流水 → 摘队列 → 扣钱 → 回填快照」。
func TestClawbackSupplierCreditTxReversesUntilBasisCovered(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	// 两条候选，基数 6 + 6；退款 10 只需要吃掉两条（第一条不够，第二条跨过线）。
	mock.ExpectQuery(regexp.QuoteMeta("FROM supplier_credit_ledger a")).
		WithArgs(int64(9), 500).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "request_id", "account_id", "amount", "basis_amount", "share_ratio"}).
			AddRow(int64(1002), int64(7), "req-b", int64(42), 4.2, 6.0, 0.7).
			AddRow(int64(1001), int64(8), "req-a", nil, 4.2, 6.0, 0.7))

	// 第一条：供给者 7。
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WithArgs(int64(7), service.SupplierCreditActionClawback, 4.2, "req-b", int64(42), int64(9), 6.0, 0.7, int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(5001)))
	mock.ExpectExec(regexp.QuoteMeta("SET frozen_until = NULL")).
		WithArgs(int64(1002), "refund order:77").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SET frozen_credit = GREATEST")).
		WithArgs(int64(7), 4.2).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit", "frozen_credit", "history_credit"}).
			AddRow(0.0, 0.0, 4.2))
	mock.ExpectExec(regexp.QuoteMeta("SET available_after")).
		WithArgs(int64(5001), 0.0, 0.0, 4.2).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("SET remark = $2")).
		WithArgs(int64(5001), "refund order:77").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 第二条：另一个供给者 8，account_id 为 NULL。
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WithArgs(int64(8), service.SupplierCreditActionClawback, 4.2, "req-a", nil, int64(9), 6.0, 0.7, int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(5002)))
	mock.ExpectExec(regexp.QuoteMeta("SET frozen_until = NULL")).
		WithArgs(int64(1001), "refund order:77").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SET frozen_credit = GREATEST")).
		WithArgs(int64(8), 4.2).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit", "frozen_credit", "history_credit"}).
			AddRow(1.0, 0.0, 4.2))
	mock.ExpectExec(regexp.QuoteMeta("SET available_after")).
		WithArgs(int64(5002), 1.0, 0.0, 4.2).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("SET remark = $2")).
		WithArgs(int64(5002), "refund order:77").
		WillReturnResult(sqlmock.NewResult(0, 1))

	result, err := clawbackSupplierCreditTx(context.Background(), client, service.SupplierClawbackParams{
		ConsumerUserID: 9,
		BasisAmount:    10.0,
		Reason:         "refund order:77",
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.Entries)
	require.Equal(t, 2, result.Suppliers)
	require.InDelta(t, 8.4, result.ReversedCredit, 1e-9)
	require.InDelta(t, 12.0, result.ReversedBasis, 1e-9)
	// 撤到 12 > 退款 10：整条入账为单位撤销的必然结果，超额如实反映在 ReversedBasis 上。
	require.Zero(t, result.UncoveredBasis)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 够了就停：基数覆盖之后剩下的候选一条都不能碰，
// 否则一次小额退款会把这个消费者名下全部入账清空。
func TestClawbackSupplierCreditTxStopsOnceBasisIsCovered(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM supplier_credit_ledger a")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "request_id", "account_id", "amount", "basis_amount", "share_ratio"}).
			AddRow(int64(1002), int64(7), "req-b", nil, 7.0, 10.0, 0.7).
			AddRow(int64(1001), int64(7), "req-a", nil, 7.0, 10.0, 0.7))

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(5001)))
	mock.ExpectExec(regexp.QuoteMeta("SET frozen_until = NULL")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SET frozen_credit = GREATEST")).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit", "frozen_credit", "history_credit"}).
			AddRow(0.0, 7.0, 14.0))
	mock.ExpectExec(regexp.QuoteMeta("SET available_after")).
		WillReturnResult(sqlmock.NewResult(0, 1))

	result, err := clawbackSupplierCreditTx(context.Background(), client, service.SupplierClawbackParams{
		ConsumerUserID: 9,
		BasisAmount:    10.0,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Entries)
	require.Equal(t, 1, result.Suppliers)
	// Reason 为空时不发 remark 语句——ExpectationsWereMet 顺带守住这一点。
	require.NoError(t, mock.ExpectationsWereMet())
}

// 冻结区不够覆盖退款：差额必须如实报出来。
// 它是"冻结窗配短了"的唯一量化证据，被吞掉的话这笔亏损就无声无息。
func TestClawbackSupplierCreditTxReportsUncoveredBasis(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM supplier_credit_ledger a")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "request_id", "account_id", "amount", "basis_amount", "share_ratio"}))

	result, err := clawbackSupplierCreditTx(context.Background(), client, service.SupplierClawbackParams{
		ConsumerUserID: 9,
		BasisAmount:    10.0,
	})
	require.NoError(t, err)
	require.Zero(t, result.Entries)
	require.InDelta(t, 10.0, result.UncoveredBasis, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 重放：流水插不进去 = 这条已经被撤过了。此时钱包一分不能动，也不能计进结果。
// 少了这道闸门，重跑一次追回就会把同一笔分成收两遍。
func TestClawbackSupplierCreditTxIsIdempotentPerAccrual(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM supplier_credit_ledger a")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "request_id", "account_id", "amount", "basis_amount", "share_ratio"}).
			AddRow(int64(1001), int64(7), "req-a", nil, 4.2, 6.0, 0.7))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	result, err := clawbackSupplierCreditTx(context.Background(), client, service.SupplierClawbackParams{
		ConsumerUserID: 9,
		BasisAmount:    10.0,
	})
	require.NoError(t, err)
	require.Zero(t, result.Entries)
	require.InDelta(t, 10.0, result.UncoveredBasis, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 摘不掉冻结标记（并发绕过了行锁）必须整笔失败而不是继续扣钱：
// 继续下去会留下一条既被扣过、又还排在解冻队列里的入账。
func TestClawbackSupplierCreditTxFailsWhenAccrualCannotBeMarked(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM supplier_credit_ledger a")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "request_id", "account_id", "amount", "basis_amount", "share_ratio"}).
			AddRow(int64(1001), int64(7), "req-a", nil, 4.2, 6.0, 0.7))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(5001)))
	mock.ExpectExec(regexp.QuoteMeta("SET frozen_until = NULL")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	_, err := clawbackSupplierCreditTx(context.Background(), client, service.SupplierClawbackParams{
		ConsumerUserID: 9,
		BasisAmount:    10.0,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "changed during clawback")
	require.NoError(t, mock.ExpectationsWereMet())
}

// 钱包行缺失说明流水与余额已经对不上，不能假装追回成功。
func TestClawbackSupplierCreditTxFailsWhenWalletMissing(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM supplier_credit_ledger a")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "request_id", "account_id", "amount", "basis_amount", "share_ratio"}).
			AddRow(int64(1001), int64(7), "req-a", nil, 4.2, 6.0, 0.7))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(5001)))
	mock.ExpectExec(regexp.QuoteMeta("SET frozen_until = NULL")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SET frozen_credit = GREATEST")).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit", "frozen_credit", "history_credit"}))

	_, err := clawbackSupplierCreditTx(context.Background(), client, service.SupplierClawbackParams{
		ConsumerUserID: 9,
		BasisAmount:    10.0,
	})
	require.ErrorIs(t, err, service.ErrSupplierWalletNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 无消费者 / 无金额一律静默跳过且一条 SQL 都不发：
// 退款路径不能因为供给侧的边界情况多出一次数据库往返。
func TestClawbackSupplierCreditTxSkipsInvalidInputWithoutSQL(t *testing.T) {
	cases := []struct {
		name   string
		params service.SupplierClawbackParams
	}{
		{"no consumer", service.SupplierClawbackParams{BasisAmount: 1}},
		{"zero amount", service.SupplierClawbackParams{ConsumerUserID: 9}},
		{"negative amount", service.SupplierClawbackParams{ConsumerUserID: 9, BasisAmount: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, mock := newSupplierCreditMock(t)
			result, err := clawbackSupplierCreditTx(context.Background(), client, tc.params)
			require.NoError(t, err)
			require.Zero(t, result.Entries)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// MaxEntries 是候选查询的 LIMIT。不传时必须落到默认上限，
// 传 0 走成 LIMIT 0 的话，追回会静默地什么都不做。
func TestClawbackSupplierCreditTxAppliesDefaultEntryLimit(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM supplier_credit_ledger a")).
		WithArgs(int64(9), supplierCreditClawbackDefaultMaxEntries).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "request_id", "account_id", "amount", "basis_amount", "share_ratio"}))

	_, err := clawbackSupplierCreditTx(context.Background(), client, service.SupplierClawbackParams{
		ConsumerUserID: 9,
		BasisAmount:    1.0,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// remark 按字符截断。按字节截会把中文理由切出半个字，落库就是乱码。
func TestSupplierClawbackRemarkTruncatesByRune(t *testing.T) {
	require.Equal(t, "refund order:77", supplierClawbackRemark("  refund order:77 "))
	require.Empty(t, supplierClawbackRemark("   "))

	long := strings.Repeat("退款", 300)
	truncated := supplierClawbackRemark(long)
	require.Equal(t, supplierClawbackRemarkMaxLen, len([]rune(truncated)))
	require.True(t, utf8ValidString(truncated))
}

func utf8ValidString(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
