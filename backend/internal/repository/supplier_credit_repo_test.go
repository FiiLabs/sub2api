package repository

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func newSupplierCreditMock(t *testing.T) (*dbent.Client, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return client, mock
}

func normalizeSQL(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func int64Ptr(v int64) *int64 { return &v }

// 幂等闸门是「消耗 === 入账」不变量的最后一道防线，而它只有在 ON CONFLICT 的推断子句
// 与迁移里的部分唯一索引逐字一致时才生效——不一致时 Postgres 不是静默降级，而是直接
// 报错，会把整个计费事务带崩。所以这条一致性用测试钉死，改一边必然红。
func TestSupplierCreditLedgerIdempotencyClauseMatchesMigrationIndex(t *testing.T) {
	migration, err := os.ReadFile("../../migrations/225_supplier_credit_wallet.sql")
	require.NoError(t, err)

	require.Contains(t, normalizeSQL(string(migration)),
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_supplier_credit_ledger_request_action_uniq "+
			"ON supplier_credit_ledger (action, request_id) WHERE request_id IS NOT NULL")
	require.Contains(t, normalizeSQL(supplierCreditLedgerInsertSQL),
		"ON CONFLICT (action, request_id) WHERE request_id IS NOT NULL DO NOTHING")
	// 冲突时必须拿不到行——「没有 id」就是「已经记过账」的信号。
	require.Contains(t, normalizeSQL(supplierCreditLedgerInsertSQL), "DO NOTHING RETURNING id")
}

// 扣款语句自己带余额守卫：即便调用方漏了检查，数据库也不允许把可用额扣成负数。
func TestSupplierCreditSpendSQLNeverGoesNegative(t *testing.T) {
	require.Contains(t, normalizeSQL(supplierCreditSpendSQL), "WHERE user_id = $1 AND available_credit >= $2")
}

// 解冻只搬已到期的行，且更新与汇总在同一条语句里，避免并发任务重复搬运。
func TestSupplierCreditThawSQLOnlyTouchesMaturedRows(t *testing.T) {
	normalized := normalizeSQL(supplierCreditThawMaturedSQL)
	require.Contains(t, normalized, "frozen_until IS NOT NULL AND frozen_until <= NOW()")
	require.Contains(t, normalized, "SET frozen_until = NULL")
	require.Contains(t, normalized, "SELECT COALESCE(SUM(amount), 0)::double precision FROM matured")
}

func TestAccrueSupplierCreditTxFreezesAndSnapshots(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	// 基数 2.0 × 比例 0.5 = 1.0，金额由服务端现算而非调用方传入。
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WithArgs(int64(7), service.SupplierCreditActionAccrue, 1.0, "req-1", int64(42), int64(9), 2.0, 0.5, int64(168)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1001)))
	// 冻结入账：可用区加 0，冻结区加全额，累计入账加全额。
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credits")).
		WithArgs(int64(7), 0.0, 1.0, 1.0).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit", "frozen_credit", "history_credit"}).
			AddRow(0.0, 3.5, 3.5))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE supplier_credit_ledger")).
		WithArgs(int64(1001), 0.0, 3.5, 3.5).
		WillReturnResult(sqlmock.NewResult(0, 1))

	applied, err := accrueSupplierCreditTx(context.Background(), client, service.SupplierAccrueParams{
		SupplierUserID: 7,
		RequestID:      "req-1",
		AccountID:      int64Ptr(42),
		ConsumerUserID: int64Ptr(9),
		BasisAmount:    2.0,
		ShareRatio:     0.5,
		FreezeHours:    168,
	})
	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 计费事务可能因为重试、崩溃补偿被重放。重放时闸门挡在余额之前：
// 流水插不进去，钱包一分不动。
func TestAccrueSupplierCreditTxIsIdempotentOnRequestID(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	applied, err := accrueSupplierCreditTx(context.Background(), client, service.SupplierAccrueParams{
		SupplierUserID: 7,
		RequestID:      "req-1",
		BasisAmount:    2.0,
		ShareRatio:     0.5,
		FreezeHours:    168,
	})
	require.NoError(t, err)
	require.False(t, applied)
	// ExpectationsWereMet 同时保证没有多余的余额写入发生。
	require.NoError(t, mock.ExpectationsWereMet())
}

