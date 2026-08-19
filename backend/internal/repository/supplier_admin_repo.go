// APEXONE-EXT: 双边市场——管理端运营视图的 SQL 实现。
//
// 全是跨表只读聚合（accounts × supplier_credits × supplier_credit_ledger × users）。
// 三条贯穿本文件的约束：
//
//  1. **状态字面量一律从 service 常量拼进来**，不在 SQL 里手写第二遍。这些表达式与
//     supplier_onboarding_repo.go 里那条扫描语句读的是同一个 jsonb 键；两处漂移的
//     症状是看板上某一类账号凭空消失，而没有任何报错。
//  2. **前端传的东西一个字节都不进 SQL 文本**。排序键走白名单映射到写死的 ORDER BY，
//     其余全部是占位符参数。这几张表里有余额。
//  3. **「供给者」的定义只有一处**（supplyRosterUserSetSQL）：看板顶部的人数和名册
//     翻页的总数必须是同一个数，否则运营会以为自己漏看了一页。
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type supplierAdminRepository struct {
	client *dbent.Client
}

// NewSupplierAdminRepository 构造运营视图仓储。
func NewSupplierAdminRepository(client *dbent.Client) service.SupplierAdminRepository {
	return &supplierAdminRepository{client: client}
}

// supplyStateExpr 是「这个账号的接入状态」的唯一表达式（带 a. 别名）。
//
// 与 supplierAccountListByStateSQL 里的兜底规则必须一致：状态缺失算 pending_review。
// 存量供给账号（迁移之前挂上来的、以及任何 extra 被清过的）都落在这一支上，
// 把它们算成「已入池」会让看板报出一批根本没跑过观察期的活跃号。
var supplyStateExpr = fmt.Sprintf(`COALESCE(NULLIF(a.extra->>'%s', ''), '%s')`,
	service.SupplyStateExtraKey, service.SupplyStatePendingReview)

// supplyAccountCountCols 是账号按状态分布的七个计数，看板与名册共用。
//
// Unhealthy 用 `status <> active` 而不是枚举所有坏状态：上游随时可能加一个新的
// 失败态，枚举法会把它悄悄算成健康的——而这个格子存在的全部意义就是「有号坏了」。
var supplyAccountCountCols = fmt.Sprintf(`
    COUNT(*)                                                    AS acc_total,
    COUNT(*) FILTER (WHERE %[1]s = '%[2]s')                     AS acc_pending_review,
    COUNT(*) FILTER (WHERE %[1]s = '%[3]s')                     AS acc_active,
    COUNT(*) FILTER (WHERE %[1]s = '%[4]s')                     AS acc_draining,
    COUNT(*) FILTER (WHERE %[1]s = '%[5]s')                     AS acc_retired,
    COUNT(*) FILTER (WHERE a.status <> '%[6]s')                 AS acc_unhealthy,
    COUNT(*) FILTER (WHERE a.schedulable)                       AS acc_schedulable`,
	supplyStateExpr,
	service.SupplyStatePendingReview,
	service.SupplyStateActive,
	service.SupplyStateDraining,
	service.SupplyStateRetired,
	service.StatusActive,
)

// supplyAccountScope 是「哪些账号算供给账号」的唯一定义。
//
// owner_user_id IS NOT NULL 是分界线：自营账号没有归属人、不分成、不该出现在
// 供给看板的任何一个数字里，否则运营会按一个混了自营号的分母去判断供给侧健康度。
const supplyAccountScope = `a.deleted_at IS NULL AND a.owner_user_id IS NOT NULL`

// supplyRosterUserSetSQL 是「谁算一个供给者」的唯一定义。
//
// 两个来源取并集：现在名下有号的人，以及钱包里还有过钱的人。第二支不能省——
// 一个把号全下线（或号被删）但余额还没结清的人，仍然是一笔要付出去的钱。
// 只按账号统计会让这笔负债从看板上整个消失，而它在数据库里活得好好的。
var supplyRosterUserSetSQL = fmt.Sprintf(`
    SELECT a.owner_user_id AS user_id
    FROM accounts a
    WHERE %s
    UNION
    SELECT w.user_id
    FROM supplier_credits w
    WHERE w.available_credit > 0 OR w.frozen_credit > 0 OR w.history_credit > 0 OR w.spent_credit > 0`,
	supplyAccountScope)

