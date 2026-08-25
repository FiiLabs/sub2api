//go:build integration

// APEXONE-EXT: 提现仓储的真库测试。
//
// 这套代码里每一条断言都对应一种「钱算错」的方式，而它们全都只有真 Postgres
// 能证伪：
//   - 申请即扣款：可用额当场少掉，且流水快照与余额一致；
//   - 扣不成负数：数据库兜底条件 available_credit >= $2 必须真的挡住；
//   - 未决单上限：钱包行 FOR UPDATE 让同一个人的两次申请串行，计数才有意义；
//   - 退款只发生一次：条件更新 + (action, request_id) 部分唯一索引两道闸；
//   - 打款不退款：paid 路径一分钱都不该回到可用区。
//
// 用 sqlmock 测这些等于把 Postgres 的行为自己再写一遍——写出来的是我以为的行为，
// 不是它的行为。
package repository

import (
	"bytes"
	"context"
	stdsql "database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedSupplierWallet 直接塞一个有可用额的钱包行。
//
// 不走 accrue 是刻意的：提现这套逻辑与钱怎么赚来的无关，让它依赖入账路径
// 会把入账的回归也算到提现头上。
func seedSupplierWallet(t *testing.T, ctx context.Context, client *dbent.Client, userID int64, available float64) {
	t.Helper()
	_, err := client.ExecContext(ctx, `
INSERT INTO supplier_credits (user_id, available_credit, history_credit, created_at, updated_at)
VALUES ($1, $2, $2, NOW(), NOW())`, userID, available)
	require.NoError(t, err)
}

func supplierAvailable(t *testing.T, ctx context.Context, client *dbent.Client, userID int64) float64 {
	t.Helper()
	return querySingleFloat(t, ctx, client,
		"SELECT available_credit::double precision FROM supplier_credits WHERE user_id = $1", userID)
}

func supplierLedgerCount(t *testing.T, ctx context.Context, client *dbent.Client, userID int64, action string) int {
	t.Helper()
	return querySingleInt(t, ctx, client,
		"SELECT COUNT(*)::int FROM supplier_credit_ledger WHERE user_id = $1 AND action = $2", userID, action)
}

// withdrawalRepoOn 造一个跑在测试事务里的仓储。
//
// client 传的是 tx.Client()，withTx 会认出 ctx 里的事务并复用它——于是所有写入
// 在测试结束时随事务一起回滚，不留脏数据。
//
// 加密器传的是**真的那一个**（AES-256-GCM，测试密钥），不是桩：
// 收款账号在这套测试里要走完整的加密入库 → 解密读回，用桩会让
// "库里到底存的是什么"这个问题在这些用例里失去意义。
func withdrawalRepoOn(client *dbent.Client) service.SupplierWithdrawalRepository {
	return NewSupplierWithdrawalRepository(client, testPayoutEncryptor())
}

// testPayoutEncryptor 是测试用的 AES 加密器，密钥是一个固定的 32 字节值。
func testPayoutEncryptor() service.SecretEncryptor {
	return &AESEncryptor{key: bytes.Repeat([]byte{0x2b}, 32)}
}

// ============================================================================
// 申请即扣款
// ============================================================================

func TestSupplierWithdrawal_CreateDeductsImmediately(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-create")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)

	w, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID:        userID,
		Amount:        30,
		PayoutChannel: "USDT",
		PayoutAccount: "0xabc",
		UserNote:      "麻烦了",
		MaxPending:    1,
	})
	require.NoError(t, err)
	require.NotNil(t, w)
	assert.Equal(t, service.SupplierWithdrawalStatusPending, w.Status)
	assert.InDelta(t, 30.0, w.Amount, 1e-9)
	require.NotNil(t, w.LedgerID, "单子必须挂上扣款流水号，否则供给者无法对账")
	require.NotNil(t, w.UserNote)
	assert.Equal(t, "麻烦了", *w.UserNote)

	// 钱当场就少了——这就是「申请即扣款」。
	assert.InDelta(t, 70.0, supplierAvailable(t, txCtx, client, userID), 1e-9)
	assert.Equal(t, 1, supplierLedgerCount(t, txCtx, client, userID, service.SupplierCreditActionWithdraw))

	// 流水快照必须与扣完之后的余额一致：供给者看的是流水，对不上就等于对账对不上。
	after := querySingleFloat(t, txCtx, client,
		"SELECT available_after::double precision FROM supplier_credit_ledger WHERE id = $1", *w.LedgerID)
	assert.InDelta(t, 70.0, after, 1e-9)

	// history / spent 一分不动：提现不是消费，也不是收入。
	assert.InDelta(t, 100.0, querySingleFloat(t, txCtx, client,
		"SELECT history_credit::double precision FROM supplier_credits WHERE user_id = $1", userID), 1e-9)
	assert.Zero(t, querySingleFloat(t, txCtx, client,
		"SELECT spent_credit::double precision FROM supplier_credits WHERE user_id = $1", userID))
}

// 余额不足必须整笔失败，不留半张单子。
func TestSupplierWithdrawal_CreateRejectsOverdraft(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-overdraft")
	seedSupplierWallet(t, txCtx, client, userID, 10)
	repo := withdrawalRepoOn(client)

	_, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 10.01, PayoutChannel: "USDT", PayoutAccount: "0xabc", MaxPending: 1,
	})
	require.ErrorIs(t, err, service.ErrSupplierCreditInsufficient)

	assert.InDelta(t, 10.0, supplierAvailable(t, txCtx, client, userID), 1e-9)
	assert.Zero(t, querySingleInt(t, txCtx, client,
		"SELECT COUNT(*)::int FROM supplier_withdrawals WHERE user_id = $1", userID))
	assert.Zero(t, supplierLedgerCount(t, txCtx, client, userID, service.SupplierCreditActionWithdraw))
}

