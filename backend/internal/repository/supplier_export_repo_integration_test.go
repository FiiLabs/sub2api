//go:build integration

// APEXONE-EXT: 对账导出仓储的真库测试。
//
// 这两条查询是这次改动里**唯一**用 sqlmock 测不出任何东西的部分：sqlmock 只会
// 把 SQL 当字符串比对，而这里每一条断言问的都是「Postgres 拿到这串东西之后做了什么」：
//
//   - `w.amount::text` 到底给出什么（这决定了对账文件里的金额是不是原样的）；
//   - 两次 LEFT JOIN users 拼得对不对，以及 reviewer_id 为 NULL 时那一行还在不在；
//   - 按 status 筛会不会撞上 ambiguous column——supplier_withdrawals 和 users
//     两张表**都有** status 这一列，这条路只在「导出且按状态筛」时才走到；
//   - limit+1 探针读到第 limit+1 行时，那一行有没有溜进文件。
//
// 前三条错了的表现都不是 panic，是运营下载到一份看起来正常、但金额被四舍五入过
// 或少了一批单子的表格。
package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exportRepoOn 造一个跑在测试事务里的导出仓储，加密器与提现仓储用的是同一个。
//
// 同一个是关键：提现仓储加密写进去、导出仓储解密读出来，两边的密钥必须是一把。
// 换成两个不同的桩，这套测试就只能证明"解密函数被调用了"，不能证明"打款单上
// 那串数字是供给者填的那一串"。
func exportRepoOn(client *dbent.Client) service.SupplierExportRepository {
	return NewSupplierExportRepository(client, testPayoutEncryptor())
}

// backdateWithdrawal 把一张单子的建单时间挪到指定时刻。
//
// 时间窗与排序这两条性质没法靠"按顺序插入"来测——同一个事务里几行的 NOW()
// 是同一个值，全部相等的 created_at 证明不了排序。
func backdateWithdrawal(t *testing.T, ctx context.Context, client *dbent.Client, id int64, at time.Time) {
	t.Helper()
	_, err := client.ExecContext(ctx,
		"UPDATE supplier_withdrawals SET created_at = $2 WHERE id = $1", id, at)
	require.NoError(t, err)
}

func collectWithdrawals(t *testing.T, ctx context.Context, repo service.SupplierExportRepository,
	filter service.SupplierWithdrawalFilter, limit int) ([]service.SupplierWithdrawalExportRow, bool) {
	t.Helper()
	var rows []service.SupplierWithdrawalExportRow
	truncated, err := repo.StreamWithdrawals(ctx, filter, limit,
		func(row *service.SupplierWithdrawalExportRow) error {
			rows = append(rows, *row)
			return nil
		})
	require.NoError(t, err)
	return rows, truncated
}

// ============================================================================
// 提现导出
// ============================================================================

// 一行完整的提现导出：邮箱来自 JOIN、金额是 NUMERIC 原文、收款账号是明文。
func TestSupplierExport_WithdrawalRowIsCompleteAndDecrypted(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "exp-full")
	seedSupplierWallet(t, txCtx, client, userID, 100)

	const account = "6222 0202 0001 2345 678 / 张三 / 招商银行深圳分行"
	w, err := withdrawalRepoOn(client).Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 30.5, PayoutChannel: "bank", PayoutAccount: account,
		UserNote: "发票抬头：张三", MaxPending: 1,
	})
	require.NoError(t, err)

	rows, truncated := collectWithdrawals(t, txCtx, exportRepoOn(client),
		service.SupplierWithdrawalFilter{UserID: userID}, 0)
	require.Len(t, rows, 1)
	assert.False(t, truncated)

	row := rows[0]
	assert.Equal(t, w.ID, row.ID)
	assert.Contains(t, row.UserEmail, "supplier-exp-full", "收款人邮箱没 JOIN 出来，运营对不上人")
	// **明文**。这份文件就是打款工作单（§3.9）。
	assert.Equal(t, account, row.PayoutAccount, "收款账号没解密，运营会照着一串 base64 去打款")
	assert.Equal(t, "发票抬头：张三", row.UserNote)
	assert.Equal(t, "pending", row.Status)
	assert.Equal(t, "bank", row.PayoutChannel)
	// NUMERIC(20,8) 的原文，不经过 float64。
	assert.Equal(t, "30.50000000", row.Amount, "金额不是 NUMERIC 原文")
	assert.NotZero(t, row.LedgerID, "扣款流水号没带出来，这一行就没法与流水对上")
	assert.False(t, row.CreatedAt.IsZero())

	// 还没处理的单子：处理人相关的三列是空的，但**行必须在**。
	assert.Zero(t, row.ReviewerID)
	assert.Empty(t, row.ReviewerEmail)
	assert.Nil(t, row.ResolvedAt)
}