// ============================================================================
// Overview
// ============================================================================

var supplyOverviewSupplierCountSQL = `SELECT COUNT(*) FROM (` + supplyRosterUserSetSQL + `) s`

var supplyOverviewAccountsSQL = fmt.Sprintf(`
SELECT %s
FROM accounts a
WHERE %s`, supplyAccountCountCols, supplyAccountScope)

const supplyOverviewWalletSQL = `
SELECT COUNT(*),
       COALESCE(SUM(available_credit), 0)::double precision,
       COALESCE(SUM(frozen_credit), 0)::double precision,
       COALESCE(SUM(history_credit), 0)::double precision,
       COALESCE(SUM(spent_credit), 0)::double precision
FROM supplier_credits`

// supplyOverviewWindowSQL 按动作分组求和。
//
// 刻意 GROUP BY 而不是写五个 SUM(FILTER)：动作是会加的（提现就在路上），
// 分组的写法加一个动作只需要在 Go 侧多认一个 case，漏认的那个会被记成 0
// 而不是被算进别的桶里。
const supplyOverviewWindowSQL = `
SELECT action, COALESCE(SUM(amount), 0)::double precision
FROM supplier_credit_ledger
WHERE created_at >= NOW() - make_interval(days => $1)
GROUP BY action`

func (r *supplierAdminRepository) Overview(ctx context.Context, windowDays int) (*service.SupplyMarketOverview, error) {
	client := clientFromContext(ctx, r.client)
	overview := &service.SupplyMarketOverview{Window: service.SupplyLedgerWindow{Days: windowDays}}

	suppliers, err := scanInt64(ctx, client, supplyOverviewSupplierCountSQL)
	if err != nil {
		return nil, fmt.Errorf("count suppliers: %w", err)
	}
	overview.Suppliers = suppliers

	counts, err := scanSupplyAccountCounts(ctx, client, supplyOverviewAccountsSQL)
	if err != nil {
		return nil, err
	}
	overview.Accounts = counts

	if err := scanSupplyWalletTotals(ctx, client, &overview.Wallet); err != nil {
		return nil, err
	}
	if err := scanSupplyLedgerWindow(ctx, client, windowDays, &overview.Window); err != nil {
		return nil, err
	}
	return overview, nil
}

// scanSupplyAccountCounts 读一行七列的计数。query 必须正好产出一行。
func scanSupplyAccountCounts(ctx context.Context, client *dbent.Client, query string, args ...any) (service.SupplyAccountCounts, error) {
	var counts service.SupplyAccountCounts
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return counts, fmt.Errorf("count supply accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(
			&counts.Total, &counts.PendingReview, &counts.Active,
			&counts.Draining, &counts.Retired, &counts.Unhealthy, &counts.Schedulable,
		); err != nil {
			return counts, fmt.Errorf("scan supply account counts: %w", err)
		}
	}
	return counts, rows.Err()
}

func scanSupplyWalletTotals(ctx context.Context, client *dbent.Client, totals *service.SupplyWalletTotals) error {
	rows, err := client.QueryContext(ctx, supplyOverviewWalletSQL)
	if err != nil {
		return fmt.Errorf("sum supplier wallets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&totals.Wallets, &totals.Available, &totals.Frozen, &totals.History, &totals.Spent); err != nil {
			return fmt.Errorf("scan supplier wallet totals: %w", err)
		}
	}
	return rows.Err()
}

func scanSupplyLedgerWindow(ctx context.Context, client *dbent.Client, windowDays int, window *service.SupplyLedgerWindow) error {
	rows, err := client.QueryContext(ctx, supplyOverviewWindowSQL, windowDays)
	if err != nil {
		return fmt.Errorf("sum supplier ledger window: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			action string
			amount float64
		)
		if err := rows.Scan(&action, &amount); err != nil {
			return fmt.Errorf("scan supplier ledger window: %w", err)
		}
		// 认不出来的动作被丢弃而不是加进某个桶：把一个未知动作的金额混进
		// 「本期入账」里，是一个会直接影响打款决定的错。
		switch action {
		case service.SupplierCreditActionAccrue:
			window.Accrued = amount
		case service.SupplierCreditActionClawback:
			window.Clawed = amount
		case service.SupplierCreditActionThaw:
			window.Thawed = amount
		case service.SupplierCreditActionSpend:
			window.Spent = amount
		case service.SupplierCreditActionWithdraw:
			window.Withdrawn = amount
		}
	}
	return rows.Err()
}