// 从没赚过钱（没有钱包行）与余额不足是同一个回答。
func TestSupplierWithdrawal_CreateWithoutWallet(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-nowallet")
	_, err := withdrawalRepoOn(client).Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 1, PayoutChannel: "USDT", PayoutAccount: "0xabc", MaxPending: 1,
	})
	require.ErrorIs(t, err, service.ErrSupplierCreditInsufficient)
}

// 恰好等于余额的申请要放行：兜底条件是 >=，不是 >。差一个等号，
// 供给者永远提不空自己的钱包，而且看不出为什么。
func TestSupplierWithdrawal_CreateAllowsExactBalance(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-exact")
	seedSupplierWallet(t, txCtx, client, userID, 42)

	_, err := withdrawalRepoOn(client).Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 42, PayoutChannel: "USDT", PayoutAccount: "0xabc", MaxPending: 1,
	})
	require.NoError(t, err)
	assert.Zero(t, supplierAvailable(t, txCtx, client, userID))
}

// 未决单上限。第二张必须被拒，且钱不能被扣第二次。
func TestSupplierWithdrawal_CreateEnforcesMaxPending(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-maxpending")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)
	params := service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 10, PayoutChannel: "USDT", PayoutAccount: "0xabc", MaxPending: 1,
	}

	_, err := repo.Create(txCtx, params)
	require.NoError(t, err)

	_, err = repo.Create(txCtx, params)
	require.ErrorIs(t, err, service.ErrSupplierWithdrawalTooManyPending)
	assert.InDelta(t, 90.0, supplierAvailable(t, txCtx, client, userID), 1e-9,
		"被上限拒掉的申请一分钱都不该扣")

	// 上限放宽到 2 就能再挂一张——证明拦住第二张的确实是上限，不是别的什么。
	params.MaxPending = 2
	_, err = repo.Create(txCtx, params)
	require.NoError(t, err)
	assert.InDelta(t, 80.0, supplierAvailable(t, txCtx, client, userID), 1e-9)

	pending, err := repo.CountPending(txCtx, userID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), pending)
}

// 终态的单子不占未决名额：不然一个人被拒过一次之后就再也提不了了。
func TestSupplierWithdrawal_ResolvedDoesNotCountAsPending(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-freeslot")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)
	params := service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 10, PayoutChannel: "USDT", PayoutAccount: "0xabc", MaxPending: 1,
	}

	first, err := repo.Create(txCtx, params)
	require.NoError(t, err)
	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: first.ID, Status: service.SupplierWithdrawalStatusRejected, Refund: true, ReviewNote: "账号错了",
	})
	require.NoError(t, err)

	_, err = repo.Create(txCtx, params)
	require.NoError(t, err, "被拒之后名额应当被释放")

	pending, err := repo.CountPending(txCtx, userID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), pending)
}

// ============================================================================
// 退款：拒绝 / 撤回退，打款不退
// ============================================================================

func TestSupplierWithdrawal_RejectRefundsOnce(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-reject")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)
	reviewer := mustCreateSupplier(t, client, "wd-reviewer")

	w, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 30, PayoutChannel: "USDT", PayoutAccount: "0xabc", MaxPending: 1,
	})
	require.NoError(t, err)
	require.InDelta(t, 70.0, supplierAvailable(t, txCtx, client, userID), 1e-9)

	resolved, err := repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID:         w.ID,
		Status:     service.SupplierWithdrawalStatusRejected,
		ReviewerID: &reviewer,
		Refund:     true,
		ReviewNote: "收款账号填错了",
	})
	require.NoError(t, err)
	assert.Equal(t, service.SupplierWithdrawalStatusRejected, resolved.Status)
	require.NotNil(t, resolved.ResolvedAt)
	require.NotNil(t, resolved.ReviewerID)
	assert.Equal(t, reviewer, *resolved.ReviewerID)

	assert.InDelta(t, 100.0, supplierAvailable(t, txCtx, client, userID), 1e-9)
	// 退款是**追加**一条 withdraw_revert，不是把 withdraw 那条删掉：
	// 删一行等于让「我的钱去哪了」在某些时刻没有答案。
	assert.Equal(t, 1, supplierLedgerCount(t, txCtx, client, userID, service.SupplierCreditActionWithdraw))
	assert.Equal(t, 1, supplierLedgerCount(t, txCtx, client, userID, service.SupplierCreditActionWithdrawRevert))

	// 第二次处理必须被状态机挡住，钱一分不动。
	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusRejected, Refund: true, ReviewNote: "再拒一次",
	})
	require.ErrorIs(t, err, service.ErrSupplierWithdrawalNotPending)
	assert.InDelta(t, 100.0, supplierAvailable(t, txCtx, client, userID), 1e-9)
	assert.Equal(t, 1, supplierLedgerCount(t, txCtx, client, userID, service.SupplierCreditActionWithdrawRevert))
}

