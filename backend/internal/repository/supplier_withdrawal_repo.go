// APEXONE-EXT: 双边市场——提现申请单的 SQL 实现。
//
// 与 supplier_credit_repo.go 分文件但同包：这里要直接用那边的流水插入与余额写回
// 助手（insertSupplierCreditLedger / querySupplierCreditBalances /
// backfillSupplierLedgerSnapshot / lockSupplierCreditAvailable），提现的每一次
// 状态变化都伴随一条流水，绕开那几个助手就等于再写一遍快照回填。
//
// 两条时序在这里落地，改动前先读 docs/two-sided-market.md §3.7：
//  1. 申请即扣款。建单、写 withdraw 流水、扣 available 在同一个事务里。
//  2. 退款只发生一次。状态机的条件更新是第一道闸，流水表 (action, request_id)
//     上的部分唯一索引是第二道——两道都在数据库里，不依赖任何进程内状态。
package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type supplierWithdrawalRepository struct {
	client *dbent.Client
	cipher payoutAccountCipher
}

// NewSupplierWithdrawalRepository 构造提现仓储。
//
// 加密器在**仓储**这一层而不是 service：收款账号在 service 与 handler 里一路都是
// 明文（运营要拿它去打款，供给者要在页面上认出自己填的是哪张卡），
// 只有落库的那一瞬间需要变成密文。把加解密放在这个边界上，
// 上面所有代码都不必知道这件事，也就不存在"某条新加的读路径忘了解密"。
func NewSupplierWithdrawalRepository(client *dbent.Client, encryptor service.SecretEncryptor) service.SupplierWithdrawalRepository {
	return &supplierWithdrawalRepository{
		client: client,
		cipher: payoutAccountCipher{encryptor: encryptor},
	}
}

// ============================================================================
// SQL 常量
// ============================================================================

// supplierWithdrawalColumns 是所有读路径共用的列清单。
//
// 抽成常量而不是各写各的：这张表的读有三处（建单后回读、状态推进后回读、列表），
// 列顺序与 scanSupplierWithdrawal 的 Scan 顺序必须逐字对应，三份手抄的清单
// 迟早会有一份漏一列，而那个错误的表现是「金额显示成了单号」。
const supplierWithdrawalColumns = `
    id, user_id, amount::double precision, status,
    payout_channel, payout_account, user_note,
    ledger_id, reviewer_id, review_note, external_ref,
    created_at, updated_at, resolved_at`

const supplierWithdrawalInsertSQL = `
INSERT INTO supplier_withdrawals (
    user_id, amount, status, payout_channel, payout_account, user_note, created_at, updated_at
)
VALUES ($1, $2, 'pending', $3, $4, $5, NOW(), NOW())
RETURNING ` + supplierWithdrawalColumns

// supplierWithdrawalCountPendingSQL 未决单计数。
//
// 与建单在同一个事务里跑，且跑在钱包行 FOR UPDATE **之后**：那把锁让同一个人的
// 两次并发申请必然串行，否则两个请求会各自数到 0、双双建单，把「每人一张」
// 变成一句空话。
const supplierWithdrawalCountPendingSQL = `
SELECT COUNT(*) FROM supplier_withdrawals WHERE user_id = $1 AND status = 'pending'`

const supplierWithdrawalAttachLedgerSQL = `
UPDATE supplier_withdrawals SET ledger_id = $2, updated_at = NOW() WHERE id = $1`

// supplierWithdrawalLockSQL 锁住单子并读出当前状态。
//
// 先锁后判，而不是「条件 UPDATE 命中几行」：命中零行有两种完全不同的原因
// （单子不是你的 / 单子已经被处理过），而运营在后台连点两下打款按钮时，
// 必须看到「已处理」而不是「不存在」——后者会让他以为自己点错了单号，再去点一次。
const supplierWithdrawalLockSQL = `
SELECT ` + supplierWithdrawalColumns + `
FROM supplier_withdrawals
WHERE id = $1
FOR UPDATE`