// ============================================================================
// 名册
// ============================================================================

// supplierRosterOrderBy 把排序键翻译成写死的 ORDER BY 片段。
//
// 与 supplierIdentitySQL 同一个脾气：未知键返回空串，调用方据此报错。排序键是最
// 容易被顺手写成「前端传什么就拼什么」的地方，而它紧挨着一张带余额的表。
//
// 每一支都以 u.id 收尾：没有稳定的次级排序，两个余额相同的人会在翻页时来回跳，
// 表现为「某一行明明存在却在两页之间反复出现/消失」。
func supplierRosterOrderBy(sort service.SupplierRosterSort) string {
	switch sort {
	case service.SupplierRosterSortOwed:
		return "owed DESC, u.id ASC"
	case service.SupplierRosterSortHistory:
		return "wallet_history DESC, u.id ASC"
	case service.SupplierRosterSortAccounts:
		return "accounts_total DESC, u.id ASC"
	case service.SupplierRosterSortRecent:
		// NULLS LAST：从没赚到过钱的人排在最后。他们是另一个问题（挂了号没被调度到），
		// 混在「最近活跃」的头部会把这个排序变得没法用。
		return "last_accrual_at DESC NULLS LAST, u.id ASC"
	default:
		return ""
	}
}

// supplierRosterBaseSQL 名册主体。%s 依次是：账号计数列、账号范围、关键字条件、排序。
//
// users 上**不加** deleted_at IS NULL：软删掉的用户如果还欠着钱，那笔负债依然存在，
// 从看板上抹掉它只会让它在对账时凭空冒出来。用户自身的状态由 user_status 列如实报出。
var supplierRosterBaseSQL = fmt.Sprintf(`
WITH acc AS (
    SELECT a.owner_user_id AS user_id, %s
    FROM accounts a
    WHERE %s
    GROUP BY a.owner_user_id
),
wal AS (
    SELECT user_id, available_credit, frozen_credit, history_credit, spent_credit
    FROM supplier_credits
),
ids AS (%s)
SELECT u.id,
       u.email,
       COALESCE(u.username, ''),
       u.status,
       COALESCE(acc.acc_total, 0) AS accounts_total,
       COALESCE(acc.acc_pending_review, 0),
       COALESCE(acc.acc_active, 0),
       COALESCE(acc.acc_draining, 0),
       COALESCE(acc.acc_retired, 0),
       COALESCE(acc.acc_unhealthy, 0),
       COALESCE(acc.acc_schedulable, 0),
       COALESCE(wal.available_credit, 0)::double precision,
       COALESCE(wal.frozen_credit, 0)::double precision,
       COALESCE(wal.history_credit, 0)::double precision AS wallet_history,
       COALESCE(wal.spent_credit, 0)::double precision,
       (COALESCE(wal.available_credit, 0) + COALESCE(wal.frozen_credit, 0))::double precision AS owed,
       (SELECT l.created_at
          FROM supplier_credit_ledger l
         WHERE l.user_id = u.id AND l.action = '%s'
         ORDER BY l.created_at DESC
         LIMIT 1) AS last_accrual_at
FROM ids
JOIN users u ON u.id = ids.user_id
LEFT JOIN acc ON acc.user_id = ids.user_id
LEFT JOIN wal ON wal.user_id = ids.user_id`,
	supplyAccountCountCols, supplyAccountScope, supplyRosterUserSetSQL, service.SupplierCreditActionAccrue)

var supplierRosterCountSQL = fmt.Sprintf(`
WITH ids AS (%s)
SELECT COUNT(*)
FROM ids
JOIN users u ON u.id = ids.user_id`, supplyRosterUserSetSQL)