// 第二道闸：状态机被绕过（有人手工把状态改回 pending）时，
// 流水表上 (action, request_id) 的部分唯一索引仍然挡住第二次退款。
func TestSupplierWithdrawal_RefundIdempotentEvenIfStatusIsTamperedBack(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-tamper")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)

	w, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 30, PayoutChannel: "USDT", PayoutAccount: "0xabc", MaxPending: 1,
	})
	require.NoError(t, err)
	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusCanceled, UserID: userID, Refund: true,
	})
	require.NoError(t, err)
	require.InDelta(t, 100.0, supplierAvailable(t, txCtx, client, userID), 1e-9)

	// 手工把单子按回 pending——模拟一次运维误操作或数据修复。
	_, err = client.ExecContext(txCtx,
		"UPDATE supplier_withdrawals SET status = 'pending', resolved_at = NULL WHERE id = $1", w.ID)
	require.NoError(t, err)

	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusRejected, Refund: true, ReviewNote: "再退一次",
	})
	require.NoError(t, err, "状态机放行了，但钱不能再退一次")
	assert.InDelta(t, 100.0, supplierAvailable(t, txCtx, client, userID), 1e-9,
		"幂等索引必须挡住第二次退款")
	assert.Equal(t, 1, supplierLedgerCount(t, txCtx, client, userID, service.SupplierCreditActionWithdrawRevert))
}

// 打款不退款：钱已经出去了，再退一次就是凭空发钱。
func TestSupplierWithdrawal_MarkPaidNeverRefunds(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-paid")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)

	w, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 30, PayoutChannel: "USDT", PayoutAccount: "0xabc", MaxPending: 1,
	})
	require.NoError(t, err)

	paid, err := repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusPaid, Refund: false, ExternalRef: "TX-123",
	})
	require.NoError(t, err)
	require.NotNil(t, paid.ExternalRef)
	assert.Equal(t, "TX-123", *paid.ExternalRef)

	assert.InDelta(t, 70.0, supplierAvailable(t, txCtx, client, userID), 1e-9)
	assert.Zero(t, supplierLedgerCount(t, txCtx, client, userID, service.SupplierCreditActionWithdrawRevert))
}

// ============================================================================
// 归属：别人的单子既看不见也动不了
// ============================================================================

// 撤回带 UserID 时，别人的单子回 NOT_FOUND（不是 FORBIDDEN）——
// 区分它们等于提供一个枚举他人单号的信息面。
func TestSupplierWithdrawal_CancelIsScopedToOwner(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	ownerID := mustCreateSupplier(t, client, "wd-owner")
	otherID := mustCreateSupplier(t, client, "wd-other")
	seedSupplierWallet(t, txCtx, client, ownerID, 100)
	repo := withdrawalRepoOn(client)

	w, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: ownerID, Amount: 30, PayoutChannel: "USDT", PayoutAccount: "0xabc", MaxPending: 1,
	})
	require.NoError(t, err)

	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, UserID: otherID, Status: service.SupplierWithdrawalStatusCanceled, Refund: true,
	})
	require.ErrorIs(t, err, service.ErrSupplierWithdrawalNotFound)
	assert.InDelta(t, 70.0, supplierAvailable(t, txCtx, client, ownerID), 1e-9,
		"别人的撤回不能把钱退进任何人的钱包")

	// 本人撤回则放行。
	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, UserID: ownerID, Status: service.SupplierWithdrawalStatusCanceled, Refund: true,
	})
	require.NoError(t, err)
	assert.InDelta(t, 100.0, supplierAvailable(t, txCtx, client, ownerID), 1e-9)
}

func TestSupplierWithdrawal_ResolveUnknownID(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	_, err := withdrawalRepoOn(client).Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: 999999999, Status: service.SupplierWithdrawalStatusPaid,
	})
	require.ErrorIs(t, err, service.ErrSupplierWithdrawalNotFound)
}

// ============================================================================
// 列表
// ============================================================================

func TestSupplierWithdrawal_ListFilters(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	aliceID := mustCreateSupplier(t, client, "wd-list-a")
	bobID := mustCreateSupplier(t, client, "wd-list-b")
	seedSupplierWallet(t, txCtx, client, aliceID, 100)
	seedSupplierWallet(t, txCtx, client, bobID, 100)
	repo := withdrawalRepoOn(client)

	first, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: aliceID, Amount: 10, PayoutChannel: "USDT", PayoutAccount: "0xa", MaxPending: 3,
	})
	require.NoError(t, err)
	second, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: aliceID, Amount: 20, PayoutChannel: "USDT", PayoutAccount: "0xa", MaxPending: 3,
	})
	require.NoError(t, err)
	_, err = repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: bobID, Amount: 30, PayoutChannel: "USDT", PayoutAccount: "0xb", MaxPending: 3,
	})
	require.NoError(t, err)

	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: first.ID, Status: service.SupplierWithdrawalStatusPaid,
	})
	require.NoError(t, err)

	// 按人筛：只能看到自己的两张。
	items, total, err := repo.List(txCtx, service.SupplierWithdrawalFilter{UserID: aliceID, Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, items, 2)
	// 最新的在前：一个按 id 升序的列表会让刚提交的那张沉到最后一页。
	assert.Equal(t, second.ID, items[0].ID)

	// 按人 + 状态筛。
	items, total, err = repo.List(txCtx, service.SupplierWithdrawalFilter{
		UserID: aliceID, Status: service.SupplierWithdrawalStatusPending, Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, second.ID, items[0].ID)

	// 分页：每页 1 条，第二页拿到的是较早的那张。
	items, total, err = repo.List(txCtx, service.SupplierWithdrawalFilter{UserID: aliceID, Page: 2, PageSize: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, items, 1)
	assert.Equal(t, first.ID, items[0].ID)

	// 不带 user_id = 管理端全量，两个人的单子都在。
	_, total, err = repo.List(txCtx, service.SupplierWithdrawalFilter{Page: 1, PageSize: 50})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, int64(3))
}

// 一张单子都没有时回空切片而不是 nil：nil 在 JSON 里是 null，
// 前端对 null 做 .length 会炸。
func TestSupplierWithdrawal_ListEmpty(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-empty")
	items, total, err := withdrawalRepoOn(client).List(txCtx, service.SupplierWithdrawalFilter{
		UserID: userID, Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.NotNil(t, items)
	assert.Empty(t, items)
}

// ============================================================================
// 看板：提现额与退回额分开报
// ============================================================================

// 运营看板的窗口合计必须认得 withdraw 与 withdraw_revert 两个动作。
//
// 认不出来的表现是静默的零：一个提现量恒为 0 的看板与一个「本期没人提现」的
// 看板长得一模一样。而把退回额混进提现额（或者干脆不报）会让「有多少单被拒」
// 这个需要有人去看一眼的信号消失——大量退回意味着渠道配置或审核标准出了问题。
func TestSupplierWithdrawal_OverviewSplitsWithdrawAndRevert(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-overview")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)
	admin := NewSupplierAdminRepository(client)

	before, err := admin.Overview(txCtx, 30)
	require.NoError(t, err)

	paidOne, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 30, PayoutChannel: "USDT", PayoutAccount: "0xabc", MaxPending: 2,
	})
	require.NoError(t, err)
	rejectedOne, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 20, PayoutChannel: "USDT", PayoutAccount: "0xabc", MaxPending: 2,
	})
	require.NoError(t, err)

	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: paidOne.ID, Status: service.SupplierWithdrawalStatusPaid,
	})
	require.NoError(t, err)
	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: rejectedOne.ID, Status: service.SupplierWithdrawalStatusRejected, Refund: true, ReviewNote: "账号错了",
	})
	require.NoError(t, err)

	after, err := admin.Overview(txCtx, 30)
	require.NoError(t, err)
	// Withdrawn 是**申请额**（两张单都算），不是已打款额。
	assert.InDelta(t, 50.0, after.Window.Withdrawn-before.Window.Withdrawn, 1e-6)
	assert.InDelta(t, 20.0, after.Window.WithdrawReverted-before.Window.WithdrawReverted, 1e-6)
}

