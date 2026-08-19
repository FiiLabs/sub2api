//go:build integration

// APEXONE-EXT: 赚取钱包的真库测试。
//
// 单测（supplier_credit_repo_test.go）用 sqlmock 钉住语句形状和调用顺序，但有两件事
// 只有真 Postgres 能证明：
//  1. ON CONFLICT 的推断子句能否匹配上迁移 225 的部分唯一索引——匹配不上不是静默降级，
//     而是直接报错，会把整个计费事务带崩；
//  2. 幂等闸门在并发/重放下是否真的只入账一次。
//
// 这两条正是「消耗 === 入账」不变量的地基，所以单独跑一遍真库。
package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func mustCreateSupplier(t *testing.T, client *dbent.Client, tag string) int64 {
	t.Helper()
	u := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("supplier-%s-%d@example.com", tag, time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		Concurrency:  5,
	})
	return u.ID
}

func TestSupplierCredit_AccrueIsIdempotentPerRequestID(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "accrue")
	requestID := fmt.Sprintf("req-%d", time.Now().UnixNano())

	params := service.SupplierAccrueParams{
		SupplierUserID: userID,
		RequestID:      requestID,
		BasisAmount:    2.0,
		ShareRatio:     0.5,
		FreezeHours:    168,
	}

	applied, err := accrueSupplierCreditTx(txCtx, client, params)
	require.NoError(t, err)
	require.True(t, applied)

	// 重放同一个 request_id：闸门必须挡住，且余额一分不动。
	applied, err = accrueSupplierCreditTx(txCtx, client, params)
	require.NoError(t, err)
	require.False(t, applied)

	frozen := querySingleFloat(t, txCtx, client,
		"SELECT frozen_credit::double precision FROM supplier_credits WHERE user_id = $1", userID)
	require.InDelta(t, 1.0, frozen, 1e-9)
	available := querySingleFloat(t, txCtx, client,
		"SELECT available_credit::double precision FROM supplier_credits WHERE user_id = $1", userID)
	require.Zero(t, available)

	entries := querySingleInt(t, txCtx, client,
		"SELECT COUNT(*)::int FROM supplier_credit_ledger WHERE user_id = $1 AND action = 'accrue'", userID)
	require.Equal(t, 1, entries)

	// 审计快照：基数 × 比例 = 金额，供给者拿流水就能自行核对。
	basis := querySingleFloat(t, txCtx, client,
		"SELECT basis_amount::double precision FROM supplier_credit_ledger WHERE user_id = $1 AND action = 'accrue'", userID)
	ratio := querySingleFloat(t, txCtx, client,
		"SELECT share_ratio::double precision FROM supplier_credit_ledger WHERE user_id = $1 AND action = 'accrue'", userID)
	amount := querySingleFloat(t, txCtx, client,
		"SELECT amount::double precision FROM supplier_credit_ledger WHERE user_id = $1 AND action = 'accrue'", userID)
	require.InDelta(t, basis*ratio, amount, 1e-9)
}

// 同一个 request_id 在不同动作下互不干扰：一次请求既可能给供给者入账（accrue），
// 也可能从消费者的赚取钱包扣款（spend），两条流水必须都能落。
func TestSupplierCredit_AccrueAndSpendShareRequestIDWithoutCollision(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	supplierID := mustCreateSupplier(t, client, "shared-accrue")
	consumerID := mustCreateSupplier(t, client, "shared-spend")
	requestID := fmt.Sprintf("req-shared-%d", time.Now().UnixNano())

	applied, err := accrueSupplierCreditTx(txCtx, client, service.SupplierAccrueParams{
		SupplierUserID: supplierID,
		RequestID:      requestID,
		BasisAmount:    2.0,
		ShareRatio:     0.5,
		FreezeHours:    0,
	})
	require.NoError(t, err)
	require.True(t, applied)

	// 给消费者充点可用额，再用同一个 request_id 扣款。
	_, err = client.ExecContext(txCtx, `
INSERT INTO supplier_credits (user_id, available_credit, history_credit, created_at, updated_at)
VALUES ($1, 3.0, 3.0, NOW(), NOW())`, consumerID)
	require.NoError(t, err)

	applied, err = spendSupplierCreditTx(txCtx, client, consumerID, 1.0, requestID)
	require.NoError(t, err)
	require.True(t, applied)

	require.InDelta(t, 2.0, querySingleFloat(t, txCtx, client,
		"SELECT available_credit::double precision FROM supplier_credits WHERE user_id = $1", consumerID), 1e-9)
	require.InDelta(t, 1.0, querySingleFloat(t, txCtx, client,
		"SELECT spent_credit::double precision FROM supplier_credits WHERE user_id = $1", consumerID), 1e-9)
}

