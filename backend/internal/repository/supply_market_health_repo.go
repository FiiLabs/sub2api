// APEXONE-EXT: 双边市场——定价健康度的聚合查询。
//
// 两条语句：一条汇总全站的钱，一条列供给账号的产出。
//
// # 为什么不是一条
//
// 两条语句的**分组粒度不同**（一条不分组、一条按账号分组），合成一条要么用窗口
// 函数把汇总值贴到每一行上（把 N 行的数据传 N 遍），要么用 GROUP BY ROLLUP
// （让扫描出来的结果里混着两种形状的行，读侧要靠判空区分）。两者都是为了省一次
// 往返而牺牲可读性，而这个接口一天被调用几十次。
//
// # 窗口用 created_at >= now() - interval
//
// 不用 date_trunc 按自然月：定价参数的观察窗口是「最近 N 天」这个滑动概念，
// 按自然月会让每月 1 号的读数塌缩成一天的数据，而那恰恰是运营最想看一眼的时候。
package repository

import (
	"context"
	"database/sql"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// supplyHealthTotalsSQL 汇总窗口内的钱。
//
// 三个 SUM 各自的口径要分清楚，混了就是把营收虚报几倍：
//   - total_cost   = 官方牌价等值（倍率**之前**）
//   - actual_cost  = 消费者实付（倍率**之后**），这才是营收
//   - 兜底那一支靠 owner_user_id IS NULL 切分——平台自有账号没有归属人
//
// LEFT JOIN 而不是 INNER：账号被硬删之后它服务过的流水仍然是营收的一部分，
// INNER 会让那部分钱从报表里凭空消失。join 不上的行 owner_user_id 为 NULL，
// 会被算进「兜底」——这是可接受的偏差（硬删账号是罕见的运维动作），
// 且方向安全：它高估兜底占比，而兜底占比高触发的建议是"去拉供给者"。
const supplyHealthTotalsSQL = `
SELECT
    COALESCE(SUM(u.total_cost), 0)                                              AS list_value,
    COALESCE(SUM(u.actual_cost), 0)                                             AS revenue,
    COALESCE(SUM(u.total_cost) FILTER (WHERE a.owner_user_id IS NULL), 0)       AS overflow_list_value
FROM usage_logs u
LEFT JOIN accounts a ON a.id = u.account_id
WHERE u.created_at >= NOW() - ($1 || ' days')::interval`

// supplyHealthPayoutSQL 汇总窗口内付出去的分成，以及实际生效的分成比例。
//
// 只数 accrue：thaw 是同一笔钱在冻结区与可用区之间搬家，withdraw 是它离开平台，
// 把它们算进"付给供给者多少"会把同一笔钱数两到三遍。
//
// 实际分成从 amount ÷ basis_amount 反推而不是读 share_ratio 列的平均值：
// 后者是把每笔的比例做算术平均，一笔一分钱的和一笔一百块的权重相同，
// 那个数不等于「这个窗口里平台实际让出去的比例」。
const supplyHealthPayoutSQL = `
SELECT
    COALESCE(SUM(amount), 0)       AS payout,
    COALESCE(SUM(basis_amount), 0) AS basis
FROM supplier_credit_ledger
WHERE action = 'accrue'
  AND created_at >= NOW() - ($1 || ' days')::interval`

// supplyHealthAccountsSQL 列每个**他人挂的**账号在窗口内的产出。
//
// INNER JOIN + owner_user_id IS NOT NULL：这张榜回答的是「供给者赚得到钱吗」，
// 平台自有账号不属于任何供给者，混进来会把中位数拉偏——而中位数正是用来
// 证伪产能假设的那个数。
//
// supplier_earned 用 LEFT JOIN 子查询而不是再 join 一次 ledger：ledger 与
// usage_logs 是两条独立的写路径（同一次计费事务里的两张表），直接 join 会在
// 任一侧缺行时丢掉另一侧的数据。子查询让"有用量但还没入账"的账号仍然上榜——
// 那种账号恰恰是要被看见的（多半是自供自用被防套利护栏挡掉了）。
const supplyHealthAccountsSQL = `
SELECT
    a.id,
    a.name,
    a.owner_user_id,
    COALESCE(SUM(u.total_cost), 0) AS list_value,
    COUNT(*)                       AS requests,
    COALESCE((
        SELECT SUM(l.amount)
        FROM supplier_credit_ledger l
        WHERE l.account_id = a.id
          AND l.action = 'accrue'
          AND l.created_at >= NOW() - ($1 || ' days')::interval
    ), 0)                          AS supplier_earned
FROM usage_logs u
JOIN accounts a ON a.id = u.account_id
WHERE u.created_at >= NOW() - ($1 || ' days')::interval
  AND a.owner_user_id IS NOT NULL
  AND a.deleted_at IS NULL
GROUP BY a.id, a.name, a.owner_user_id
ORDER BY list_value DESC
LIMIT 200`

// supplyHealthDaysPerMonth 折月换算的分母。见 scanAccounts 里为什么是 30。
const supplyHealthDaysPerMonth = 30.0

type supplyMarketHealthRepository struct {
	client *dbent.Client
}

// NewSupplyMarketHealthRepository 构造健康度读侧仓储。
func NewSupplyMarketHealthRepository(client *dbent.Client) service.SupplyMarketHealthRepository {
	return &supplyMarketHealthRepository{client: client}
}

// Aggregate 见 service.SupplyMarketHealthRepository。
func (r *supplyMarketHealthRepository) Aggregate(ctx context.Context, windowDays int) (*service.SupplyMarketHealth, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("supply market health repository unavailable")
	}
	// 参数以字符串传给 interval 拼接：Postgres 不接受把绑定参数直接放进
	// interval 字面量，而 `($1 || ' days')::interval` 是官方推荐的等价写法。
	// windowDays 已由 service 层夹在 [1,90]，不存在注入面。
	window := fmt.Sprintf("%d", windowDays)

	health := &service.SupplyMarketHealth{}
	if err := r.scanTotals(ctx, window, health); err != nil {
		return nil, err
	}
	if err := r.scanPayout(ctx, window, health); err != nil {
		return nil, err
	}
	if err := r.scanAccounts(ctx, window, windowDays, health); err != nil {
		return nil, err
	}
	return health, nil
}