// 待处理的单子（reviewer_id 为 NULL）必须出现在文件里。
//
// 这条钉的是那两个 LEFT JOIN 的 LEFT。写成 INNER 的话，所有还没人处理的单子
// 会从对账文件里整批消失——而"还没打的款"恰恰是运营导这份表格的第一个理由。
// 症状是静默的：文件能下载、格式正常、只是少了一半。
func TestSupplierExport_KeepsRowsWithoutAReviewer(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "exp-noreviewer")
	reviewerID := mustCreateSupplier(t, client, "exp-reviewer")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)

	pending, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 10, PayoutChannel: "USDT", PayoutAccount: "0xa", MaxPending: 2,
	})
	require.NoError(t, err)
	reviewed, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 20, PayoutChannel: "USDT", PayoutAccount: "0xb", MaxPending: 2,
	})
	require.NoError(t, err)
	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: reviewed.ID, Status: service.SupplierWithdrawalStatusPaid,
		ReviewerID: &reviewerID, ExternalRef: "TX-77",
	})
	require.NoError(t, err)

	rows, _ := collectWithdrawals(t, txCtx, exportRepoOn(client),
		service.SupplierWithdrawalFilter{UserID: userID}, 0)
	require.Len(t, rows, 2, "没人处理的单子从文件里消失了 = INNER JOIN")

	byID := map[int64]service.SupplierWithdrawalExportRow{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	assert.Zero(t, byID[pending.ID].ReviewerID)
	assert.Equal(t, reviewerID, byID[reviewed.ID].ReviewerID)
	assert.Contains(t, byID[reviewed.ID].ReviewerEmail, "supplier-exp-reviewer",
		"处理人邮箱没 JOIN 出来，「这笔是谁批的」就答不出来")
	assert.Equal(t, "TX-77", byID[reviewed.ID].ExternalRef)
	require.NotNil(t, byID[reviewed.ID].ResolvedAt)
}

// 按状态筛不能撞上 ambiguous column。
//
// supplier_withdrawals.status 和 users.status **同名**，而导出比屏幕上的列表
// 多了一个 LEFT JOIN users。不带表名前缀的话 Postgres 直接拒绝执行整条查询，
// 而这条路只有"导出 + 按状态筛"这一个组合能走到——列表页不 JOIN，测不出来。
func TestSupplierExport_StatusFilterIsUnambiguousAcrossTheJoin(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "exp-status")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)

	paidOne, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 10, PayoutChannel: "USDT", PayoutAccount: "0xa", MaxPending: 2,
	})
	require.NoError(t, err)
	_, err = repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 20, PayoutChannel: "USDT", PayoutAccount: "0xb", MaxPending: 2,
	})
	require.NoError(t, err)
	_, err = repo.Resolve(txCtx, service.SupplierWithdrawalResolveParams{
		ID: paidOne.ID, Status: service.SupplierWithdrawalStatusPaid,
	})
	require.NoError(t, err)

	rows, _ := collectWithdrawals(t, txCtx, exportRepoOn(client), service.SupplierWithdrawalFilter{
		UserID: userID, Status: service.SupplierWithdrawalStatusPaid,
	}, 0)
	require.Len(t, rows, 1, "状态筛子筛的不是提现单的状态")
	assert.Equal(t, paidOne.ID, rows[0].ID)
}

