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
	"testing"

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
