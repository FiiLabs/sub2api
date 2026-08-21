// APEXONE-EXT: 双边市场——供给账号失效事件的 SQL 实现。
//
// 事件的判据、三个时刻的含义、以及「一个号同时只有一条未结事件」那条部分唯一索引，
// 全部写在 migrations/233_supplier_account_incidents.sql 的文件头。
//
// # 本文件唯一需要在这里说的事：开与关是一对互补的谓词
//
// supplierIncidentOpenSQL 的 WHERE 与 supplierIncidentResolveSQL 里那个 OR 串
// 必须永远是**同一个条件的正反两面**。它们分开写是因为一个是 INSERT..SELECT、
// 另一个是 UPDATE..WHERE IN，SQL 上没法共用；但只要有一条被改动而另一条没跟上，
// 系统就会进入一个自相矛盾的稳态：
//
//   - 开的条件比关的宽 → 同一个号被开了事件、下一轮立刻被关掉，再下一轮又开一条。
//     每五分钟一封"你的号坏了"的邮件，直到有人来问为止。
//   - 关的条件比开的宽 → 事件开出来就再也关不掉，运营的"当前坏了几个"永远只增。
//
// 所以两处都从同一批常量拼字面量，且两处的差异只允许出现在「账号行还在不在」
// 这一项上——那一项在 INSERT 侧天然为真（它就是从 accounts 里选出来的）。
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// supplierIncidentDefaultLimit 单轮开/关事件的批量上限。
//
// 有上限的理由与观察期扫描一样，但担心的不是上游流量而是**通知**：一次误判
// （比如上游全站故障把所有号同时打成 error）在没有上限时会变成一封给每个供给者的
// 群发邮件。分批只让它慢下来，不能阻止它——真正的闸是 notified_at 那一列
// （一次事件一封信），这里只是让运营有机会在第二轮之前看见日志。
const supplierIncidentDefaultLimit = 500

// supplierIncidentTopDefault 封禁率榜的默认长度。
const supplierIncidentTopDefault = 10

// supplierIncidentTopMax 封禁率榜的最大长度。
//
// 榜是给人看的，不是导出口径——要全量该走事件明细的分页接口。
const supplierIncidentTopMax = 100

type supplierIncidentRepository struct {
	client *dbent.Client
}

// NewSupplierIncidentRepository 构造失效事件仓储。
func NewSupplierIncidentRepository(client *dbent.Client) service.SupplierIncidentRepository {
	return &supplierIncidentRepository{client: client}
}

// ============================================================================
// 检测：开事件 / 关事件
// ============================================================================

// supplierIncidentBrokenExpr 是「这个号此刻算坏的」。
//
// 两点与直觉不同的地方：
//
//  1. **空状态算健康**。历史账号（本功能之前挂上来的）可能有一个空的 status，
//     把空串算成坏的会在功能上线的第一分钟给一批人发一封莫名其妙的邮件。
//     这与 supplier_lifecycle_service.go 的 shouldProbe 里
//     `account.Status != "" && account.Status != StatusActive` 是同一条规则。
//  2. **已下线（retired）的号不算**。号被主人暂停到期、或者被他自己解绑之后，
//     状态坏不坏都不是他需要被告知的事——他知道自己按了那个按钮。
//     排空中（draining）的号仍然算：它还在接单，坏了就是坏了。
var supplierIncidentBrokenExpr = fmt.Sprintf(
	`COALESCE(a.status, '') NOT IN ('', '%s') AND %s <> '%s'`,
	service.StatusActive, supplyStateExpr, service.SupplyStateRetired)