// ============================================================================
// 收款账号密文存储（迁移 232）
// ============================================================================

// payoutAccountRaw 读出**库里真正存着的**那一串，绕过仓储的解密。
//
// 这个测试的全部意义就在"绕过"这两个字上：走仓储读回来的永远是明文，
// 那证不了任何事。要证的是拿到一份 pg_dump 的人看到的是什么。
func payoutAccountRaw(t *testing.T, ctx context.Context, client *dbent.Client, id int64) string {
	t.Helper()
	rows, err := client.QueryContext(ctx,
		"SELECT payout_account FROM supplier_withdrawals WHERE id = $1", id)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	require.True(t, rows.Next(), "expected one row")
	var value string
	require.NoError(t, rows.Scan(&value))
	require.NoError(t, rows.Err())
	return value
}

// 落库的是密文，读回来的是明文。
//
// 顺带证明迁移 232 真的把列宽放开了：VARCHAR(256) 下面这个账号加密后约 400 个
// base64 字符，插入会直接报 value too long——而那种失败只有真库能给出来。
func TestSupplierWithdrawal_PayoutAccountIsStoredEncrypted(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-cipher")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)

	const account = "6222 0202 0001 2345 678 / 张三 / 招商银行深圳分行"
	w, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 30, PayoutChannel: "bank", PayoutAccount: account, MaxPending: 1,
	})
	require.NoError(t, err)

	// 建单时的回读已经解过密：申请页要把他填的账号显示回去。
	assert.Equal(t, account, w.PayoutAccount)

	stored := payoutAccountRaw(t, txCtx, client, w.ID)
	assert.NotEqual(t, account, stored, "库里存的还是明文")
	assert.NotContains(t, stored, "6222", "卡号出现在库里")
	assert.NotContains(t, stored, "张三")
	assert.Contains(t, stored, supplierPayoutCipherPrefix)

	// 另外两条读路径也都得解密：列表与审批后的回读。
	// 少解一条的表现是运营照着一串 base64 去打款。
	items, _, err := repo.List(txCtx, service.SupplierWithdrawalFilter{UserID: userID, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, account, items[0].PayoutAccount, "列表没解密")

	resolved, err := repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusPaid,
	})
	require.NoError(t, err)
	assert.Equal(t, account, resolved.PayoutAccount, "审批回读没解密")
}

// 232 之前写下的明文行照常读得出来。
//
// 这条钉住的是"不需要停机窗口"这个承诺。这里绕过仓储直接插一行明文，
// 模拟的正是升级那一刻库里的实际状态：一张已经扣过钱、还等着打款的旧单子。
func TestSupplierWithdrawal_LegacyPlaintextPayoutAccountStillReadable(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-legacy")
	seedSupplierWallet(t, txCtx, client, userID, 100)

	const legacy = "0xdeadbeef"
	rows, err := client.QueryContext(txCtx, `
        INSERT INTO supplier_withdrawals (user_id, amount, status, payout_channel, payout_account, created_at, updated_at)
        VALUES ($1, 30, 'pending', 'USDT', $2, NOW(), NOW())
        RETURNING id`, userID, legacy)
	require.NoError(t, err)
	require.True(t, rows.Next())
	var id int64
	require.NoError(t, rows.Scan(&id))
	require.NoError(t, rows.Close())

	repo := withdrawalRepoOn(client)
	items, _, err := repo.List(txCtx, service.SupplierWithdrawalFilter{UserID: userID, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, legacy, items[0].PayoutAccount, "旧单子读不出来 = 升级当天所有待办一起失效")

	// 旧单子照常能推进到终态：升级不该让任何一张已经扣过钱的单子卡死。
	resolved, err := repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: id, Status: service.SupplierWithdrawalStatusPaid,
	})
	require.NoError(t, err)
	assert.Equal(t, legacy, resolved.PayoutAccount)
}