func (r *supplierAdminRepository) ListSuppliers(ctx context.Context, filter service.SupplierRosterFilter) ([]service.SupplierRosterEntry, int64, error) {
	orderBy := supplierRosterOrderBy(filter.Sort)
	if orderBy == "" {
		return nil, 0, fmt.Errorf("unsupported supplier roster sort %q", filter.Sort)
	}

	client := clientFromContext(ctx, r.client)
	keyword := strings.TrimSpace(filter.Keyword)

	var (
		where string
		args  []any
	)
	if keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = fmt.Sprintf("\nWHERE (u.email ILIKE $%d OR COALESCE(u.username, '') ILIKE $%d)", len(args), len(args))
	}

	total, err := scanInt64(ctx, client, supplierRosterCountSQL+where, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("count supplier roster: %w", err)
	}
	if total == 0 {
		return []service.SupplierRosterEntry{}, 0, nil
	}

	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	query := fmt.Sprintf("%s%s\nORDER BY %s\nLIMIT $%d OFFSET $%d",
		supplierRosterBaseSQL, where, orderBy, len(args)-1, len(args))

	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list supplier roster: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]service.SupplierRosterEntry, 0, filter.PageSize)
	for rows.Next() {
		var (
			entry         service.SupplierRosterEntry
			owed          float64
			lastAccrualAt sql.NullTime
		)
		if err := rows.Scan(
			&entry.UserID, &entry.Email, &entry.Username, &entry.UserStatus,
			&entry.Accounts.Total, &entry.Accounts.PendingReview, &entry.Accounts.Active,
			&entry.Accounts.Draining, &entry.Accounts.Retired, &entry.Accounts.Unhealthy,
			&entry.Accounts.Schedulable,
			&entry.Wallet.Available, &entry.Wallet.Frozen, &entry.Wallet.History, &entry.Wallet.Spent,
			&owed, &lastAccrualAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan supplier roster row: %w", err)
		}
		// owed 只为排序而存在，不回给前端：它恒等于 available + frozen，
		// 多报一个能被前端自己算出来的字段，就多一处会算得跟后端不一样的地方。
		if lastAccrualAt.Valid {
			t := lastAccrualAt.Time
			entry.LastAccrualAt = &t
		}
		entries = append(entries, entry)
	}
	return entries, total, rows.Err()
}

// ============================================================================
// 账号明细
// ============================================================================

var supplyAccountListSQL = fmt.Sprintf(`
SELECT a.id,
       a.name,
       a.platform,
       COALESCE(a.owner_user_id, 0),
       COALESCE(u.email, ''),
       %s,
       a.status,
       COALESCE(a.error_message, ''),
       a.schedulable,
       COALESCE(a.credentials->>'email_address', ''),
       a.last_used_at,
       a.created_at,
       a.extra->>'%s',
       a.extra->>'%s',
       a.extra->>'%s',
       a.extra->>'%s'
FROM accounts a
LEFT JOIN users u ON u.id = a.owner_user_id
WHERE %s`,
	supplyStateExpr,
	service.SupplyProbationSinceExtraKey,
	service.SupplyProbePassesExtraKey,
	service.SupplyProbeErrorExtraKey,
	service.SupplyDrainUntilExtraKey,
	supplyAccountScope)

var supplyAccountCountSQL = fmt.Sprintf(`
SELECT COUNT(*)
FROM accounts a
WHERE %s`, supplyAccountScope)

