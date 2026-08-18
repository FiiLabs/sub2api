//go:build unit

// APEXONE-EXT: 双边市场——计费事务内供给侧结算的单测。
//
// 这里守的是三件事，任何一件破了都是资损：
//  1. 结算关闭时，计费路径与上游逐字一致（多一条 SQL 都算回归）；
//  2. 「同一请求只扣一处」——钱包付掉就绝不能再扣 users.balance；
//  3. 自供自用不入账，否则是一条 70% 的白送套利路径。
package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const supplierOwnerLookupSQL = `SELECT owner_user_id FROM accounts WHERE id = \$1 AND deleted_at IS NULL`

func newBillingTx(t *testing.T) (*sql.Tx, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx, mock
}

// 供给结算关闭（Supplier 全零）时，计费事务里不允许出现任何供给侧 SQL。
// 这条测试的价值在于：功能带着代码先上线、配置开关后打开的这段观察期里，
// 它保证「没打开」真的等于「没影响」。
func TestApplyUsageBillingEffects_SupplierDisabledLeavesBillingUntouched(t *testing.T) {
	ctx := context.Background()
	tx, mock := newBillingTx(t)

	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WithArgs(10.0, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(90.0))

	result := &service.UsageBillingApplyResult{Applied: true}
	err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		RequestID:   "req-off",
		UserID:      42,
		AccountID:   7,
		BalanceCost: 10,
	}, result)
	require.NoError(t, err)
	require.NotNil(t, result.NewBalance)
	// ExpectationsWereMet 会因为任何一条计划外的 SQL 而失败。
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyUsageBillingEffects_AccruesSupplierShareInSameTransaction(t *testing.T) {
	ctx := context.Background()
	tx, mock := newBillingTx(t)

	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WithArgs(10.0, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(90.0))
	mock.ExpectQuery(supplierOwnerLookupSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"owner_user_id"}).AddRow(int64(99)))
	// 基数 = 消费者实付 10，比例 0.5 → 入账 5，且带冻结窗。
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WithArgs(int64(99), service.SupplierCreditActionAccrue, 5.0, "req-accrue", int64(7), int64(42), 10.0, 0.5, int64(168)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(501)))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credits")).
		WithArgs(int64(99), 0.0, 5.0, 5.0).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit", "frozen_credit", "history_credit"}).
			AddRow(0.0, 5.0, 5.0))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE supplier_credit_ledger")).
		WithArgs(int64(501), 0.0, 5.0, 5.0).
		WillReturnResult(sqlmock.NewResult(0, 1))

	result := &service.UsageBillingApplyResult{Applied: true}
	err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		RequestID:   "req-accrue",
		UserID:      42,
		AccountID:   7,
		BalanceCost: 10,
		Supplier: service.UsageBillingSupplierParams{
			ShareRatio:  0.5,
			FreezeHours: 168,
		},
	}, result)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 订阅额度付费同样构成「实付」，供给者照样分成——否则订阅用户消耗的供给量白嫖。
func TestApplyUsageBillingEffects_SubscriptionCostCountsAsSettlementBasis(t *testing.T) {
	ctx := context.Background()
	tx, mock := newBillingTx(t)

	subscriptionID := int64(3)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE user_subscriptions")).
		WithArgs(8.0, subscriptionID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(supplierOwnerLookupSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"owner_user_id"}).AddRow(int64(99)))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WithArgs(int64(99), service.SupplierCreditActionAccrue, 4.0, "req-sub", int64(7), int64(42), 8.0, 0.5, int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(502)))
	// FreezeHours <= 0 → 直接进可用区。
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credits")).
		WithArgs(int64(99), 4.0, 0.0, 4.0).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit", "frozen_credit", "history_credit"}).
			AddRow(4.0, 0.0, 4.0))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE supplier_credit_ledger")).
		WithArgs(int64(502), 4.0, 0.0, 4.0).
		WillReturnResult(sqlmock.NewResult(0, 1))

	result := &service.UsageBillingApplyResult{Applied: true}
	err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		RequestID:        "req-sub",
		UserID:           42,
		AccountID:        7,
		SubscriptionID:   &subscriptionID,
		SubscriptionCost: 8,
		Supplier: service.UsageBillingSupplierParams{
			ShareRatio: 0.5,
		},
	}, result)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 自营账号（owner_user_id IS NULL）：查一次归属就收工，没有结算对象。
func TestApplyUsageBillingEffects_FirstPartyAccountAccruesNothing(t *testing.T) {
	ctx := context.Background()
	tx, mock := newBillingTx(t)

	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WithArgs(10.0, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(90.0))
	mock.ExpectQuery(supplierOwnerLookupSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"owner_user_id"}).AddRow(nil))

	result := &service.UsageBillingApplyResult{Applied: true}
	err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		RequestID:   "req-firstparty",
		UserID:      42,
		AccountID:   7,
		BalanceCost: 10,
		Supplier:    service.UsageBillingSupplierParams{ShareRatio: 0.5, FreezeHours: 168},
	}, result)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 供给者用自己挂上去的账号：左手倒右手不入账。