// 时间窗真的筛，且顺序是时间正序。
//
// 正序是刻意的：对账文件是给人从上往下读、跟着时间对流水的。倒序的表格
// 在被截断时留下的还是**最近**那一批，但读起来是反的；正序 + 尾行里的窗口说明
// 让"这份文件覆盖了哪段时间"这个问题有两个互相印证的答案。
func TestSupplierExport_WindowFiltersAndOrdersChronologically(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "exp-window")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)

	ids := make([]int64, 0, 3)
	for i := 0; i < 3; i++ {
		w, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
			UserID: userID, Amount: 10, PayoutChannel: "USDT", PayoutAccount: "0xa", MaxPending: 5,
		})
		require.NoError(t, err)
		ids = append(ids, w.ID)
	}
	base := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	backdateWithdrawal(t, txCtx, client, ids[0], base.AddDate(0, -2, 0)) // 窗口之前
	backdateWithdrawal(t, txCtx, client, ids[1], base)
	backdateWithdrawal(t, txCtx, client, ids[2], base.Add(48*time.Hour))

	start := base.Add(-time.Hour)
	end := base.Add(72 * time.Hour)
	rows, _ := collectWithdrawals(t, txCtx, exportRepoOn(client), service.SupplierWithdrawalFilter{
		UserID: userID, StartAt: &start, EndAt: &end,
	}, 0)

	require.Len(t, rows, 2, "窗口外的单子被导进来了（或窗口根本没生效）")
	assert.Equal(t, ids[1], rows[0].ID, "顺序不是建单时间正序")
	assert.Equal(t, ids[2], rows[1].ID)
}

// 探针行不进文件。
//
// limit=2 时查 3 行：第 3 行只用来回答"后面还有没有"。它要是溜进了文件，
// 那份文件就比它声称的多一行——而尾行里写的行数会与实际行数对不上，
// 一份自相矛盾的对账文件比一份截断的更难处理。
func TestSupplierExport_TruncationProbeNeverEntersTheFile(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "exp-limit")
	seedSupplierWallet(t, txCtx, client, userID, 100)
	repo := withdrawalRepoOn(client)
	for i := 0; i < 3; i++ {
		_, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
			UserID: userID, Amount: 10, PayoutChannel: "USDT", PayoutAccount: "0xa", MaxPending: 5,
		})
		require.NoError(t, err)
	}

	exportRepo := exportRepoOn(client)
	rows, truncated := collectWithdrawals(t, txCtx, exportRepo,
		service.SupplierWithdrawalFilter{UserID: userID}, 2)
	assert.True(t, truncated, "撞上限了却说文件是完整的")
	assert.Len(t, rows, 2, "探针那一行溜进文件了")

	// 恰好等于总行数时**不**算截断：差一个等号，每一份刚好导满的文件都会
	// 被打上"不完整"的标记，于是那个标记很快就没人信了。
	rows, truncated = collectWithdrawals(t, txCtx, exportRepo,
		service.SupplierWithdrawalFilter{UserID: userID}, 3)
	assert.False(t, truncated, "刚好导完也被判成截断")
	assert.Len(t, rows, 3)
}

// 232 之前的明文行照样导得出来，且与新的密文行同处一份文件。
func TestSupplierExport_MixesLegacyPlaintextWithEncryptedRows(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "exp-legacy")
	seedSupplierWallet(t, txCtx, client, userID, 100)

	_, err := withdrawalRepoOn(client).Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 10, PayoutChannel: "USDT", PayoutAccount: "0xnew", MaxPending: 5,
	})
	require.NoError(t, err)
	_, err = client.ExecContext(txCtx, `
INSERT INTO supplier_withdrawals (user_id, amount, status, payout_channel, payout_account, created_at, updated_at)
VALUES ($1, 20, 'pending', 'USDT', $2, NOW(), NOW())`, userID, "0xlegacy")
	require.NoError(t, err)

	rows, _ := collectWithdrawals(t, txCtx, exportRepoOn(client),
		service.SupplierWithdrawalFilter{UserID: userID}, 0)
	require.Len(t, rows, 2)

	accounts := []string{rows[0].PayoutAccount, rows[1].PayoutAccount}
	assert.Contains(t, accounts, "0xnew")
	assert.Contains(t, accounts, "0xlegacy", "升级前的单子导不出来 = 那批待办的款没法对账")
}