// supplierIncidentHealedExpr 是上面那条的反面，外加「账号行已经不在了」。
//
// 三支之外没有第四支；改动这里之前请读本文件顶部。
//
// 第一支 `a.id IS NULL` 逻辑上是冗余的：LEFT JOIN 补出来的空行里 a.owner_user_id
// 同样是 NULL，第二支已经把这种情况兜住了。它留在这里是为了让「号被删了」这件事
// 在表达式里有名字——否则下一个人读到的是「归属被摘的号要关事件」，很容易顺手把
// 删号那条路径的责任算到 JOIN 条件上去。代价是它不可能被变异测试单独钉住（去掉它
// 行为完全不变，见 §8）；真正守着删号路径的是 JOIN 上的 `a.deleted_at IS NULL`。
var supplierIncidentHealedExpr = fmt.Sprintf(
	`a.id IS NULL OR a.owner_user_id IS NULL OR COALESCE(a.status, '') IN ('', '%s') OR %s = '%s'`,
	service.StatusActive, supplyStateExpr, service.SupplyStateRetired)

// supplierIncidentOpenSQL 给每一个「坏着但还没有未结事件」的号开一条事件。
//
// 幂等交给数据库：`ON CONFLICT (account_id) WHERE resolved_at IS NULL DO NOTHING`
// 走的正是那条部分唯一索引。这不是一个「顺便」的写法——扫描每 5 分钟跑一次、
// 多实例下还可能同时跑，用「先 SELECT 有没有再 INSERT」会在两次往返之间开出两条。
//
// LIMIT 之外还有 ORDER BY a.id：没有它的话，一次超出批量上限的故障里，
// 每一轮扫到的是哪一批取决于 Postgres 当时的执行计划，于是有些号可能连续几轮
// 都排不上号。按 id 定序意味着分批是稳定推进的。
var supplierIncidentOpenSQL = fmt.Sprintf(`
INSERT INTO supplier_account_incidents (
    account_id, user_id, account_name, platform, status, error_message, detected_at, created_at
)
SELECT a.id,
       a.owner_user_id,
       LEFT(COALESCE(a.name, ''), 255),
       LEFT(COALESCE(a.platform, ''), 32),
       LEFT(COALESCE(a.status, ''), 32),
       LEFT(COALESCE(a.error_message, ''), %d),
       NOW(),
       NOW()
FROM accounts a
WHERE %s
  AND (%s)
ORDER BY a.id
LIMIT $1
ON CONFLICT (account_id) WHERE resolved_at IS NULL DO NOTHING`,
	service.SupplierIncidentErrorMaxLen, supplyAccountScope, supplierIncidentBrokenExpr)

// supplierIncidentResolveSQL 关掉所有「号已经好了或者已经不在了」的未结事件。
//
// LEFT JOIN 而不是 JOIN：账号被硬删之后 a.id 是 NULL，而那种事件恰恰必须被关掉——
// 用 INNER JOIN 的话它会永远留在"当前坏着"里，指向一个不存在的号。
var supplierIncidentResolveSQL = fmt.Sprintf(`
UPDATE supplier_account_incidents
SET resolved_at = NOW()
WHERE id IN (
    SELECT i.id
    FROM supplier_account_incidents i
    LEFT JOIN accounts a ON a.id = i.account_id AND a.deleted_at IS NULL
    WHERE i.resolved_at IS NULL
      AND (%s)
    ORDER BY i.id
    LIMIT $1
)`, supplierIncidentHealedExpr)

func supplierIncidentLimit(limit int) int {
	if limit <= 0 || limit > supplierIncidentDefaultLimit {
		return supplierIncidentDefaultLimit
	}
	return limit
}

func (r *supplierIncidentRepository) OpenIncidents(ctx context.Context, limit int) (int64, error) {
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, supplierIncidentOpenSQL, supplierIncidentLimit(limit))
	if err != nil {
		return 0, fmt.Errorf("open supply incidents: %w", err)
	}
	opened, err := result.RowsAffected()
	if err != nil {
		return 0, nil // 驱动不报行数不是错误，调用方只拿它记日志
	}
	return opened, nil
}

func (r *supplierIncidentRepository) ResolveIncidents(ctx context.Context, limit int) (int64, error) {
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, supplierIncidentResolveSQL, supplierIncidentLimit(limit))
	if err != nil {
		return 0, fmt.Errorf("resolve supply incidents: %w", err)
	}
	resolved, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return resolved, nil
}