// 重放扣款必须返回 true 且不重复扣：返回 false 会让计费侧转头去扣 users.balance，
// 同一个请求就扣了两处。
func TestSupplierCredit_SpendIsIdempotentAndNeverDoubleCharges(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "spend")
	requestID := fmt.Sprintf("req-spend-%d", time.Now().UnixNano())

	_, err := client.ExecContext(txCtx, `
INSERT INTO supplier_credits (user_id, available_credit, history_credit, created_at, updated_at)
VALUES ($1, 5.0, 5.0, NOW(), NOW())`, userID)
	require.NoError(t, err)

	applied, err := spendSupplierCreditTx(txCtx, client, userID, 1.25, requestID)
	require.NoError(t, err)
	require.True(t, applied)

	applied, err = spendSupplierCreditTx(txCtx, client, userID, 1.25, requestID)
	require.NoError(t, err)
	require.True(t, applied)

	require.InDelta(t, 3.75, querySingleFloat(t, txCtx, client,
		"SELECT available_credit::double precision FROM supplier_credits WHERE user_id = $1", userID), 1e-9)
	require.Equal(t, 1, querySingleInt(t, txCtx, client,
		"SELECT COUNT(*)::int FROM supplier_credit_ledger WHERE user_id = $1 AND action = 'spend'", userID))
}

// 余额不足时不落流水、不动余额，调用方据此回退到 users.balance。
func TestSupplierCredit_SpendLeavesNoTraceWhenInsufficient(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "insufficient")
	_, err := client.ExecContext(txCtx, `
INSERT INTO supplier_credits (user_id, available_credit, history_credit, created_at, updated_at)
VALUES ($1, 0.4, 0.4, NOW(), NOW())`, userID)
	require.NoError(t, err)

	applied, err := spendSupplierCreditTx(txCtx, client, userID, 1.0, "req-insufficient")
	require.NoError(t, err)
	require.False(t, applied)

	require.InDelta(t, 0.4, querySingleFloat(t, txCtx, client,
		"SELECT available_credit::double precision FROM supplier_credits WHERE user_id = $1", userID), 1e-9)
	require.Zero(t, querySingleInt(t, txCtx, client,
		"SELECT COUNT(*)::int FROM supplier_credit_ledger WHERE user_id = $1", userID))
}