// ============================================================================
// 流水导出
// ============================================================================

// 金额列是 NUMERIC 原文，NULL 折成空串/0。
//
// 原文这件事在流水里比在提现单里更要紧：basis_amount × share_ratio = amount
// 这个等式是供给者自行核对分成的唯一依据，而 float64 往返之后它就不一定成立了。
// 这里刻意用 0.00000001（DECIMAL(20,8) 的最小刻度）和一个除不尽的比例。
func TestSupplierExport_LedgerKeepsNumericTextAndFoldsNulls(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "exp-ledger")
	sourceID := mustCreateSupplier(t, client, "exp-ledger-src")

	// 一条带满字段的入账 + 一条什么都没有的（account_id / basis / ratio 全 NULL）。
	_, err := client.ExecContext(txCtx, `
INSERT INTO supplier_credit_ledger
    (user_id, action, amount, request_id, source_user_id,
     basis_amount, share_ratio, available_after, frozen_after, history_after, remark, created_at, updated_at)
VALUES ($1, 'accrue', 0.00000001, 'req-exp-1', $2,
        0.00000003, 0.333333, 1.23456789, 0, 1.23456789, '第一笔', NOW(), NOW())`, userID, sourceID)
	require.NoError(t, err)
	_, err = client.ExecContext(txCtx, `
INSERT INTO supplier_credit_ledger (user_id, action, amount, created_at, updated_at)
VALUES ($1, 'thaw', 2, NOW() + INTERVAL '1 second', NOW())`, userID)
	require.NoError(t, err)

	var rows []service.SupplyLedgerExportRow
	truncated, err := exportRepoOn(client).StreamLedger(txCtx,
		service.SupplyAdminLedgerFilter{UserID: userID}, 0,
		func(row *service.SupplyLedgerExportRow) error {
			rows = append(rows, *row)
			return nil
		})
	require.NoError(t, err)
	assert.False(t, truncated)
	require.Len(t, rows, 2)

	full := rows[0]
	assert.Equal(t, "accrue", full.Action)
	assert.Equal(t, "0.00000001", full.Amount, "最小刻度被四舍五入掉了")
	assert.Equal(t, "0.00000003", full.BasisAmount)
	assert.Equal(t, "0.333333", full.ShareRatio, "比例不是快照原文，供给者核不出分成")
	assert.Equal(t, "1.23456789", full.AvailableAfter)
	assert.Equal(t, "req-exp-1", full.RequestID)
	assert.Equal(t, sourceID, full.SourceUserID)
	assert.Equal(t, "第一笔", full.Remark)
	assert.Contains(t, full.UserEmail, "supplier-exp-ledger")
	assert.Nil(t, full.FrozenUntil)

	// 空字段一律折成空串 / 0——CSV 里没有 null 这个概念，
	// 一个写着 "<nil>" 或 "NULL" 的单元格会被下游当成一个普通字符串值。
	sparse := rows[1]
	assert.Equal(t, "thaw", sparse.Action)
	assert.Zero(t, sparse.AccountID)
	assert.Zero(t, sparse.SourceUserID)
	assert.Empty(t, sparse.BasisAmount)
	assert.Empty(t, sparse.ShareRatio)
	assert.Empty(t, sparse.RequestID)
	assert.Empty(t, sparse.Remark)
}