const supplierWithdrawalResolveSQL = `
UPDATE supplier_withdrawals
SET status       = $2,
    reviewer_id  = $3,
    review_note  = $4,
    external_ref = $5,
    resolved_at  = NOW(),
    updated_at   = NOW()
WHERE id = $1
  AND status = 'pending'
RETURNING ` + supplierWithdrawalColumns

// supplierWithdrawalRefundSQL 把钱退回可用区。
//
// 刻意不碰 history_credit / spent_credit：提现从来没有动过这两个数（见 §3.7），
// 退款自然也不该动。available 是这条路径上唯一变化的余额。
const supplierWithdrawalRefundSQL = `
UPDATE supplier_credits
SET available_credit = available_credit + $2,
    updated_at       = NOW()
WHERE user_id = $1
RETURNING available_credit::double precision,
          frozen_credit::double precision,
          history_credit::double precision`

// supplierWithdrawalDeductSQL 从可用区扣钱。
//
// WHERE 里的 available_credit >= $2 是最后一道数据库兜底：调用前已经 FOR UPDATE
// 锁行并比过余额，这里再挡一次，任何情况下可用额都不允许被扣成负数。
const supplierWithdrawalDeductSQL = `
UPDATE supplier_credits
SET available_credit = available_credit - $2,
    updated_at       = NOW()
WHERE user_id = $1
  AND available_credit >= $2
RETURNING available_credit::double precision,
          frozen_credit::double precision,
          history_credit::double precision`

// ============================================================================
// 幂等键
// ============================================================================

// supplierWithdrawalRequestID 一张单子的扣款流水幂等键。
//
// 用单号而不是随机串：单号是这条流水唯一的自然键，于是「一张单子只扣一次钱」
// 直接由流水表上那条部分唯一索引 (action, request_id) 保证，不需要任何应用层判断。
func supplierWithdrawalRequestID(id int64) string {
	return fmt.Sprintf("withdraw:%d", id)
}

// supplierWithdrawalRevertRequestID 退款流水的幂等键。
//
// 与扣款用**不同的前缀**而不是同一个：它们的 action 不同，(action, request_id)
// 本来就不会撞——但同一个字符串出现在两条流水上，会让排查的人以为它们是一对
// 「同一笔交易的两半」，进而以为可以按 request_id 去 join。前缀不同，就不会有人这么做。
func supplierWithdrawalRevertRequestID(id int64) string {
	return fmt.Sprintf("withdraw-revert:%d", id)
}

// ============================================================================
// 写路径
// ============================================================================