// ============================================================================
// 通知
// ============================================================================

// supplierIncidentColumns 是事件行的列清单，三处查询共用。
const supplierIncidentColumns = `id, account_id, user_id, account_name, platform,
       status, error_message, detected_at, notified_at, resolved_at`

// supplierIncidentPendingNoticeSQL 取还没发过信的未结事件。
//
// 按 detected_at 升序：积压时先通知最早出事的那个人——他的号已经停了最久。
var supplierIncidentPendingNoticeSQL = fmt.Sprintf(`
SELECT %s
FROM supplier_account_incidents
WHERE resolved_at IS NULL
  AND notified_at IS NULL
ORDER BY detected_at ASC, id ASC
LIMIT $1`, supplierIncidentColumns)

// supplierIncidentMarkNotifiedSQL 记下信已经发出去了。
//
// `AND notified_at IS NULL` 不是多余的：两个实例同时跑到这里时，第二条 UPDATE
// 会影响 0 行。虽然此刻两封信都已经发出去了（这条闸拦不住那个），但它保证
// notified_at 是**第一封**信的时刻，而不是最后一封的。
const supplierIncidentMarkNotifiedSQL = `
UPDATE supplier_account_incidents
SET notified_at = NOW()
WHERE id = $1 AND notified_at IS NULL`

func (r *supplierIncidentRepository) ListPendingNotice(ctx context.Context, limit int) ([]service.SupplierAccountIncident, error) {
	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, supplierIncidentPendingNoticeSQL, supplierIncidentLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list pending incident notices: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanSupplierIncidents(rows)
}

func (r *supplierIncidentRepository) MarkNotified(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	if _, err := client.ExecContext(ctx, supplierIncidentMarkNotifiedSQL, id); err != nil {
		return fmt.Errorf("mark incident notified: %w", err)
	}
	return nil
}

// scanSupplierIncidents 把结果集摊成事件切片。列顺序必须与 supplierIncidentColumns 一致。
func scanSupplierIncidents(rows *sql.Rows) ([]service.SupplierAccountIncident, error) {
	var incidents []service.SupplierAccountIncident
	for rows.Next() {
		var (
			incident   service.SupplierAccountIncident
			notifiedAt sql.NullTime
			resolvedAt sql.NullTime
		)
		if err := rows.Scan(
			&incident.ID, &incident.AccountID, &incident.UserID,
			&incident.AccountName, &incident.Platform,
			&incident.Status, &incident.ErrorMessage,
			&incident.DetectedAt, &notifiedAt, &resolvedAt,
		); err != nil {
			return nil, fmt.Errorf("scan supply incident row: %w", err)
		}
		if notifiedAt.Valid {
			t := notifiedAt.Time
			incident.NotifiedAt = &t
		}
		if resolvedAt.Valid {
			t := resolvedAt.Time
			incident.ResolvedAt = &t
		}
		incidents = append(incidents, incident)
	}
	return incidents, rows.Err()
}

// ============================================================================
// 明细列表
// ============================================================================

var supplierIncidentListSQL = fmt.Sprintf(`
SELECT %s
FROM supplier_account_incidents
WHERE 1 = 1`, supplierIncidentColumns)

const supplierIncidentCountSQL = `
SELECT COUNT(*)
FROM supplier_account_incidents
WHERE 1 = 1`