// 缺幂等键 / 无供给者 / 金额非正，一律静默跳过且一条 SQL 都不发：
// 计费主链路不能因为供给侧的边界情况而变慢或失败。
func TestAccrueSupplierCreditTxSkipsInvalidInputWithoutSQL(t *testing.T) {
	cases := []struct {
		name   string
		params service.SupplierAccrueParams
	}{
		{"no supplier", service.SupplierAccrueParams{RequestID: "r", BasisAmount: 1, ShareRatio: 0.5}},
		{"blank request id", service.SupplierAccrueParams{SupplierUserID: 7, RequestID: "  ", BasisAmount: 1, ShareRatio: 0.5}},
		{"zero ratio", service.SupplierAccrueParams{SupplierUserID: 7, RequestID: "r", BasisAmount: 1, ShareRatio: 0}},
		{"zero basis", service.SupplierAccrueParams{SupplierUserID: 7, RequestID: "r", BasisAmount: 0, ShareRatio: 0.5}},
		{"negative basis", service.SupplierAccrueParams{SupplierUserID: 7, RequestID: "r", BasisAmount: -1, ShareRatio: 0.5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, mock := newSupplierCreditMock(t)
			applied, err := accrueSupplierCreditTx(context.Background(), client, tc.params)
			require.NoError(t, err)
			require.False(t, applied)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSpendSupplierCreditTxDeductsAndSnapshots(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FOR UPDATE")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit"}).AddRow(5.0))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WithArgs(int64(7), service.SupplierCreditActionSpend, 1.25, "req-2", nil, nil, nil, nil, int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(2002)))
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE supplier_credits")).
		WithArgs(int64(7), 1.25).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit", "frozen_credit", "history_credit"}).
			AddRow(3.75, 0.0, 5.0))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE supplier_credit_ledger")).
		WithArgs(int64(2002), 3.75, 0.0, 5.0).
		WillReturnResult(sqlmock.NewResult(0, 1))

	applied, err := spendSupplierCreditTx(context.Background(), client, 7, 1.25, "req-2")
	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 余额不足不是错误：调用方据此回退到 users.balance。
// 关键是此时一条流水都不能落，否则账面上会凭空多出一笔没发生的消费。
func TestSpendSupplierCreditTxFallsBackWhenBalanceInsufficient(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FOR UPDATE")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit"}).AddRow(0.4))

	applied, err := spendSupplierCreditTx(context.Background(), client, 7, 1.0, "req-3")
	require.NoError(t, err)
	require.False(t, applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 钱包行不存在（从没入过账的用户）同样走回退，而不是报错。
func TestSpendSupplierCreditTxFallsBackWhenWalletMissing(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FOR UPDATE")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit"}))

	applied, err := spendSupplierCreditTx(context.Background(), client, 7, 1.0, "req-4")
	require.NoError(t, err)
	require.False(t, applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 重放已扣过的请求：返回 true（"这笔已经扣掉了"），但不再扣第二次。
// 返回 true 而不是 false 很重要——false 会让计费侧转头去扣 users.balance，
// 变成同一个请求扣两处。
func TestSpendSupplierCreditTxIsIdempotentOnRequestID(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FOR UPDATE")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit"}).AddRow(5.0))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	applied, err := spendSupplierCreditTx(context.Background(), client, 7, 1.0, "req-2")
	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestThawSupplierCreditTxMovesFrozenAndWritesLedger(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("WITH matured AS")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(2.5))
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE supplier_credits")).
		WithArgs(int64(7), 2.5).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit", "frozen_credit", "history_credit"}).
			AddRow(2.5, 0.0, 2.5))
	// thaw 流水没有 request_id：它是钱包内部搬运，不是一次消耗。
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WithArgs(int64(7), service.SupplierCreditActionThaw, 2.5, nil, nil, nil, nil, nil, int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(3003)))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE supplier_credit_ledger")).
		WithArgs(int64(3003), 2.5, 0.0, 2.5).
		WillReturnResult(sqlmock.NewResult(0, 1))

	thawed, err := thawSupplierCreditTx(context.Background(), client, 7)
	require.NoError(t, err)
	require.InDelta(t, 2.5, thawed, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 没有到期的冻结额时直接收工，不写任何流水。
func TestThawSupplierCreditTxNoopWhenNothingMatured(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("WITH matured AS")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(0.0))

	thawed, err := thawSupplierCreditTx(context.Background(), client, 7)
	require.NoError(t, err)
	require.Zero(t, thawed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildSupplierLedgerWhereNumbersPlaceholdersInOrder(t *testing.T) {
	accountID := int64(42)
	where, args := buildSupplierLedgerWhere(service.SupplierCreditLedgerFilter{
		UserID:    7,
		Action:    service.SupplierCreditActionAccrue,
		AccountID: &accountID,
	})
	require.Equal(t, " WHERE user_id = $1 AND action = $2 AND account_id = $3", where)
	require.Equal(t, []any{int64(7), service.SupplierCreditActionAccrue, int64(42)}, args)

	where, args = buildSupplierLedgerWhere(service.SupplierCreditLedgerFilter{})
	require.Empty(t, where)
	require.Empty(t, args)
}