// 流水导出的筛子与截断探针走的是与提现单同一套逻辑，这里只钉住"真的会筛"。
func TestSupplierExport_LedgerFiltersByActionAndWindow(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "exp-ledger-filter")
	base := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	for _, seed := range []struct {
		action string
		at     time.Time
	}{
		{"accrue", base},
		{"accrue", base.AddDate(0, -3, 0)}, // 窗口之前
		{"withdraw", base.Add(time.Hour)},
	} {
		_, err := client.ExecContext(txCtx, `
INSERT INTO supplier_credit_ledger (user_id, action, amount, created_at, updated_at)
VALUES ($1, $2, 1, $3, NOW())`, userID, seed.action, seed.at)
		require.NoError(t, err)
	}

	start := base.Add(-time.Hour)
	end := base.Add(24 * time.Hour)
	count := func(filter service.SupplyAdminLedgerFilter) int {
		n := 0
		_, err := exportRepoOn(client).StreamLedger(txCtx, filter, 0,
			func(*service.SupplyLedgerExportRow) error { n++; return nil })
		require.NoError(t, err)
		return n
	}

	assert.Equal(t, 2, count(service.SupplyAdminLedgerFilter{UserID: userID, StartAt: &start, EndAt: &end}),
		"时间窗没生效")
	assert.Equal(t, 1, count(service.SupplyAdminLedgerFilter{
		UserID: userID, Action: "accrue", StartAt: &start, EndAt: &end}),
		"动作筛子没生效")
}

// 链上单的五个新列（M3/M4）逐个出现在导出行里；人工单上它们全是空串/零。
//
// 这五列是财务把对账文件劈成两半的依据：人工的对银行流水，链上的拿
// net_amount + tx_hash 对区块浏览器。少一列不会报错——文件照样能下载、
// 格式正常，只是链上那一半永远对不上。
func TestSupplierExport_CarriesChainColumns(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "exp-chain")
	seedSupplierWallet(t, txCtx, client, userID, 200)
	repo := withdrawalRepoOn(client)
	queue := payoutQueueOn(client)

	// 一张走完 worker 全程的链上单。
	onchain := createOnchainWithdrawal(t, txCtx, repo, userID)
	require.NoError(t, queue.BeginPayout(txCtx, onchain.ID, 0))
	require.NoError(t, queue.RecordPayoutTx(txCtx, onchain.ID, "0xchain-hash"))
	_, err := queue.FinishPayout(txCtx, service.SupplierPayoutFinishParams{
		ID: onchain.ID, Status: service.SupplierWithdrawalStatusPaid, TxHash: "0xchain-hash",
	})
	require.NoError(t, err)

	// 一张人工单作对照。
	manual, err := repo.Create(txCtx, service.SupplierWithdrawalCreateParams{
		UserID: userID, Amount: 20, PayoutChannel: "bank", PayoutAccount: "acct", MaxPending: 10,
	})
	require.NoError(t, err)

	rows, _ := collectWithdrawals(t, txCtx, exportRepoOn(client),
		service.SupplierWithdrawalFilter{UserID: userID}, 0)
	require.Len(t, rows, 2)
	byID := map[int64]service.SupplierWithdrawalExportRow{}
	for _, row := range rows {
		byID[row.ID] = row
	}

	chain := byID[onchain.ID]
	assert.Equal(t, "bsc", chain.Network)
	assert.Equal(t, "USDT", chain.TokenSymbol)
	// NUMERIC 原文，不经过 float64——免手续费改版后建单一律落 0。
	assert.Equal(t, "0.00000000", chain.FeeAmount)
	// net 由数据库算：免手续费后 = amount(30)。与服务层同数由
	// TestSupplierWithdrawal_ChainSnapshotRoundTrips 钉着。
	assert.Equal(t, "30.00000000", chain.NetAmount)
	assert.Equal(t, "0xchain-hash", chain.TxHash)
	assert.Equal(t, "0xchain-hash", chain.ExternalRef, "paid 的链上单里 external_ref 与 tx_hash 同源")

	blank := byID[manual.ID]
	assert.Empty(t, blank.Network)
	assert.Empty(t, blank.TokenSymbol)
	assert.Equal(t, "0.00000000", blank.FeeAmount, "人工单的手续费是 NUMERIC 的零，不是空串——它是数字列")
	assert.Equal(t, "20.00000000", blank.NetAmount, "人工单全额到手")
	assert.Empty(t, blank.TxHash)
}