// buildSupplierIncidentWhere 拼筛选条件。全部走占位符。
//
// 时间筛的是 detected_at 而不是 resolved_at：运营问的是「这段时间坏了哪些号」，
// 按恢复时刻筛会漏掉所有还没恢复的——那正是他最想看的那一批。
func buildSupplierIncidentWhere(filter service.SupplierIncidentFilter) (string, []any) {
	var (
		clauses []string
		args    []any
	)
	if filter.UserID > 0 {
		args = append(args, filter.UserID)
		clauses = append(clauses, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if filter.AccountID > 0 {
		args = append(args, filter.AccountID)
		clauses = append(clauses, fmt.Sprintf("account_id = $%d", len(args)))
	}
	if filter.OpenOnly {
		clauses = append(clauses, "resolved_at IS NULL")
	}
	if filter.StartAt != nil {
		args = append(args, *filter.StartAt)
		clauses = append(clauses, fmt.Sprintf("detected_at >= $%d", len(args)))
	}
	if filter.EndAt != nil {
		args = append(args, *filter.EndAt)
		clauses = append(clauses, fmt.Sprintf("detected_at <= $%d", len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " AND " + strings.Join(clauses, " AND "), args
}

func (r *supplierIncidentRepository) List(ctx context.Context, filter service.SupplierIncidentFilter) ([]service.SupplierAccountIncident, int64, error) {
	client := clientFromContext(ctx, r.client)
	where, args := buildSupplierIncidentWhere(filter)

	total, err := scanInt64(ctx, client, supplierIncidentCountSQL+where, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("count supply incidents: %w", err)
	}
	if total == 0 {
		return []service.SupplierAccountIncident{}, 0, nil
	}

	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	// 未结的排在最前，然后按发现时刻倒序。运营巡检时最想先看到的是还没好的那些，
	// 而在一个混排的列表里它们会散落在各页——那时这个接口的默认视图就没用了。
	query := fmt.Sprintf("%s%s\nORDER BY (resolved_at IS NULL) DESC, detected_at DESC, id DESC\nLIMIT $%d OFFSET $%d",
		supplierIncidentListSQL, where, len(args)-1, len(args))

	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list supply incidents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	incidents, err := scanSupplierIncidents(rows)
	if err != nil {
		return nil, 0, err
	}
	if incidents == nil {
		incidents = []service.SupplierAccountIncident{}
	}
	return incidents, total, nil
}

// ============================================================================
// 封禁率报表
// ============================================================================

// supplierIncidentSummaryCountsSQL 顶部那四个数，一次查完。
//
// 三个 FILTER 走同一次扫描：拆成三条语句会让它们落在三个不同的时刻上，
// 于是"窗口内开了 5 条、关了 6 条、当前还开着 0 条"这种自相矛盾的一屏是可能出现的。
const supplierIncidentSummaryCountsSQL = `
SELECT COUNT(*) FILTER (WHERE detected_at >= NOW() - make_interval(days => $1)),
       COUNT(*) FILTER (WHERE resolved_at IS NOT NULL AND resolved_at >= NOW() - make_interval(days => $1)),
       COUNT(*) FILTER (WHERE resolved_at IS NULL),
       COUNT(DISTINCT user_id) FILTER (WHERE detected_at >= NOW() - make_interval(days => $1))
FROM supplier_account_incidents`

var supplierIncidentSummaryAccountsSQL = fmt.Sprintf(`
SELECT COUNT(*)
FROM accounts a
WHERE %s`, supplyAccountScope)

// supplierIncidentTopSQL 是榜单。
//
// 三个 CTE 各回答一个问题，然后左连起来——open_counts 与 owned 都**不带窗口条件**，
// 理由写在 service.SupplierIncidentRate 的注释里：一个坏了三个月的号不该因为
// 它是在窗口之前坏的就从"现在还有几个坏着"里消失。
//
// 驱动表是 windowed（窗口内出过事的人），所以榜上不会出现零事件的行——
// 那是一张"谁在坏"的榜，不是全体供给者名册。
var supplierIncidentTopSQL = fmt.Sprintf(`
WITH windowed AS (
    SELECT user_id, COUNT(*) AS incidents, MAX(detected_at) AS last_detected_at
    FROM supplier_account_incidents
    WHERE detected_at >= NOW() - make_interval(days => $1)
    GROUP BY user_id
), open_counts AS (
    SELECT user_id, COUNT(*) AS open_incidents
    FROM supplier_account_incidents
    WHERE resolved_at IS NULL
    GROUP BY user_id
), owned AS (
    SELECT a.owner_user_id AS user_id, COUNT(*) AS accounts
    FROM accounts a
    WHERE %s
    GROUP BY a.owner_user_id
)
SELECT w.user_id,
       COALESCE(u.email, ''),
       COALESCE(u.username, ''),
       COALESCE(o.accounts, 0),
       w.incidents,
       COALESCE(c.open_incidents, 0),
       w.last_detected_at
FROM windowed w
LEFT JOIN users u ON u.id = w.user_id
LEFT JOIN owned o ON o.user_id = w.user_id
LEFT JOIN open_counts c ON c.user_id = w.user_id
ORDER BY w.incidents DESC, w.last_detected_at DESC, w.user_id ASC
LIMIT $2`, supplyAccountScope)

func (r *supplierIncidentRepository) Summary(ctx context.Context, windowDays, topN int) (*service.SupplierIncidentSummary, error) {
	client := clientFromContext(ctx, r.client)
	if topN <= 0 {
		topN = supplierIncidentTopDefault
	}
	if topN > supplierIncidentTopMax {
		topN = supplierIncidentTopMax
	}

	summary := &service.SupplierIncidentSummary{WindowDays: windowDays}

	// 四个数一次查完（见 supplierIncidentSummaryCountsSQL 的说明）。
	// 用 QueryContext 而不是 QueryRowContext：ent 的 client 上没有后者。
	rows, err := client.QueryContext(ctx, supplierIncidentSummaryCountsSQL, windowDays)
	if err != nil {
		return nil, fmt.Errorf("count supply incident summary: %w", err)
	}
	if rows.Next() {
		if err := rows.Scan(&summary.Opened, &summary.Resolved, &summary.Open, &summary.Suppliers); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan supply incident summary: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("scan supply incident summary: %w", err)
	}
	_ = rows.Close()

	accounts, err := scanInt64(ctx, client, supplierIncidentSummaryAccountsSQL)
	if err != nil {
		return nil, fmt.Errorf("count supply accounts for incident summary: %w", err)
	}
	summary.Accounts = accounts

	top, err := client.QueryContext(ctx, supplierIncidentTopSQL, windowDays, topN)
	if err != nil {
		return nil, fmt.Errorf("list supply incident top suppliers: %w", err)
	}
	defer func() { _ = top.Close() }()

	summary.Top = []service.SupplierIncidentRate{}
	for top.Next() {
		var (
			entry          service.SupplierIncidentRate
			lastDetectedAt sql.NullTime
		)
		if err := top.Scan(&entry.UserID, &entry.Email, &entry.Username,
			&entry.Accounts, &entry.Incidents, &entry.OpenIncidents, &lastDetectedAt); err != nil {
			return nil, fmt.Errorf("scan supply incident top row: %w", err)
		}
		if lastDetectedAt.Valid {
			t := lastDetectedAt.Time
			entry.LastDetectedAt = &t
		}
		// 比率在 Go 侧算而不是在 SQL 里：SQL 侧要写成
		// `incidents::float / NULLIF(accounts, 0)`，那个 NULLIF 漏掉一次就是一次
		// 除零错误——而它发生在运营点开报表的时候，表现是整页 500。
		if entry.Accounts > 0 {
			entry.Rate = float64(entry.Incidents) / float64(entry.Accounts)
		}
		summary.Top = append(summary.Top, entry)
	}
	return summary, top.Err()
}

// ============================================================================
// 熔断判据
// ============================================================================

const supplierIncidentCountRecentSQL = `
SELECT COUNT(*)
FROM supplier_account_incidents
WHERE user_id = $1 AND detected_at >= $2`

func (r *supplierIncidentRepository) CountRecentByUser(ctx context.Context, userID int64, since time.Time) (int, error) {
	client := clientFromContext(ctx, r.client)
	count, err := scanInt64(ctx, client, supplierIncidentCountRecentSQL, userID, since)
	if err != nil {
		return 0, fmt.Errorf("count recent supply incidents: %w", err)
	}
	return int(count), nil
}