// ============================================================================
// 链上快照四列（M3）
// ============================================================================

// 四列写进去、读出来、且**库里那一行算出的 net 与服务层是同一个数**。
//
// 最后一条是这组测试的重心：fee_amount 落在 DECIMAL(20,8) 上，服务层若不把
// 估算值收敛到 8 位就交给这里，这一列会替我们做一次没人看见的截断——
// 于是「按内存里的 fee 算的 net」与「按库里这一行算的 net」不是同一个数，
// 而 M4 的 worker 打款读的是库里那一行。这种分岔只有真 Postgres 能证伪，
// sqlmock 里的 DECIMAL 是我以为的 DECIMAL。
func TestSupplierWithdrawal_ChainSnapshotRoundTrips(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-chain")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)

	// 手续费取一个把 8 位小数占满的值：截断类回归只在最后几位现形。
	const fee = 0.12345678
	const tokenAddress = "0x55d398326f99059ff775485246999027b3197955"
	w, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID:        userID,
		Amount:        30,
		PayoutChannel: "BSC-USDT",
		PayoutAccount: "0xde709f2102306220921060314715629080e2fb77",
		Network:       "bsc",
		TokenSymbol:   "USDT",
		TokenAddress:  tokenAddress,
		FeeAmount:     fee,
		MaxPending:    1,
	})
	require.NoError(t, err)

	// 建单的回读四列齐全。
	require.NotNil(t, w.Network)
	assert.Equal(t, "bsc", *w.Network)
	require.NotNil(t, w.TokenSymbol)
	assert.Equal(t, "USDT", *w.TokenSymbol)
	require.NotNil(t, w.TokenAddress)
	assert.Equal(t, tokenAddress, *w.TokenAddress)
	assert.Equal(t, fee, w.FeeAmount, "fee 回读时变了——多半是列精度或扫描类型")

	// 库里那一行：fee 一位不丢，net 与服务层同一个数。
	storedFee := querySingleFloat(t, txCtx, client,
		"SELECT fee_amount::double precision FROM supplier_withdrawals WHERE id = $1", w.ID)
	assert.Equal(t, fee, storedFee, "DECIMAL(20,8) 截断了 fee——服务层没收敛就落库")
	storedNet := querySingleFloat(t, txCtx, client,
		"SELECT (amount - fee_amount)::double precision FROM supplier_withdrawals WHERE id = $1", w.ID)
	assert.Equal(t, w.NetAmount(), storedNet, "按库里算的 net 与服务层不是同一个数")

	// 列表走的是同一个 scan，但那是今天的实现细节；明天有人给列表单写一条
	// 精简 SELECT，第一个坏的就是这里。
	items, _, err := repo.List(txCtx, service.SupplierWithdrawalFilter{UserID: userID, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NotNil(t, items[0].Network)
	assert.Equal(t, "bsc", *items[0].Network)
	assert.Equal(t, fee, items[0].FeeAmount)

	// 快照在终态推进后原样还在：审批只改状态，不动这四列。
	resolved, err := repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusPaid,
	})
	require.NoError(t, err)
	require.NotNil(t, resolved.TokenAddress)
	assert.Equal(t, tokenAddress, *resolved.TokenAddress)
	assert.Equal(t, fee, resolved.FeeAmount)
}

// 人工单三列落 NULL、fee 落 0——不是空串，不是别的哨兵值。
//
// M4 的 worker 用 `WHERE network IS NOT NULL` 捞单：这里若把空串写进去，
// 一张人工单就成了一张链不存在的「链上单」，worker 捞到它、打不出去、
// 无限重试。空串与 NULL 的区别在应用层看不见，只有真库能证。
func TestSupplierWithdrawal_ManualOrderLeavesChainColumnsNull(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-manual")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)

	w, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID:        userID,
		Amount:        30,
		PayoutChannel: "支付宝",
		PayoutAccount: "alipay@example.com",
		MaxPending:    1,
	})
	require.NoError(t, err)

	assert.Nil(t, w.Network)
	assert.Nil(t, w.TokenSymbol)
	assert.Nil(t, w.TokenAddress)
	assert.Zero(t, w.FeeAmount)

	nullRows := querySingleInt(t, txCtx, client, `
SELECT COUNT(*)::int FROM supplier_withdrawals
WHERE id = $1 AND network IS NULL AND token_symbol IS NULL AND token_address IS NULL AND fee_amount = 0`,
		w.ID)
	assert.Equal(t, 1, nullRows, "人工单的链上列不是 NULL/0——worker 会把它当成链上单捞走")
}

// ============================================================================
// 打款 worker 的队列面（M4）
// ============================================================================

// payoutQueueOn 队列仓储与审批仓储是同一个实现，这里各造一个，
// 顺带证明两个构造函数看到的是同一张表。
func payoutQueueOn(client *dbent.Client) service.SupplierPayoutQueueRepository {
	return NewSupplierPayoutQueueRepository(client, testPayoutEncryptor())
}

// createOnchainWithdrawal 建一张链上单（四列齐全）。
func createOnchainWithdrawal(t *testing.T, txCtx context.Context, repo service.SupplierWithdrawalRepository, userID int64) *service.SupplierWithdrawal {
	t.Helper()
	w, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID:        userID,
		Amount:        30,
		PayoutChannel: "BSC-USDT",
		PayoutAccount: "0xde709f2102306220921060314715629080e2fb77",
		Network:       "bsc",
		TokenSymbol:   "USDT",
		TokenAddress:  "0x55d398326f99059ff775485246999027b3197955",
		FeeAmount:     0.3,
		MaxPending:    10,
	})
	require.NoError(t, err)
	return w
}

