// APEXONE-EXT: 双边市场——对账导出的 SQL 实现。
//
// 两条查询，都是「一次 QueryContext，边扫边推」。这里没有分页循环是刻意的：
// 用 LIMIT/OFFSET 翻页导出，会在两页之间给这张表留下插入新行的机会，
// 于是同一行可能被导出两次、也可能一次都不出现——对账文件里多一行少一行，
// 是这份文件唯一不能有的毛病。一个游标从头扫到尾，看到的是一个一致的快照。
//
// 金额一律 `::text`：Postgres 把 NUMERIC(20,8) 原样给出来，不经过 float64。
// 全仓其它读路径都是 `::double precision`，因为那些值要参与计算；导出的值只给人看，
// 没有理由让它先绕一趟二进制浮点（理由展开在 service/supplier_export.go 顶部）。
package repository

import (
	"context"
	"database/sql"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// supplierExportRepository 实现 service.SupplierExportRepository。
//
// 它持有 payoutAccountCipher：提现导出要把收款账号解密成明文（那份文件就是
// 打款工作单）。这也是导出没有并进 SupplierAdminRepository 的原因之一——
// 运营视图那一层从不碰收款账号，不该为了导出多拿一把密钥。
type supplierExportRepository struct {
	client *dbent.Client
	cipher payoutAccountCipher
}

// NewSupplierExportRepository 构造导出仓储。
func NewSupplierExportRepository(client *dbent.Client, encryptor service.SecretEncryptor) service.SupplierExportRepository {
	return &supplierExportRepository{client: client, cipher: payoutAccountCipher{encryptor: encryptor}}
}

// exportLimit 把上限归一，并额外多要一行。
//
// 多要的那一行是**截断探针**：查出 limit+1 行，能读到第 limit+1 行就说明后面还有，
// 而这一行本身不写进文件。不用 COUNT(*) 先数一遍是因为那要多扫一次表，
// 而且数完到扫完之间的新增行会让两个数字对不上——探针只回答"有没有更多"，
// 这正是唯一需要回答的问题。
func exportLimit(limit int) int {
	if limit <= 0 {
		limit = service.SupplyExportMaxRows
	}
	return limit
}

// ============================================================================
// 提现单
// ============================================================================

// supplierWithdrawalExportSQL 的列顺序与下面的 Scan 逐字对应。
//
// LEFT JOIN 两次 users：一次拿收款人邮箱，一次拿处理人邮箱。都用 LEFT——
// 用户被删掉之后单子还在，用 INNER JOIN 会让那些单子从对账文件里整行消失，
// 而一笔付给已注销用户的钱恰恰是最需要被对上的那种。
const supplierWithdrawalExportSQL = `
SELECT w.id,
       w.user_id,
       COALESCE(u.email, ''),
       w.amount::text,
       w.status,
       w.payout_channel,
       w.payout_account,
       COALESCE(w.user_note, ''),
       COALESCE(w.ledger_id, 0),
       COALESCE(w.reviewer_id, 0),
       COALESCE(r.email, ''),
       COALESCE(w.review_note, ''),
       COALESCE(w.external_ref, ''),
       w.created_at,
       w.resolved_at,
       COALESCE(w.network, ''),
       COALESCE(w.token_symbol, ''),
       w.fee_amount::text,
       (w.amount - w.fee_amount)::text,
       COALESCE(w.tx_hash, '')
FROM supplier_withdrawals w
LEFT JOIN users u ON u.id = w.user_id
LEFT JOIN users r ON r.id = w.reviewer_id`

func (r *supplierExportRepository) StreamWithdrawals(
	ctx context.Context,
	filter service.SupplierWithdrawalFilter,
	limit int,
	fn func(*service.SupplierWithdrawalExportRow) error,
) (bool, error) {
	limit = exportLimit(limit)
	where, args := buildSupplierWithdrawalWhereOn(filter, "w.")
	args = append(args, limit+1)
	query := fmt.Sprintf("%s%s ORDER BY w.created_at ASC, w.id ASC LIMIT $%d",
		supplierWithdrawalExportSQL, where, len(args))

	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("stream supplier withdrawals for export: %w", err)
	}
	defer func() { _ = rows.Close() }()

	emitted := 0
	for rows.Next() {
		if emitted == limit {
			// 探针行。读到它就说明后面还有，但它不进文件。
			return true, nil
		}
		var (
			row        service.SupplierWithdrawalExportRow
			sealed     string
			resolvedAt sql.NullTime
		)
		if err := rows.Scan(
			&row.ID, &row.UserID, &row.UserEmail, &row.Amount, &row.Status,
			&row.PayoutChannel, &sealed, &row.UserNote,
			&row.LedgerID, &row.ReviewerID, &row.ReviewerEmail, &row.ReviewNote, &row.ExternalRef,
			&row.CreatedAt, &resolvedAt,
			&row.Network, &row.TokenSymbol, &row.FeeAmount, &row.NetAmount, &row.TxHash,
		); err != nil {
			return false, fmt.Errorf("scan supplier withdrawal export row: %w", err)
		}
		// 解不开就整份导出失败，不写一个空账号进去：一份收款账号为空的对账文件
		// 会让运营以为供给者没填。理由与 payoutAccountCipher.open 顶部同一条。
		account, err := r.cipher.open(sealed)
		if err != nil {
			return false, err
		}
		row.PayoutAccount = account
		if resolvedAt.Valid {
			t := resolvedAt.Time
			row.ResolvedAt = &t
		}
		if err := fn(&row); err != nil {
			return false, err
		}
		emitted++
	}
	return false, rows.Err()
}