// 冻结窗未到不解冻；到期后 frozen → available，且原 accrue 流水的 frozen_until 被清空
// （下一轮扫描不会重复搬运）。
func TestSupplierCredit_ThawOnlyReleasesMaturedEntries(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "thaw")

	// 一笔已到期（冻结到 1 小时前），一笔仍在冻结（到期在 1 小时后）。
	_, err := client.ExecContext(txCtx, `
INSERT INTO supplier_credits (user_id, frozen_credit, history_credit, created_at, updated_at)
VALUES ($1, 3.0, 3.0, NOW(), NOW())`, userID)
	require.NoError(t, err)
	_, err = client.ExecContext(txCtx, `
INSERT INTO supplier_credit_ledger (user_id, action, amount, request_id, frozen_until, created_at, updated_at)
VALUES ($1, 'accrue', 1.0, $2, NOW() - INTERVAL '1 hour', NOW(), NOW()),
       ($1, 'accrue', 2.0, $3, NOW() + INTERVAL '1 hour', NOW(), NOW())`,
		userID,
		fmt.Sprintf("req-matured-%d", time.Now().UnixNano()),
		fmt.Sprintf("req-pending-%d", time.Now().UnixNano()),
	)
	require.NoError(t, err)

	thawed, err := thawSupplierCreditTx(txCtx, client, userID)
	require.NoError(t, err)
	require.InDelta(t, 1.0, thawed, 1e-9)

	require.InDelta(t, 1.0, querySingleFloat(t, txCtx, client,
		"SELECT available_credit::double precision FROM supplier_credits WHERE user_id = $1", userID), 1e-9)
	require.InDelta(t, 2.0, querySingleFloat(t, txCtx, client,
		"SELECT frozen_credit::double precision FROM supplier_credits WHERE user_id = $1", userID), 1e-9)
	require.Equal(t, 1, querySingleInt(t, txCtx, client,
		"SELECT COUNT(*)::int FROM supplier_credit_ledger WHERE user_id = $1 AND frozen_until IS NOT NULL", userID))
	require.Equal(t, 1, querySingleInt(t, txCtx, client,
		"SELECT COUNT(*)::int FROM supplier_credit_ledger WHERE user_id = $1 AND action = 'thaw'", userID))

	// 再跑一次：没有新到期的，不产生第二条 thaw 流水。
	thawed, err = thawSupplierCreditTx(txCtx, client, userID)
	require.NoError(t, err)
	require.Zero(t, thawed)
	require.Equal(t, 1, querySingleInt(t, txCtx, client,
		"SELECT COUNT(*)::int FROM supplier_credit_ledger WHERE user_id = $1 AND action = 'thaw'", userID))
}

func TestSupplierCredit_RepositoryWalletAndLedgerRoundTrip(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	repo := NewSupplierCreditRepository(client)
	userID := mustCreateSupplier(t, client, "roundtrip")

	_, err := repo.GetWallet(txCtx, userID)
	require.ErrorIs(t, err, service.ErrSupplierWalletNotFound)

	wallet, err := repo.EnsureWallet(txCtx, userID)
	require.NoError(t, err)
	require.Equal(t, userID, wallet.UserID)
	require.Zero(t, wallet.AvailableCredit)

	applied, err := repo.Accrue(txCtx, service.SupplierAccrueParams{
		SupplierUserID: userID,
		RequestID:      fmt.Sprintf("req-rt-%d", time.Now().UnixNano()),
		BasisAmount:    4.0,
		ShareRatio:     0.25,
		FreezeHours:    0,
	})
	require.NoError(t, err)
	require.True(t, applied)

	wallet, err = repo.GetWallet(txCtx, userID)
	require.NoError(t, err)
	require.InDelta(t, 1.0, wallet.AvailableCredit, 1e-9)
	require.InDelta(t, 1.0, wallet.HistoryCredit, 1e-9)

	entries, total, err := repo.ListLedger(txCtx, service.SupplierCreditLedgerFilter{
		UserID: userID,
		Action: service.SupplierCreditActionAccrue,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].RequestID)
	require.NotNil(t, entries[0].AvailableAfter)
	require.InDelta(t, 1.0, *entries[0].AvailableAfter, 1e-9)
	require.NotNil(t, entries[0].ShareRatio)
	require.InDelta(t, 0.25, *entries[0].ShareRatio, 1e-9)
}

// ---------------------------------------------------------------------------
// clawback（冻结窗内拒付追回）
// ---------------------------------------------------------------------------