// buildSupplyAccountWhere 拼筛选条件。全部走占位符。
func buildSupplyAccountWhere(filter service.SupplyAccountAdminFilter) (string, []any) {
	var (
		clauses []string
		args    []any
	)
	if state := strings.TrimSpace(filter.State); state != "" {
		args = append(args, state)
		clauses = append(clauses, fmt.Sprintf("%s = $%d", supplyStateExpr, len(args)))
	}
	switch filter.Health {
	case service.SupplyAccountHealthHealthy:
		args = append(args, service.StatusActive)
		clauses = append(clauses, fmt.Sprintf("a.status = $%d", len(args)))
	case service.SupplyAccountHealthUnhealthy:
		args = append(args, service.StatusActive)
		clauses = append(clauses, fmt.Sprintf("a.status <> $%d", len(args)))
	}
	if filter.OwnerUserID > 0 {
		args = append(args, filter.OwnerUserID)
		clauses = append(clauses, fmt.Sprintf("a.owner_user_id = $%d", len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " AND " + strings.Join(clauses, " AND "), args
}

func (r *supplierAdminRepository) ListAccounts(ctx context.Context, filter service.SupplyAccountAdminFilter) ([]service.SupplyAccountAdminView, int64, error) {
	client := clientFromContext(ctx, r.client)
	where, args := buildSupplyAccountWhere(filter)

	total, err := scanInt64(ctx, client, supplyAccountCountSQL+where, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("count supply accounts: %w", err)
	}
	if total == 0 {
		return []service.SupplyAccountAdminView{}, 0, nil
	}

	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	query := fmt.Sprintf("%s%s\nORDER BY a.id DESC\nLIMIT $%d OFFSET $%d",
		supplyAccountListSQL, where, len(args)-1, len(args))

	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list supply accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	views := make([]service.SupplyAccountAdminView, 0, filter.PageSize)
	for rows.Next() {
		var (
			view           service.SupplyAccountAdminView
			lastUsedAt     sql.NullTime
			probationSince sql.NullString
			probePasses    sql.NullString
			probeError     sql.NullString
			drainUntil     sql.NullString
		)
		if err := rows.Scan(
			&view.ID, &view.Name, &view.Platform, &view.OwnerUserID, &view.OwnerEmail,
			&view.SupplyState, &view.Status, &view.ErrorMessage, &view.Schedulable,
			&view.EmailAddress, &lastUsedAt, &view.CreatedAt,
			&probationSince, &probePasses, &probeError, &drainUntil,
		); err != nil {
			return nil, 0, fmt.Errorf("scan supply account row: %w", err)
		}
		if lastUsedAt.Valid {
			t := lastUsedAt.Time
			view.LastUsedAt = &t
		}
		view.ProbationSince = parseSupplyExtraTime(probationSince)
		view.DrainUntil = parseSupplyExtraTime(drainUntil)
		view.ProbePasses = parseSupplyExtraInt(probePasses)
		if probeError.Valid {
			view.ProbeError = probeError.String
		}
		views = append(views, view)
	}
	return views, total, rows.Err()
}

// parseSupplyExtraTime 解析 extra 里的 RFC3339 字符串。
//
// 解析不了就当没有：这几个字段是观察期任务写的辅助信息，一个畸形的值不该让
// 整个看板 500——运营看到「没有观察期起点」会去查那个号，看到一个错误页不会。
func parseSupplyExtraTime(raw sql.NullString) *time.Time {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw.String))
	if err != nil {
		return nil
	}
	return &parsed
}

// parseSupplyExtraInt 解析 extra 里的计数。`->>` 把 jsonb 数字也吐成文本，所以走 Atoi。
func parseSupplyExtraInt(raw sql.NullString) int {
	if !raw.Valid {
		return 0
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(raw.String))
	if err != nil {
		return 0
	}
	return parsed
}

// ============================================================================
// 全站流水
// ============================================================================

const supplyAdminLedgerSelectSQL = `
SELECT l.id,
       l.user_id,
       COALESCE(u.email, ''),
       l.action,
       l.amount::double precision,
       l.request_id,
       l.account_id,
       l.source_user_id,
       l.basis_amount::double precision,
       l.share_ratio::double precision,
       l.frozen_until,
       l.available_after::double precision,
       l.frozen_after::double precision,
       l.history_after::double precision,
       l.remark,
       l.created_at
FROM supplier_credit_ledger l
LEFT JOIN users u ON u.id = l.user_id`

const supplyAdminLedgerCountSQL = `
SELECT COUNT(*)
FROM supplier_credit_ledger l`

// buildSupplyAdminLedgerWhere 拼全站流水的筛选条件。
//
// 刻意**不复用** buildSupplierLedgerWhere：那一个的 UserID > 0 判断是供给者侧的
// 安全边界（漏传就查不到自己的账），这一个的 UserID = 0 是合法的「看全站」。
// 共用一个函数就意味着有一天有人为了这边的需求把那个判断改掉。
func buildSupplyAdminLedgerWhere(filter service.SupplyAdminLedgerFilter) (string, []any) {
	var (
		clauses []string
		args    []any
	)
	if filter.UserID > 0 {
		args = append(args, filter.UserID)
		clauses = append(clauses, fmt.Sprintf("l.user_id = $%d", len(args)))
	}
	if action := strings.TrimSpace(filter.Action); action != "" {
		args = append(args, action)
		clauses = append(clauses, fmt.Sprintf("l.action = $%d", len(args)))
	}
	if filter.AccountID > 0 {
		args = append(args, filter.AccountID)
		clauses = append(clauses, fmt.Sprintf("l.account_id = $%d", len(args)))
	}
	if requestID := strings.TrimSpace(filter.RequestID); requestID != "" {
		args = append(args, requestID)
		clauses = append(clauses, fmt.Sprintf("l.request_id = $%d", len(args)))
	}
	if filter.StartAt != nil {
		args = append(args, *filter.StartAt)
		clauses = append(clauses, fmt.Sprintf("l.created_at >= $%d", len(args)))
	}
	if filter.EndAt != nil {
		args = append(args, *filter.EndAt)
		clauses = append(clauses, fmt.Sprintf("l.created_at <= $%d", len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "\nWHERE " + strings.Join(clauses, " AND "), args
}

func (r *supplierAdminRepository) ListLedger(ctx context.Context, filter service.SupplyAdminLedgerFilter) ([]service.SupplyAdminLedgerEntry, int64, error) {
	client := clientFromContext(ctx, r.client)
	where, args := buildSupplyAdminLedgerWhere(filter)

	total, err := scanInt64(ctx, client, supplyAdminLedgerCountSQL+where, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("count supply admin ledger: %w", err)
	}
	if total == 0 {
		return []service.SupplyAdminLedgerEntry{}, 0, nil
	}

	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	query := fmt.Sprintf("%s%s\nORDER BY l.id DESC\nLIMIT $%d OFFSET $%d",
		supplyAdminLedgerSelectSQL, where, len(args)-1, len(args))

	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list supply admin ledger: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]service.SupplyAdminLedgerEntry, 0, filter.PageSize)
	for rows.Next() {
		var (
			entry          service.SupplyAdminLedgerEntry
			requestID      sql.NullString
			accountID      sql.NullInt64
			sourceUserID   sql.NullInt64
			basisAmount    sql.NullFloat64
			shareRatio     sql.NullFloat64
			frozenUntil    sql.NullTime
			availableAfter sql.NullFloat64
			frozenAfter    sql.NullFloat64
			historyAfter   sql.NullFloat64
			remark         sql.NullString
		)
		if err := rows.Scan(
			&entry.ID, &entry.UserID, &entry.UserEmail, &entry.Action, &entry.Amount,
			&requestID, &accountID, &sourceUserID, &basisAmount, &shareRatio,
			&frozenUntil, &availableAfter, &frozenAfter, &historyAfter, &remark,
			&entry.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan supply admin ledger row: %w", err)
		}
		if requestID.Valid {
			entry.RequestID = &requestID.String
		}
		if accountID.Valid {
			entry.AccountID = &accountID.Int64
		}
		if sourceUserID.Valid {
			entry.SourceUserID = &sourceUserID.Int64
		}
		entry.BasisAmount = nullableFloat64Ptr(basisAmount)
		entry.ShareRatio = nullableFloat64Ptr(shareRatio)
		if frozenUntil.Valid {
			t := frozenUntil.Time
			entry.FrozenUntil = &t
		}
		entry.AvailableAfter = nullableFloat64Ptr(availableAfter)
		entry.FrozenAfter = nullableFloat64Ptr(frozenAfter)
		entry.HistoryAfter = nullableFloat64Ptr(historyAfter)
		if remark.Valid {
			entry.Remark = &remark.String
		}
		entries = append(entries, entry)
	}
	return entries, total, rows.Err()
}