func (r *supplierWithdrawalRepository) Create(ctx context.Context, params service.SupplierWithdrawalCreateParams) (*service.SupplierWithdrawal, error) {
	if params.UserID <= 0 || params.Amount <= 0 {
		return nil, service.ErrSupplierWithdrawalNotFound
	}

	var out *service.SupplierWithdrawal
	err := r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		// 1. 先锁钱包行。这把锁同时承担两件事：挡住并发扣款，以及让下面那次
		//    未决单计数在同一个人的多个请求之间串行。
		available, ok, err := lockSupplierCreditAvailable(txCtx, txClient, params.UserID)
		if err != nil {
			return err
		}
		if !ok || available < params.Amount {
			// 没有钱包行 = 从没赚到过钱，与余额不足是同一件事，回同一个错误。
			return service.ErrSupplierCreditInsufficient
		}

		// 2. 未决单上限。
		if params.MaxPending > 0 {
			pending, err := scanSupplierInt64(txCtx, txClient, supplierWithdrawalCountPendingSQL, params.UserID)
			if err != nil {
				return fmt.Errorf("count pending withdrawals: %w", err)
			}
			if pending >= int64(params.MaxPending) {
				return service.ErrSupplierWithdrawalTooManyPending
			}
		}

		// 3. 建单。先建单才有单号，而单号是扣款流水的幂等键。
		//
		//    收款账号在这里变成密文（见 supplier_payout_cipher.go）。加密失败就整笔
		//    失败：这一步在扣款**之前**，失败时钱还没动过，供给者重试即可。
		//    反过来把它挪到扣款之后、或者失败时降级存明文，都是拿一个人的银行卡号
		//    去换一次不必要的成功。
		sealedAccount, err := r.cipher.seal(strings.TrimSpace(params.PayoutAccount))
		if err != nil {
			return err
		}
		withdrawal, err := r.cipher.scanSupplierWithdrawalRow(txCtx, txClient, supplierWithdrawalInsertSQL,
			params.UserID,
			params.Amount,
			strings.TrimSpace(params.PayoutChannel),
			sealedAccount,
			nullableTrimmedString(params.UserNote),
		)
		if err != nil {
			return fmt.Errorf("insert supplier withdrawal: %w", err)
		}
		if withdrawal == nil {
			return fmt.Errorf("insert supplier withdrawal returned no row")
		}

		// 4. 写扣款流水。先流水后余额，与 accrue/spend 同一个顺序：
		//    流水上的唯一索引是幂等闸门，闸门先落，余额才动。
		requestID := supplierWithdrawalRequestID(withdrawal.ID)
		remark := fmt.Sprintf("withdrawal #%d", withdrawal.ID)
		ledgerID, inserted, err := insertSupplierCreditLedger(txCtx, txClient, supplierLedgerInsert{
			UserID:    params.UserID,
			Action:    service.SupplierCreditActionWithdraw,
			Amount:    params.Amount,
			RequestID: &requestID,
		})
		if err != nil {
			return err
		}
		if !inserted {
			// 单号是 BIGSERIAL，同一个单号不可能被建两次——真撞上了说明有人
			// 手工插过流水或改过序列。宁可整笔失败，也不要一张扣不到钱的单子。
			return fmt.Errorf("withdrawal ledger already exists for request %s", requestID)
		}
		if err := setSupplierLedgerRemark(txCtx, txClient, ledgerID, remark); err != nil {
			return err
		}

		// 5. 扣款。
		snapshot, err := querySupplierCreditBalances(txCtx, txClient, supplierWithdrawalDeductSQL, params.UserID, params.Amount)
		if err != nil {
			return fmt.Errorf("deduct supplier credit for withdrawal: %w", err)
		}
		if snapshot == nil {
			// 锁已经拿在手里，数据库兜底条件仍不满足：有并发路径绕过了锁。
			return service.ErrSupplierCreditInsufficient
		}
		if err := backfillSupplierLedgerSnapshot(txCtx, txClient, ledgerID, snapshot); err != nil {
			return err
		}

		// 6. 把流水号挂回单子上，供给者对账时靠它把单子与流水对上。
		if _, err := txClient.ExecContext(txCtx, supplierWithdrawalAttachLedgerSQL, withdrawal.ID, ledgerID); err != nil {
			return fmt.Errorf("attach ledger to withdrawal: %w", err)
		}
		withdrawal.LedgerID = &ledgerID
		out = withdrawal
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *supplierWithdrawalRepository) Resolve(ctx context.Context, params service.SupplierWithdrawalResolveParams) (*service.SupplierWithdrawal, error) {
	if params.ID <= 0 {
		return nil, service.ErrSupplierWithdrawalNotFound
	}

	var out *service.SupplierWithdrawal
	err := r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		// 1. 锁住单子并看清它现在是什么。
		current, err := r.cipher.scanSupplierWithdrawalRow(txCtx, txClient, supplierWithdrawalLockSQL, params.ID)
		if err != nil {
			return fmt.Errorf("lock supplier withdrawal: %w", err)
		}
		if current == nil {
			return service.ErrSupplierWithdrawalNotFound
		}
		// 归属不符与不存在合并成同一个错误：区分它们等于提供一个枚举他人单号的信息面。
		if params.UserID > 0 && current.UserID != params.UserID {
			return service.ErrSupplierWithdrawalNotFound
		}
		if !current.Pending() {
			return service.ErrSupplierWithdrawalNotPending
		}

		// 2. 推进状态。WHERE 里那个 status = 'pending' 与上面的判断重复，是故意的：
		//    锁虽然拿着，但这条件写在语句里，读 SQL 的人不必回去确认调用方判过。
		updated, err := r.cipher.scanSupplierWithdrawalRow(txCtx, txClient, supplierWithdrawalResolveSQL,
			params.ID,
			params.Status,
			nullableInt64Arg(params.ReviewerID),
			nullableTrimmedString(params.ReviewNote),
			nullableTrimmedString(params.ExternalRef),
		)
		if err != nil {
			return fmt.Errorf("resolve supplier withdrawal: %w", err)
		}
		if updated == nil {
			return service.ErrSupplierWithdrawalNotPending
		}

		// 3. 退款。paid 不走这里——钱已经打出去了，再退一次就是凭空发钱。
		if params.Refund {
			if err := refundSupplierWithdrawal(txCtx, txClient, updated); err != nil {
				return err
			}
		}
		out = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// refundSupplierWithdrawal 把一张单子的金额退回可用区，并记一条 withdraw_revert。
//
// 流水插不进去（幂等命中）时**直接返回、不动余额**：那意味着这张单子已经退过一次，
// 再加一次钱就是凭空发钱。这一条与状态机的 pending 判断是同一条规则的两个身位，
// 留着它是因为两者失效的方式不同——状态判断挡不住「有人手工把状态改回 pending」。
func refundSupplierWithdrawal(ctx context.Context, exec supplierCreditExecer, w *service.SupplierWithdrawal) error {
	requestID := supplierWithdrawalRevertRequestID(w.ID)
	ledgerID, inserted, err := insertSupplierCreditLedger(ctx, exec, supplierLedgerInsert{
		UserID:    w.UserID,
		Action:    service.SupplierCreditActionWithdrawRevert,
		Amount:    w.Amount,
		RequestID: &requestID,
	})
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	remark := fmt.Sprintf("withdrawal #%d %s", w.ID, w.Status)
	if err := setSupplierLedgerRemark(ctx, exec, ledgerID, remark); err != nil {
		return err
	}

	snapshot, err := querySupplierCreditBalances(ctx, exec, supplierWithdrawalRefundSQL, w.UserID, w.Amount)
	if err != nil {
		return fmt.Errorf("refund supplier withdrawal: %w", err)
	}
	if snapshot == nil {
		// 钱包行没了，但单子还在。这是数据层面的不一致，不能静默——
		// 供给者的钱会凭空消失，而且没有任何一条记录说明去了哪。
		return fmt.Errorf("supplier credit wallet not found for withdrawal #%d", w.ID)
	}
	return backfillSupplierLedgerSnapshot(ctx, exec, ledgerID, snapshot)
}

// ============================================================================
// 读路径
// ============================================================================

func (r *supplierWithdrawalRepository) List(ctx context.Context, filter service.SupplierWithdrawalFilter) ([]service.SupplierWithdrawal, int64, error) {
	where, args := buildSupplierWithdrawalWhere(filter)
	client := clientFromContext(ctx, r.client)

	total, err := scanSupplierInt64(ctx, client, "SELECT COUNT(*) FROM supplier_withdrawals"+where, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("count supplier withdrawals: %w", err)
	}
	if total == 0 {
		return []service.SupplierWithdrawal{}, 0, nil
	}

	page, pageSize := filter.Page, filter.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	query := "SELECT" + supplierWithdrawalColumns + " FROM supplier_withdrawals" + where +
		fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)

	rows, err := client.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list supplier withdrawals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.SupplierWithdrawal, 0, pageSize)
	for rows.Next() {
		var item service.SupplierWithdrawal
		if err := r.cipher.scanSupplierWithdrawal(rows, &item); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *supplierWithdrawalRepository) CountPending(ctx context.Context, userID int64) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	client := clientFromContext(ctx, r.client)
	return scanSupplierInt64(ctx, client, supplierWithdrawalCountPendingSQL, userID)
}