// 追回只动冻结区，且必须把被撤的入账摘出解冻队列。
//
// 后半句是真库才证得了的：解冻任务扫的是 frozen_until IS NOT NULL，摘漏了的话
// 已经扣走的钱会被再往可用区搬一次，供给者的可用余额凭空变多。
func TestSupplierCredit_ClawbackReversesOnlyFrozenAccruals(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	supplierID := mustCreateSupplier(t, client, "clawback-supplier")
	consumerID := mustCreateSupplier(t, client, "clawback-consumer")
	otherConsumerID := mustCreateSupplier(t, client, "clawback-other")

	// 冻结 2.0（可追回）+ 已释放 1.0（不可追回）+ 别人产生的 1.0（不该被波及）。
	_, err := client.ExecContext(txCtx, `
INSERT INTO supplier_credits (user_id, available_credit, frozen_credit, history_credit, created_at, updated_at)
VALUES ($1, 1.0, 3.0, 4.0, NOW(), NOW())`, supplierID)
	require.NoError(t, err)

	frozenReq := fmt.Sprintf("req-frozen-%d", time.Now().UnixNano())
	releasedReq := fmt.Sprintf("req-released-%d", time.Now().UnixNano())
	otherReq := fmt.Sprintf("req-other-%d", time.Now().UnixNano())
	_, err = client.ExecContext(txCtx, `
INSERT INTO supplier_credit_ledger
    (user_id, action, amount, request_id, source_user_id, basis_amount, share_ratio, frozen_until, created_at, updated_at)
VALUES ($1, 'accrue', 2.0, $2, $3, 4.0, 0.5, NOW() + INTERVAL '100 hours', NOW(), NOW()),
       ($1, 'accrue', 1.0, $4, $3, 2.0, 0.5, NULL,                          NOW(), NOW()),
       ($1, 'accrue', 1.0, $5, $6, 2.0, 0.5, NOW() + INTERVAL '100 hours', NOW(), NOW())`,
		supplierID, frozenReq, consumerID, releasedReq, otherReq, otherConsumerID)
	require.NoError(t, err)

	// 退款基数 100：远超冻结区能覆盖的部分，用来同时验证"撤到没得撤为止"与 UncoveredBasis。
	result, err := clawbackSupplierCreditTx(txCtx, client, service.SupplierClawbackParams{
		ConsumerUserID: consumerID,
		BasisAmount:    100.0,
		Reason:         "refund order:77",
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Entries)
	require.Equal(t, 1, result.Suppliers)
	require.InDelta(t, 2.0, result.ReversedCredit, 1e-9)
	require.InDelta(t, 4.0, result.ReversedBasis, 1e-9)
	require.InDelta(t, 96.0, result.UncoveredBasis, 1e-9)

	// 冻结区少了 2.0；可用区与 history 一分未动。
	require.InDelta(t, 1.0, querySingleFloat(t, txCtx, client,
		"SELECT frozen_credit::double precision FROM supplier_credits WHERE user_id = $1", supplierID), 1e-9)
	require.InDelta(t, 1.0, querySingleFloat(t, txCtx, client,
		"SELECT available_credit::double precision FROM supplier_credits WHERE user_id = $1", supplierID), 1e-9)
	require.InDelta(t, 4.0, querySingleFloat(t, txCtx, client,
		"SELECT history_credit::double precision FROM supplier_credits WHERE user_id = $1", supplierID), 1e-9)

	// clawback 流水与被撤的入账共用 request_id，且自带审计快照。
	require.Equal(t, 1, querySingleInt(t, txCtx, client,
		"SELECT COUNT(*)::int FROM supplier_credit_ledger WHERE action = 'clawback' AND request_id = $1", frozenReq))
	require.InDelta(t, 1.0, querySingleFloat(t, txCtx, client,
		"SELECT frozen_after::double precision FROM supplier_credit_ledger WHERE action = 'clawback' AND request_id = $1", frozenReq), 1e-9)

	// 被撤的入账已摘出解冻队列：解冻任务再跑也搬不动它。
	require.Equal(t, 0, querySingleInt(t, txCtx, client,
		"SELECT COUNT(*)::int FROM supplier_credit_ledger WHERE request_id = $1 AND frozen_until IS NOT NULL", frozenReq))
	// 别人产生的那笔仍在冻结队列里，没被波及。
	require.Equal(t, 1, querySingleInt(t, txCtx, client,
		"SELECT COUNT(*)::int FROM supplier_credit_ledger WHERE request_id = $1 AND frozen_until IS NOT NULL", otherReq))
}

// 重放同一次退款不能收第二遍钱。跨事务的闸门是流水表上的部分唯一索引，
// 只有真库能证明它确实拦得住。
func TestSupplierCredit_ClawbackIsIdempotentAcrossReplays(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	supplierID := mustCreateSupplier(t, client, "clawback-replay-supplier")
	consumerID := mustCreateSupplier(t, client, "clawback-replay-consumer")

	_, err := client.ExecContext(txCtx, `
INSERT INTO supplier_credits (user_id, frozen_credit, history_credit, created_at, updated_at)
VALUES ($1, 2.0, 2.0, NOW(), NOW())`, supplierID)
	require.NoError(t, err)
	requestID := fmt.Sprintf("req-replay-%d", time.Now().UnixNano())
	_, err = client.ExecContext(txCtx, `
INSERT INTO supplier_credit_ledger
    (user_id, action, amount, request_id, source_user_id, basis_amount, share_ratio, frozen_until, created_at, updated_at)
VALUES ($1, 'accrue', 2.0, $2, $3, 4.0, 0.5, NOW() + INTERVAL '100 hours', NOW(), NOW())`,
		supplierID, requestID, consumerID)
	require.NoError(t, err)

	params := service.SupplierClawbackParams{ConsumerUserID: consumerID, BasisAmount: 4.0, Reason: "refund order:88"}

	result, err := clawbackSupplierCreditTx(txCtx, client, params)
	require.NoError(t, err)
	require.Equal(t, 1, result.Entries)

	result, err = clawbackSupplierCreditTx(txCtx, client, params)
	require.NoError(t, err)
	require.Zero(t, result.Entries)

	require.Zero(t, querySingleFloat(t, txCtx, client,
		"SELECT frozen_credit::double precision FROM supplier_credits WHERE user_id = $1", supplierID))
	require.Equal(t, 1, querySingleInt(t, txCtx, client,
		"SELECT COUNT(*)::int FROM supplier_credit_ledger WHERE action = 'clawback' AND request_id = $1", requestID))
}

// 追回之后再跑解冻：被撤的那笔不能被搬进可用区。
// 这是 clawback 与 thaw 两条写路径唯一会打架的地方，单测证不了。
func TestSupplierCredit_ClawbackSurvivesLaterThaw(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	supplierID := mustCreateSupplier(t, client, "clawback-thaw-supplier")
	consumerID := mustCreateSupplier(t, client, "clawback-thaw-consumer")

	_, err := client.ExecContext(txCtx, `
INSERT INTO supplier_credits (user_id, frozen_credit, history_credit, created_at, updated_at)
VALUES ($1, 2.0, 2.0, NOW(), NOW())`, supplierID)
	require.NoError(t, err)
	// 已经到期但解冻任务还没跑到的入账——追回与解冻抢同一笔钱的窗口。
	requestID := fmt.Sprintf("req-race-%d", time.Now().UnixNano())
	_, err = client.ExecContext(txCtx, `
INSERT INTO supplier_credit_ledger
    (user_id, action, amount, request_id, source_user_id, basis_amount, share_ratio, frozen_until, created_at, updated_at)
VALUES ($1, 'accrue', 2.0, $2, $3, 4.0, 0.5, NOW() - INTERVAL '1 hour', NOW(), NOW())`,
		supplierID, requestID, consumerID)
	require.NoError(t, err)

	result, err := clawbackSupplierCreditTx(txCtx, client, service.SupplierClawbackParams{
		ConsumerUserID: consumerID,
		BasisAmount:    4.0,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Entries)

	thawed, err := thawSupplierCreditTx(txCtx, client, supplierID)
	require.NoError(t, err)
	require.Zero(t, thawed)
	require.Zero(t, querySingleFloat(t, txCtx, client,
		"SELECT available_credit::double precision FROM supplier_credits WHERE user_id = $1", supplierID))
	require.Zero(t, querySingleFloat(t, txCtx, client,
		"SELECT frozen_credit::double precision FROM supplier_credits WHERE user_id = $1", supplierID))
}