// 捞单只捞「链上、未决、租约空闲」，且捞到即续租。
//
// 每个被排除的类别单独断言：人工单混进来是把一张该人工打的单交给广播代码，
// 已租的单捞两次是两个 worker 同时给一个人打款。
func TestSupplierWithdrawal_ClaimPayoutDueScopesAndLeases(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-claim")
	seedSupplierWallet(t, txCtx, client, userID, 200)
	repo := withdrawalRepoOn(client)
	queue := payoutQueueOn(client)

	onchain := createOnchainWithdrawal(t, txCtx, repo, userID)
	manual, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 20, PayoutChannel: "支付宝",
		PayoutAccount: "alipay@example.com", MaxPending: 10,
	})
	require.NoError(t, err)

	claimed, err := queue.ClaimPayoutDue(txCtx, 10, 5*time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1, "只该捞到那张链上单")
	assert.Equal(t, onchain.ID, claimed[0].ID)
	assert.NotEqual(t, manual.ID, claimed[0].ID)
	// 捞到的行必须带着解密后的收款地址——worker 直接拿它广播。
	assert.Equal(t, "0xde709f2102306220921060314715629080e2fb77", claimed[0].PayoutAccount)
	require.NotNil(t, claimed[0].LeasedUntil, "捞到即续租，否则下一轮会再捞一次")

	// 立刻再捞：租约在手，谁都捞不到。
	again, err := queue.ClaimPayoutDue(txCtx, 10, 5*time.Minute)
	require.NoError(t, err)
	assert.Empty(t, again, "租约期内的单子被第二次捞走 = 两个 worker 同时打一笔款")
}

// 先来的单子先打；limit 是硬上限。
func TestSupplierWithdrawal_ClaimPayoutDueOldestFirst(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-claim-order")
	seedSupplierWallet(t, txCtx, client, userID, 200)
	repo := withdrawalRepoOn(client)
	queue := payoutQueueOn(client)

	first := createOnchainWithdrawal(t, txCtx, repo, userID)
	second := createOnchainWithdrawal(t, txCtx, repo, userID)

	claimed, err := queue.ClaimPayoutDue(txCtx, 1, 5*time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, first.ID, claimed[0].ID, "插队 = 后申请的人先拿钱")

	rest, err := queue.ClaimPayoutDue(txCtx, 1, 5*time.Minute)
	require.NoError(t, err)
	require.Len(t, rest, 1)
	assert.Equal(t, second.ID, rest[0].ID)
}

// BeginPayout：同一个 nonce 幂等，换一个 nonce 拒绝，终态单拒绝。
func TestSupplierWithdrawal_BeginPayoutPinsTheNonce(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-begin")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)
	queue := payoutQueueOn(client)
	w := createOnchainWithdrawal(t, txCtx, repo, userID)

	require.NoError(t, queue.BeginPayout(txCtx, w.ID, 7))

	items, _, err := repo.List(txCtx, service.SupplierWithdrawalFilter{UserID: userID, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, service.SupplierWithdrawalStatusProcessing, items[0].Status)
	require.NotNil(t, items[0].ChainNonce)
	assert.Equal(t, int64(7), *items[0].ChainNonce)

	// 重播路径：同一个 nonce 再钉一次，放行。
	require.NoError(t, queue.BeginPayout(txCtx, w.ID, 7))
	// 换号：这正是双付的形状，条件更新必须拒绝。
	err = queue.BeginPayout(txCtx, w.ID, 8)
	assert.ErrorIs(t, err, service.ErrSupplierWithdrawalNotPending,
		"带着另一个 nonce 的 BeginPayout 被放行 = 同一张单子两个 nonce 两笔钱")
}

// 已被人工处理掉的单子，worker 一步都推不动。
func TestSupplierWithdrawal_BeginPayoutRefusesResolvedOrders(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-begin-resolved")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)
	queue := payoutQueueOn(client)
	w := createOnchainWithdrawal(t, txCtx, repo, userID)

	_, err := repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusRejected, Refund: true, ReviewNote: "手工处理",
	})
	require.NoError(t, err)

	assert.ErrorIs(t, queue.BeginPayout(txCtx, w.ID, 0), service.ErrSupplierWithdrawalNotPending)
}

