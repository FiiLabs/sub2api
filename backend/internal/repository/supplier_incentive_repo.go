// APEXONE-EXT: 双边市场——挂号奖励的 SQL 实现。
//
// 三条查询，没有新表：在线天数存在 accounts.extra 里，发放记录就是
// supplier_credit_ledger 里那几行 accrue。刻意不建表的理由是这个功能的两个状态
// 都已经有天然的归属地——「这个号在役了多久」是账号属性，「发过没发过」是流水，
// 再建一张表只会让同一件事有两个真相。
package repository

import (
	"context"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// supplierIncentiveCandidateDefaultLimit 单轮最多取多少个候选账号。
//
// 与观察期扫描不同，这里每个候选只是一次入账（不打上游），所以上限可以宽一些；
// 留着上限是为了让单轮的耗时有个上界——worker 有 2 分钟的超时，一轮扫出上万个
// 账号会让整轮超时，而超时的那一轮什么都发不出去。
const supplierIncentiveCandidateDefaultLimit = 500

type supplierIncentiveRepository struct {
	client *dbent.Client
}

// NewSupplierIncentiveRepository 构造挂号奖励仓储。
func NewSupplierIncentiveRepository(client *dbent.Client) service.SupplierIncentiveRepository {
	return &supplierIncentiveRepository{client: client}
}

// supplierIncentiveTickActiveDaysSQL 给所有在役供给号的在线天数 +1。
//
// # 为什么是一条 SQL 而不是「读出来、加一、写回去」
//
// 读-改-写有两个问题，而它们都不会报错，只会让数字悄悄不对：
//   - 并发：另一个写 extra 的路径（观察期探测就在写 probe_at）会覆盖掉这次累加；
//   - 重入：worker 一小时跑一轮，读到旧值的那一轮会把天数写回去。
//
// JSONB 的 `||` 是原子合并，配上 WHERE 里的 `last_day <> $1`，两个问题一起消失：
// 同一天第二次执行时受影响行数为 0，不是「加了又覆盖回来」。
//
// # 为什么 WHERE 里要有 schedulable
//
// 这是「在线」的定义。观察期里的号 supply_state 还不是 active、schedulable=false，
// 它没在供货；被熔断下线的号 schedulable 被置 false，它也没在供货。两者都不该涨天数。
// 反过来——**不清零**：不满足条件只是不出现在更新集里，days 原地不动，接回来接着涨。
//
// extra 的键名从 service 常量拼进来（同 supplierAccountListByStateSQL 的理由）：
// 漂移了不报错，只会让所有人的天数永远停在 0，一个完全静默的失效。
var supplierIncentiveTickActiveDaysSQL = fmt.Sprintf(`
UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object(
        '%[1]s',
        jsonb_build_object(
            'days',     COALESCE((extra->'%[1]s'->>'days')::int, 0) + 1,
            'last_day', $1::text
        )
    ),
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND owner_user_id IS NOT NULL
  AND schedulable = TRUE
  AND COALESCE(NULLIF(extra->>'%[2]s', ''), '%[3]s') = '%[4]s'
  AND COALESCE(extra->'%[1]s'->>'last_day', '') <> $1::text`,
	service.SupplyActiveDaysExtraKey,
	service.SupplyStateExtraKey,
	service.SupplyStatePendingReview,
	service.SupplyStateActive)

// TickActiveDays 给所有在役供给号的在线天数 +1，同一天重复调用不重复累加。
func (r *supplierIncentiveRepository) TickActiveDays(ctx context.Context, day string) (int64, error) {
	if day == "" {
		return 0, fmt.Errorf("tick active days: empty day")
	}
	result, err := r.client.ExecContext(ctx, supplierIncentiveTickActiveDaysSQL, day)
	if err != nil {
		return 0, fmt.Errorf("tick supply active days: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		// 驱动不报行数不是失败——天数已经加上了。返回 0 只会让日志少一个数字。
		return 0, nil
	}
	return affected, nil
}

// supplierIncentiveGrantStatsSQL 某一档已发份数，按人分组。
//
// 按人分组而不是只数一个总数：名额按账号计，防刷要按人计（见
// SupplyIncentiveGrantStats 的注释）。总数由调用方把各组相加得到，
// 省一次查询也省掉「两个数出自两条语句、中间隔着写入」的不一致。
//
// 走 `(action, request_id)` 那个部分唯一索引的前缀扫描。索引在默认 collation 下
// 不保证被 LIKE 前缀用上，但 ledger 目前是万行量级，全扫也是毫秒级；真长大了
// 给 request_id 补一个 text_pattern_ops 索引即可，不必现在为它加一张表。
const supplierIncentiveGrantStatsSQL = `
SELECT user_id, COUNT(*)
FROM supplier_credit_ledger
WHERE action = 'accrue'
  AND request_id LIKE $1 || '%'
GROUP BY user_id`

// GrantStats 某一档已用名额与按人明细。
func (r *supplierIncentiveRepository) GrantStats(
	ctx context.Context, requestPrefix string,
) (*service.SupplyIncentiveGrantStats, error) {
	if requestPrefix == "" {
		return nil, fmt.Errorf("grant stats: empty prefix")
	}
	rows, err := r.client.QueryContext(ctx, supplierIncentiveGrantStatsSQL, requestPrefix)
	if err != nil {
		return nil, fmt.Errorf("read incentive grant stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stats := &service.SupplyIncentiveGrantStats{ByUser: map[int64]int{}}
	for rows.Next() {
		var userID int64
		var n int
		if err := rows.Scan(&userID, &n); err != nil {
			return nil, fmt.Errorf("scan incentive grant stats: %w", err)
		}
		stats.ByUser[userID] = n
		stats.Total += n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate incentive grant stats: %w", err)
	}
	return stats, nil
}

// supplierIncentiveListCandidatesSQL 列出够格且未发过这一档的账号。
//
// 三处值得说明：
//
//   - `NOT EXISTS ... request_id = $3 || a.id::text` 是**等值**命中唯一索引，
//     不是前缀扫描。这条子查询会对每个候选跑一次，等值让它保持在索引查找的量级。
//   - `ORDER BY days DESC, a.id ASC` 就是对外宣称的「按接入先后分配」：天数多的
//     必然接入得早。用天数而不是 created_at 排序，是因为中途断线过的号确实该排在
//     同期未断线的号后面——它在线的时间本来就更短。
//   - `NOT (owner_user_id = ANY(COALESCE($5, '{}')))` 挡的是解绑重挂：上游订阅查重只看未删除的行，
//     而解绑会软删该行并抹掉 credentials，于是同一份订阅能换一个新 account id 再挂
//     一次。按账号计的名额对此无能为力，只有把拿满的人整个排除掉才挡得住。
//   - platform 那一条走「$2 是空串就不过滤」：program.platform 为空表示全平台共用
//     一个名额池。写成 OR 而不是在 Go 侧拼两条 SQL，是为了让「空 = 不限」这条语义
//     只有一个落点。
var supplierIncentiveListCandidatesSQL = fmt.Sprintf(`
SELECT a.id,
       a.owner_user_id,
       COALESCE((a.extra->'%[1]s'->>'days')::int, 0) AS active_days
FROM accounts a
WHERE a.deleted_at IS NULL
  AND a.owner_user_id IS NOT NULL
  AND a.schedulable = TRUE
  AND COALESCE(NULLIF(a.extra->>'%[2]s', ''), '%[3]s') = '%[4]s'
  AND COALESCE((a.extra->'%[1]s'->>'days')::int, 0) >= $1
  AND ($2 = '' OR a.platform = $2)
  AND NOT (a.owner_user_id = ANY(COALESCE($5::bigint[], ARRAY[]::bigint[])))
  AND NOT EXISTS (
        SELECT 1
        FROM supplier_credit_ledger l
        WHERE l.action = 'accrue'
          AND l.request_id = $3 || a.id::text
      )
ORDER BY active_days DESC, a.id ASC
LIMIT $4`,
	service.SupplyActiveDaysExtraKey,
	service.SupplyStateExtraKey,
	service.SupplyStatePendingReview,
	service.SupplyStateActive)

// ListCandidates 列出够格且未发过的账号，按在线天数降序。
func (r *supplierIncentiveRepository) ListCandidates(
	ctx context.Context,
	query service.SupplyIncentiveCandidateQuery,
) ([]service.SupplyIncentiveCandidate, error) {
	if query.MinActiveDays <= 0 || query.RequestPrefix == "" {
		return nil, nil
	}
	limit := query.Limit
	if limit <= 0 || limit > supplierIncentiveCandidateDefaultLimit {
		limit = supplierIncentiveCandidateDefaultLimit
	}

	// pq.Array 对 nil 切片产出的是 **NULL**，不是 '{}'。而 `x = ANY(NULL)` 求值为
	// NULL、`NOT NULL` 还是 NULL——WHERE 里的 NULL 等于假，于是「没有人被排除」
	// 这条最常见的路径会把**全部候选**一起筛掉，一个奖励也发不出去。
	// SQL 侧用 COALESCE 兜成空数组，这里就不必为 nil 再分一条语句。
	rows, err := r.client.QueryContext(ctx, supplierIncentiveListCandidatesSQL,
		query.MinActiveDays, query.Platform, query.RequestPrefix, limit,
		pq.Array(query.ExcludedUserIDs))
	if err != nil {
		return nil, fmt.Errorf("list incentive candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var candidates []service.SupplyIncentiveCandidate
	for rows.Next() {
		var c service.SupplyIncentiveCandidate
		if err := rows.Scan(&c.AccountID, &c.OwnerUserID, &c.ActiveDays); err != nil {
			return nil, fmt.Errorf("scan incentive candidate: %w", err)
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}