func (r *supplyMarketHealthRepository) scanTotals(ctx context.Context, window string, health *service.SupplyMarketHealth) error {
	rows, err := r.client.QueryContext(ctx, supplyHealthTotalsSQL, window)
	if err != nil {
		return fmt.Errorf("aggregate supply health totals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		if err := rows.Scan(&health.ListValue, &health.Revenue, &health.OverflowListValue); err != nil {
			return fmt.Errorf("scan supply health totals: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("aggregate supply health totals: %w", err)
	}
	return nil
}

func (r *supplyMarketHealthRepository) scanPayout(ctx context.Context, window string, health *service.SupplyMarketHealth) error {
	rows, err := r.client.QueryContext(ctx, supplyHealthPayoutSQL, window)
	if err != nil {
		return fmt.Errorf("aggregate supply health payout: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var basis float64
	if rows.Next() {
		if err := rows.Scan(&health.SupplierPayout, &basis); err != nil {
			return fmt.Errorf("scan supply health payout: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("aggregate supply health payout: %w", err)
	}
	// 在这里算而不是交给服务层：basis 只在这条语句里出现，把它一路传出去
	// 只为了做一次除法，等于给 SupplyMarketHealth 加一个没人看的字段。
	if basis != 0 {
		health.EffectiveShare = health.SupplierPayout / basis
	}
	return nil
}

func (r *supplyMarketHealthRepository) scanAccounts(
	ctx context.Context, window string, windowDays int, health *service.SupplyMarketHealth,
) error {
	rows, err := r.client.QueryContext(ctx, supplyHealthAccountsSQL, window)
	if err != nil {
		return fmt.Errorf("aggregate supply health accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	accounts := []service.SupplyAccountOutput{}
	owners := map[int64]struct{}{}
	for rows.Next() {
		var (
			out   service.SupplyAccountOutput
			owner sql.NullInt64
		)
		if err := rows.Scan(&out.AccountID, &out.Name, &owner, &out.ListValue, &out.Requests, &out.SupplierEarned); err != nil {
			return fmt.Errorf("scan supply health account: %w", err)
		}
		out.OwnerUserID = owner.Int64
		// 折月：窗口 7 天时乘 30/7，窗口 30 天时原样。让不同窗口下的读数
		// 能和同一个产能估算（$3000/月）比，否则切一次窗口就要在脑子里换算一次。
		//
		// 分母取 30 而不是自然月长度：这个数是拿去和产能估算比的，
		// 而那个估算本身就按 30 天算。两边用同一个分母，比较才有意义。
		if windowDays > 0 {
			out.MonthlyOutput = out.ListValue * (supplyHealthDaysPerMonth / float64(windowDays))
		}
		accounts = append(accounts, out)
		if owner.Valid && owner.Int64 > 0 {
			owners[owner.Int64] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("aggregate supply health accounts: %w", err)
	}

	health.SupplyAccounts = accounts
	health.SupplierCount = len(owners)
	return nil
}