// 记哈希 → 终局 paid：external_ref 与 tx_hash 同值、broadcasted_at 只写一次、
// 余额一分不动（paid 没有退款这回事）。
func TestSupplierWithdrawal_RecordTxThenFinishPaid(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-finish-paid")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)
	queue := payoutQueueOn(client)
	w := createOnchainWithdrawal(t, txCtx, repo, userID)
	require.NoError(t, queue.BeginPayout(txCtx, w.ID, 0))

	require.NoError(t, queue.RecordPayoutTx(txCtx, w.ID, "0xhash-first"))
	items, _, err := repo.List(txCtx, service.SupplierWithdrawalFilter{UserID: userID, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.NotNil(t, items[0].BroadcastedAt)
	firstBroadcast := *items[0].BroadcastedAt

	// 重播换了哈希（gasPrice 变了）：哈希更新，但放弃时钟**不许**被刷新——
	// 刷新等于让"等确认的期限"永远到不了。
	require.NoError(t, queue.RecordPayoutTx(txCtx, w.ID, "0xhash-second"))
	items, _, err = repo.List(txCtx, service.SupplierWithdrawalFilter{UserID: userID, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.NotNil(t, items[0].TxHash)
	assert.Equal(t, "0xhash-second", *items[0].TxHash)
	assert.Equal(t, firstBroadcast, *items[0].BroadcastedAt, "broadcasted_at 被重播刷新了")

	// 先制造一条 last_error（一次失败的重试留下的），让下面「paid 清空它」
	// 这条断言真的有东西可清。
	require.NoError(t, queue.ReleasePayoutLease(txCtx, w.ID, "confirm timed out once", 0))

	paid, err := queue.FinishPayout(txCtx, service.SupplierPayoutFinishParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusPaid, TxHash: "0xhash-second",
	})
	require.NoError(t, err)
	assert.Equal(t, service.SupplierWithdrawalStatusPaid, paid.Status)
	require.NotNil(t, paid.ExternalRef)
	assert.Equal(t, "0xhash-second", *paid.ExternalRef, "供给者对账的凭证就是交易哈希")
	assert.Nil(t, paid.LastError)
	assert.Nil(t, paid.LeasedUntil)
	require.NotNil(t, paid.ResolvedAt)

	// 钱已经上链发走，可用区一分不回。
	assert.InDelta(t, 70.0, supplierAvailable(t, txCtx, client, userID), 1e-9)
	assert.Equal(t, 0, supplierLedgerCount(t, txCtx, client, userID, service.SupplierCreditActionWithdrawRevert))

	// 终局只有一次。
	_, err = queue.FinishPayout(txCtx, service.SupplierPayoutFinishParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusPaid, TxHash: "0xhash-third",
	})
	assert.ErrorIs(t, err, service.ErrSupplierWithdrawalNotPending)
}

// failed 停靠位：钱还扣着；运营从这里拒绝时才退，且只退一次。
func TestSupplierWithdrawal_FinishFailedThenOperatorDecides(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-finish-failed")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)
	queue := payoutQueueOn(client)
	w := createOnchainWithdrawal(t, txCtx, repo, userID)
	require.NoError(t, queue.BeginPayout(txCtx, w.ID, 0))

	failed, err := queue.FinishPayout(txCtx, service.SupplierPayoutFinishParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusFailed, Reason: "transaction reverted on-chain",
	})
	require.NoError(t, err)
	assert.Equal(t, service.SupplierWithdrawalStatusFailed, failed.Status)
	require.NotNil(t, failed.LastError)
	assert.Nil(t, failed.ResolvedAt, "failed 不是终态，不该有 resolved_at")
	// **钱还扣着**：自动退款被刻意排除在 worker 的能力之外。
	assert.InDelta(t, 70.0, supplierAvailable(t, txCtx, client, userID), 1e-9)

	// 供给者不能撤回一张 failed 单（FromFailed=false 是用户路径）。
	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, UserID: userID, Status: service.SupplierWithdrawalStatusCanceled, Refund: true,
	})
	assert.ErrorIs(t, err, service.ErrSupplierWithdrawalNotPending)
	assert.InDelta(t, 70.0, supplierAvailable(t, txCtx, client, userID), 1e-9)

	// 运营裁决：拒绝退款。FromFailed 打开这条路，退款走原有的两道闸。
	resolved, err := repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusRejected, Refund: true,
		FromFailed: true, ReviewNote: "链上失败，退回",
	})
	require.NoError(t, err)
	assert.Equal(t, service.SupplierWithdrawalStatusRejected, resolved.Status)
	require.NotNil(t, resolved.LastError, "拒绝时保留 last_error——它解释了这张单子为什么走到人工")
	assert.InDelta(t, 100.0, supplierAvailable(t, txCtx, client, userID), 1e-9)

	// 再拒一次：状态机挡住，余额纹丝不动。
	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusRejected, Refund: true,
		FromFailed: true, ReviewNote: "再来一次",
	})
	assert.ErrorIs(t, err, service.ErrSupplierWithdrawalNotPending)
	assert.InDelta(t, 100.0, supplierAvailable(t, txCtx, client, userID), 1e-9)
}

// 运营也可以把 failed 核实成 paid（链上其实成了 / 人工补打了）——不退款，清 last_error。
func TestSupplierWithdrawal_OperatorCanMarkFailedAsPaid(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-failed-paid")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)
	queue := payoutQueueOn(client)
	w := createOnchainWithdrawal(t, txCtx, repo, userID)
	require.NoError(t, queue.BeginPayout(txCtx, w.ID, 0))
	_, err := queue.FinishPayout(txCtx, service.SupplierPayoutFinishParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusFailed, Reason: "confirmation unknown",
	})
	require.NoError(t, err)

	resolved, err := repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusPaid, Refund: false,
		FromFailed: true, ExternalRef: "0xhash-manual",
	})
	require.NoError(t, err)
	assert.Equal(t, service.SupplierWithdrawalStatusPaid, resolved.Status)
	assert.Nil(t, resolved.LastError, "paid 上留着旧报错只会误导下一个看到它的人")
	assert.InDelta(t, 70.0, supplierAvailable(t, txCtx, client, userID), 1e-9, "标 paid 不退款")
}