// ============================================================================
// 钱包流水
// ============================================================================

// supplyLedgerExportSQL 与运营视图的 ListLedger 查的是同一批列，
// 但金额全部 `::text`，且 NULL 在 SQL 里就折成空串/0——CSV 里没有 null 这个概念。
//
// 唯独 account_id / source_user_id 折成 0 而不是空串：它们在 CSV 里是数字列，
// 折成空串会让表格软件把整列当文本，于是筛选和排序都不按数字来。0 与真实的 id
// 不会撞车（id 从 1 起）。
const supplyLedgerExportSQL = `
SELECT l.id,
       l.user_id,
       COALESCE(u.email, ''),
       l.action,
       l.amount::text,
       COALESCE(l.request_id, ''),
       COALESCE(l.account_id, 0),
       COALESCE(l.source_user_id, 0),
       COALESCE(l.basis_amount::text, ''),
       COALESCE(l.share_ratio::text, ''),
       l.frozen_until,
       COALESCE(l.available_after::text, ''),
       COALESCE(l.frozen_after::text, ''),
       COALESCE(l.history_after::text, ''),
       COALESCE(l.remark, ''),
       l.created_at
FROM supplier_credit_ledger l
LEFT JOIN users u ON u.id = l.user_id`

func (r *supplierExportRepository) StreamLedger(
	ctx context.Context,
	filter service.SupplyAdminLedgerFilter,
	limit int,
	fn func(*service.SupplyLedgerExportRow) error,
) (bool, error) {
	limit = exportLimit(limit)
	where, args := buildSupplyAdminLedgerWhere(filter)
	args = append(args, limit+1)
	query := fmt.Sprintf("%s%s\nORDER BY l.created_at ASC, l.id ASC\nLIMIT $%d",
		supplyLedgerExportSQL, where, len(args))

	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("stream supply ledger for export: %w", err)
	}
	defer func() { _ = rows.Close() }()

	emitted := 0
	for rows.Next() {
		if emitted == limit {
			return true, nil
		}
		var (
			row         service.SupplyLedgerExportRow
			frozenUntil sql.NullTime
		)
		if err := rows.Scan(
			&row.ID, &row.UserID, &row.UserEmail, &row.Action, &row.Amount,
			&row.RequestID, &row.AccountID, &row.SourceUserID,
			&row.BasisAmount, &row.ShareRatio, &frozenUntil,
			&row.AvailableAfter, &row.FrozenAfter, &row.HistoryAfter,
			&row.Remark, &row.CreatedAt,
		); err != nil {
			return false, fmt.Errorf("scan supply ledger export row: %w", err)
		}
		if frozenUntil.Valid {
			t := frozenUntil.Time
			row.FrozenUntil = &t
		}
		if err := fn(&row); err != nil {
			return false, err
		}
		emitted++
	}
	return false, rows.Err()
}