// buildSupplierWithdrawalWhere 拼筛选条件。两个筛子都是参数化的，没有一处字符串拼值。
func buildSupplierWithdrawalWhere(filter service.SupplierWithdrawalFilter) (string, []any) {
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if filter.UserID > 0 {
		args = append(args, filter.UserID)
		clauses = append(clauses, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// ============================================================================
// 扫描助手
// ============================================================================

// supplierWithdrawalScanner 是 *sql.Rows 上这段代码用到的那部分。
type supplierWithdrawalScanner interface {
	Scan(dest ...any) error
}

func (c payoutAccountCipher) scanSupplierWithdrawal(row supplierWithdrawalScanner, out *service.SupplierWithdrawal) error {
	var (
		userNote    *string
		ledgerID    *int64
		reviewerID  *int64
		reviewNote  *string
		externalRef *string
		resolvedAt  *time.Time
	)
	if err := row.Scan(
		&out.ID, &out.UserID, &out.Amount, &out.Status,
		&out.PayoutChannel, &out.PayoutAccount, &userNote,
		&ledgerID, &reviewerID, &reviewNote, &externalRef,
		&out.CreatedAt, &out.UpdatedAt, &resolvedAt,
	); err != nil {
		return fmt.Errorf("scan supplier withdrawal: %w", err)
	}
	out.UserNote = userNote
	out.LedgerID = ledgerID
	out.ReviewerID = reviewerID
	out.ReviewNote = reviewNote
	out.ExternalRef = externalRef
	out.ResolvedAt = resolvedAt

	// 解密放在这里而不是各个调用点：这是这张表**唯一**的 Scan，
	// 把它放在这一行上，等于将来任何一条新的读路径都自动解了密。
	account, err := c.open(out.PayoutAccount)
	if err != nil {
		return err
	}
	out.PayoutAccount = account
	return nil
}

// scanSupplierWithdrawalRow 跑一条最多返回一行的语句。没有行时返回 nil（不是错误），
// 让调用方自己决定「没有行」意味着什么。
func (c payoutAccountCipher) scanSupplierWithdrawalRow(ctx context.Context, exec supplierCreditExecer, query string, args ...any) (*service.SupplierWithdrawal, error) {
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil, rows.Err()
	}
	var out service.SupplierWithdrawal
	if err := c.scanSupplierWithdrawal(rows, &out); err != nil {
		return nil, err
	}
	return &out, rows.Err()
}

// scanSupplierInt64 跑一条返回单个整数的语句（COUNT 之类）。
func scanSupplierInt64(ctx context.Context, exec supplierCreditExecer, query string, args ...any) (int64, error) {
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, rows.Err()
	}
	var out int64
	if err := rows.Scan(&out); err != nil {
		return 0, err
	}
	return out, rows.Err()
}

// nullableTrimmedString 空白串存 NULL 而不是空字符串。
//
// 两者在 Postgres 里是不同的值，而在「这条备注有没有填」这个问题上必须是同一个答案——
// 否则前端要同时判 null 和 ""，总有一处会漏。
func nullableTrimmedString(v string) any {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

// setSupplierLedgerRemark 给刚插入的流水写备注。
func setSupplierLedgerRemark(ctx context.Context, exec supplierCreditExecer, ledgerID int64, remark string) error {
	if strings.TrimSpace(remark) == "" {
		return nil
	}
	if _, err := exec.ExecContext(ctx, supplierCreditLedgerRemarkSQL, ledgerID, remark); err != nil {
		return fmt.Errorf("set supplier ledger remark: %w", err)
	}
	return nil
}

func (r *supplierWithdrawalRepository) withTx(ctx context.Context, fn func(txCtx context.Context, txClient *dbent.Client) error) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return fn(ctx, tx.Client())
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin supplier withdrawal transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := fn(txCtx, tx.Client()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit supplier withdrawal transaction: %w", err)
	}
	return nil
}