// 租约在手（还没翻 processing）与 processing 本身，人工审批都必须吃闭门羹。
func TestSupplierWithdrawal_ResolveRefusesOrdersHeldByTheWorker(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-resolve-held")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)
	queue := payoutQueueOn(client)
	w := createOnchainWithdrawal(t, txCtx, repo, userID)

	// 阶段一：捞走（pending + 租约）。此刻 worker 随时可能钉 nonce。
	claimed, err := queue.ClaimPayoutDue(txCtx, 10, 5*time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusRejected, Refund: true,
		FromFailed: true, ReviewNote: "抢在 worker 前面",
	})
	assert.ErrorIs(t, err, service.ErrSupplierWithdrawalProcessing,
		"租约在手时放行人工拒绝 = 退款与广播赛跑")
	assert.InDelta(t, 70.0, supplierAvailable(t, txCtx, client, userID), 1e-9)

	// 阶段二：翻进 processing 并把租约清零——状态本身就足够挡住。
	require.NoError(t, queue.BeginPayout(txCtx, w.ID, 0))
	require.NoError(t, queue.ReleasePayoutLease(txCtx, w.ID, "test", 0))
	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusRejected, Refund: true,
		FromFailed: true, ReviewNote: "还是想拒",
	})
	assert.ErrorIs(t, err, service.ErrSupplierWithdrawalProcessing)
}

// 交还租约 = 退避：退避期内捞不到，退避归零后立刻回队。
func TestSupplierWithdrawal_ReleaseLeaseBacksOff(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-release")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)
	queue := payoutQueueOn(client)
	w := createOnchainWithdrawal(t, txCtx, repo, userID)

	claimed, err := queue.ClaimPayoutDue(txCtx, 10, 5*time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	require.NoError(t, queue.ReleasePayoutLease(txCtx, w.ID, "rpc flaky", time.Hour))
	held, err := queue.ClaimPayoutDue(txCtx, 10, 5*time.Minute)
	require.NoError(t, err)
	assert.Empty(t, held, "退避期内的单子不该被捞")

	require.NoError(t, queue.ReleasePayoutLease(txCtx, w.ID, "retry now", 0))
	back, err := queue.ClaimPayoutDue(txCtx, 10, 5*time.Minute)
	require.NoError(t, err)
	require.Len(t, back, 1)
	assert.Equal(t, w.ID, back[0].ID)
	require.NotNil(t, back[0].LastError)
	assert.Equal(t, "retry now", *back[0].LastError, "退避原因要跟着单子走，运营看它排查")
}

// processing / failed 都占用未决名额：卡在链上流程里的钱仍然挂在单子上。
func TestSupplierWithdrawal_ProcessingAndFailedCountAsPending(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wd-count")
	seedSupplierWallet(t, txCtx, client, userID, 200)
	repo := withdrawalRepoOn(client)
	queue := payoutQueueOn(client)
	w := createOnchainWithdrawal(t, txCtx, repo, userID)
	require.NoError(t, queue.BeginPayout(txCtx, w.ID, 0))

	// processing 占名额。
	_, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 10, PayoutChannel: "支付宝",
		PayoutAccount: "a@b.c", MaxPending: 1,
	})
	assert.ErrorIs(t, err, service.ErrSupplierWithdrawalTooManyPending)

	// failed 同样占。
	_, err = queue.FinishPayout(txCtx, service.SupplierPayoutFinishParams{
		ID: w.ID, Status: service.SupplierWithdrawalStatusFailed, Reason: "reverted",
	})
	require.NoError(t, err)
	_, err = repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 10, PayoutChannel: "支付宝",
		PayoutAccount: "a@b.c", MaxPending: 1,
	})
	assert.ErrorIs(t, err, service.ErrSupplierWithdrawalTooManyPending,
		"failed 不占名额的话，一张卡住的链上单会放开整个闸门")
}

// 提现仓储的资金事务同样显式 READ COMMITTED——与
// TestSupplierCredit_WalletTxSurvivesSerializableServerDefault 同一个事故、
// 同一个修法、同一套确定性构造（快照钉住 → 并发提交 → 事务内再写）。
// 单独一条而不是共用：两个仓储的 withTx 是两份代码，谁被单独改回
// r.client.Tx(ctx) 都得有自己的红灯。
func TestSupplierWithdrawal_TxSurvivesSerializableServerDefault(t *testing.T) {
	serializableDB, err := stdsql.Open("postgres", integrationDSN+"&default_transaction_isolation=serializable")
	require.NoError(t, err)
	t.Cleanup(func() { _ = serializableDB.Close() })
	require.NoError(t, serializableDB.Ping())

	drv := entsql.OpenDB(dialect.Postgres, serializableDB)
	client := dbent.NewClient(dbent.Driver(drv))
	repo := &supplierWithdrawalRepository{client: client, cipher: payoutAccountCipher{encryptor: testPayoutEncryptor()}}

	ctx := context.Background()
	userID := mustCreateSupplier(t, integrationEntClient, "serializable-wd")
	t.Cleanup(func() {
		_, _ = integrationDB.Exec("DELETE FROM supplier_credits WHERE user_id = $1", userID)
		_, _ = integrationDB.Exec("DELETE FROM users WHERE id = $1", userID)
	})
	_, err = integrationDB.Exec(
		"INSERT INTO supplier_credits (user_id, created_at, updated_at) VALUES ($1, NOW(), NOW())", userID)
	require.NoError(t, err)

	snapshotPinned := make(chan struct{})
	concurrentDone := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- repo.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
			rows, err := txClient.QueryContext(txCtx,
				"SELECT available_credit FROM supplier_credits WHERE user_id = $1", userID)
			if err != nil {
				return err
			}
			_ = rows.Close()
			close(snapshotPinned)
			<-concurrentDone
			_, err = txClient.ExecContext(txCtx,
				"UPDATE supplier_credits SET updated_at = NOW() WHERE user_id = $1", userID)
			return err
		})
	}()

	<-snapshotPinned
	_, err = integrationDB.Exec("UPDATE supplier_credits SET updated_at = NOW() WHERE user_id = $1", userID)
	require.NoError(t, err)
	close(concurrentDone)

	require.NoError(t, <-errCh)
}