// 放任不管的话，自供自用等于每笔消费白拿 50%~70% 返利。
func TestApplyUsageBillingEffects_SelfSuppliedRequestAccruesNothing(t *testing.T) {
	ctx := context.Background()
	tx, mock := newBillingTx(t)

	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WithArgs(10.0, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(90.0))
	mock.ExpectQuery(supplierOwnerLookupSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"owner_user_id"}).AddRow(int64(42)))

	result := &service.UsageBillingApplyResult{Applied: true}
	err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		RequestID:   "req-self",
		UserID:      42,
		AccountID:   7,
		BalanceCost: 10,
		Supplier:    service.UsageBillingSupplierParams{ShareRatio: 0.5, FreezeHours: 168},
	}, result)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 零实付（免费分组、倍率 0）连归属都不查：主链路不为一个必然为零的分成付出一次点查。
func TestApplyUsageBillingEffects_ZeroCostSkipsOwnerLookup(t *testing.T) {
	ctx := context.Background()
	tx, mock := newBillingTx(t)

	result := &service.UsageBillingApplyResult{Applied: true}
	err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		RequestID: "req-free",
		UserID:    42,
		AccountID: 7,
		Supplier:  service.UsageBillingSupplierParams{ShareRatio: 0.5, FreezeHours: 168},
	}, result)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 钱包足额付掉整笔：users.balance 一分不动，result.NewBalance 保持 nil。
// 这是「同一请求只扣一处」的正面用例。
func TestApplyUsageBillingEffects_WalletPaymentSkipsBalanceDeduction(t *testing.T) {
	ctx := context.Background()
	tx, mock := newBillingTx(t)

	mock.ExpectQuery(regexp.QuoteMeta("FOR UPDATE")).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit"}).AddRow(25.0))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supplier_credit_ledger")).
		WithArgs(int64(42), service.SupplierCreditActionSpend, 10.0, "req-wallet", nil, nil, nil, nil, int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(601)))
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE supplier_credits")).
		WithArgs(int64(42), 10.0).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit", "frozen_credit", "history_credit"}).
			AddRow(15.0, 0.0, 25.0))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE supplier_credit_ledger")).
		WithArgs(int64(601), 15.0, 0.0, 25.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(supplierOwnerLookupSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"owner_user_id"}).AddRow(nil))

	result := &service.UsageBillingApplyResult{Applied: true}
	err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		RequestID:   "req-wallet",
		UserID:      42,
		AccountID:   7,
		BalanceCost: 10,
		Supplier: service.UsageBillingSupplierParams{
			ShareRatio:           0.5,
			FreezeHours:          168,
			SpendFromWalletFirst: true,
		},
	}, result)
	require.NoError(t, err)
	require.Nil(t, result.NewBalance, "钱包付款时余额不应被动过")
	require.NoError(t, mock.ExpectationsWereMet())
}

// 钱包余额不够：整笔回退到 users.balance，且钱包侧不留任何流水。
// 不做部分抵扣是刻意的——拆成两个来源会让对账、退款、拒付追回都要按比例拆。
func TestApplyUsageBillingEffects_InsufficientWalletFallsBackToBalance(t *testing.T) {
	ctx := context.Background()
	tx, mock := newBillingTx(t)

	mock.ExpectQuery(regexp.QuoteMeta("FOR UPDATE")).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"available_credit"}).AddRow(1.0))
	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WithArgs(10.0, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(90.0))
	mock.ExpectQuery(supplierOwnerLookupSQL).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"owner_user_id"}).AddRow(nil))

	result := &service.UsageBillingApplyResult{Applied: true}
	err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		RequestID:   "req-wallet-short",
		UserID:      42,
		AccountID:   7,
		BalanceCost: 10,
		Supplier: service.UsageBillingSupplierParams{
			ShareRatio:           0.5,
			FreezeHours:          168,
			SpendFromWalletFirst: true,
		},
	}, result)
	require.NoError(t, err)
	require.NotNil(t, result.NewBalance)
	require.InDelta(t, 90.0, *result.NewBalance, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 入账失败必须把整笔计费带回滚：收了消费者的钱却没给供给者记上，
// 等于平台单方面吞掉供给者收入，宁可让这次计费失败由上层重试。
func TestApplyUsageBillingEffects_AccrueFailureFailsTheWholeBilling(t *testing.T) {
	ctx := context.Background()
	tx, mock := newBillingTx(t)

	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WithArgs(10.0, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(90.0))
	mock.ExpectQuery(supplierOwnerLookupSQL).
		WithArgs(int64(7)).
		WillReturnError(errors.New("owner lookup exploded"))

	result := &service.UsageBillingApplyResult{Applied: true}
	err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		RequestID:   "req-boom",
		UserID:      42,
		AccountID:   7,
		BalanceCost: 10,
		Supplier:    service.UsageBillingSupplierParams{ShareRatio: 0.5, FreezeHours: 168},
	}, result)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSupplierSettlementBasisUsesConsumerActualPayment(t *testing.T) {
	require.Zero(t, supplierSettlementBasis(nil))
	require.InDelta(t, 10.0, supplierSettlementBasis(&service.UsageBillingCommand{BalanceCost: 10}), 1e-9)
	require.InDelta(t, 8.0, supplierSettlementBasis(&service.UsageBillingCommand{SubscriptionCost: 8}), 1e-9)
	// AccountQuotaCost 是账号维度记账口径（官方价 × 账号倍率），不是消费者实付，
	// 拿它当分成基数会让供给者按官方价分成，平台每笔都亏。
	require.Zero(t, supplierSettlementBasis(&service.UsageBillingCommand{AccountQuotaCost: 12}))
}
