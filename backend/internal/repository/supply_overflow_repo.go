// APEXONE-EXT: 双边市场——溢出日配额与日计数的 SQL 实现。
//
// 只有两条语句，但第一条的形状是这套闸门唯一的正确性来源，改动前先读
// service/supply_overflow_budget.go 顶部那三条约束。
package repository

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ============================================================================
// SQL 常量：抽出来让单测能直接对语句做断言，不必起数据库。
// ============================================================================

// supplyOverflowConsumeSQL 原子地「判定配额 + 计数」。
//
// 三种结果，靠「有没有返回行」区分：
//   - 当日无记录  → INSERT 分支成功，返回 1（第一次溢出总是允许的，配额至少为 1；
//     limit 传进来时已经由 service 侧翻译过，<= 0 是 MaxInt64）
//   - 有记录且未满 → DO UPDATE 分支的 WHERE 成立，计数 +1 并返回新值
//   - 有记录且已满 → DO UPDATE 分支的 WHERE 不成立，**不返回任何行**，且不写入
//
// 最后那条是关键：ON CONFLICT DO UPDATE 上的 WHERE 让「判定」和「加一」发生在同一个
// 行锁里，并发的耗尽请求不可能都读到未满然后各自放行。写成「先 SELECT 再 UPDATE」的话，
// 配额设 100 时几十个并发请求会一起超发。
//
// 注意 INSERT 分支不受 limit 约束：当日第一次溢出时表里还没有行，没有可比的计数。
// 这意味着 limit 实际的下限是 1（配成 0 表示不限量，不是禁止溢出——禁止溢出请关总开关）。
const supplyOverflowConsumeSQL = `
INSERT INTO supply_overflow_daily (day, overflow_count, denied_count)
VALUES ($1, 1, 0)
ON CONFLICT (day) DO UPDATE
SET overflow_count = supply_overflow_daily.overflow_count + 1,
    updated_at = NOW()
WHERE supply_overflow_daily.overflow_count < $2
RETURNING overflow_count`

// supplyOverflowDenySQL 记一次「配额已满、没有溢出」。
//
// 与 overflow_count 分开计数，理由见迁移 227 里的注释：合在一起的话同一个数字既可能
// 表示花了钱也可能表示省了钱。这条语句失败不影响判定结果（已经判定为拒绝了），
// 所以调用方只记日志、不改变返回值。
const supplyOverflowDenySQL = `
INSERT INTO supply_overflow_daily (day, overflow_count, denied_count)
VALUES ($1, 0, 1)
ON CONFLICT (day) DO UPDATE
SET denied_count = supply_overflow_daily.denied_count + 1,
    updated_at = NOW()`

// supplyOverflowExhaustedSQL 记一次「溢出了，但兜底池也空了」（迁移 236）。
//
// 形状与 denySQL 相同、语义正交：那个数的是「配额把我们拦住了」，这个数的是
// 「我们放行了，但没货」。前者是平台在省钱，后者是**用户拿到了一个错误**。
const supplyOverflowExhaustedSQL = `
INSERT INTO supply_overflow_daily (day, overflow_count, denied_count, exhausted_count)
VALUES ($1, 0, 0, 1)
ON CONFLICT (day) DO UPDATE
SET exhausted_count = supply_overflow_daily.exhausted_count + 1,
    updated_at = NOW()`

const supplyOverflowUsageSQL = `
SELECT overflow_count, denied_count, exhausted_count
FROM supply_overflow_daily
WHERE day = $1`

type supplyOverflowRepository struct {
	client *dbent.Client
}

// NewSupplyOverflowCounter 构造溢出日计数仓储。
func NewSupplyOverflowCounter(client *dbent.Client) service.SupplyOverflowCounter {
	return &supplyOverflowRepository{client: client}
}

// TryConsumeDailyOverflow 见 service.SupplyOverflowCounter。
//
// 返回 error 时调用方按 fail-closed 处理（不溢出）——所以这里**不吞任何错误**，
// 只有 sql.ErrNoRows 被翻译成「配额已满」这个正常结果。
func (r *supplyOverflowRepository) TryConsumeDailyOverflow(ctx context.Context, day time.Time, limit int) (bool, error) {
	if r == nil || r.client == nil {
		return false, fmt.Errorf("supply overflow counter unavailable")
	}

	dayKey := supplyOverflowDayKey(day)
	rows, err := r.client.QueryContext(ctx, supplyOverflowConsumeSQL, dayKey, supplyOverflowLimitBound(limit))
	if err != nil {
		return false, fmt.Errorf("consume supply overflow budget: %w", err)
	}
	allowed := rows.Next()
	if allowed {
		// 返回值本身用不上（配额判定已经在 SQL 里做完了），但必须 Scan：
		// 不消费掉这一行，某些驱动会把它留在连接上。
		var count int64
		if scanErr := rows.Scan(&count); scanErr != nil {
			_ = rows.Close()
			return false, fmt.Errorf("scan supply overflow budget: %w", scanErr)
		}
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if rowsErr != nil {
		return false, fmt.Errorf("consume supply overflow budget: %w", rowsErr)
	}
	if allowed {
		return true, nil
	}

	// 配额已满。记一次 denied 供运营看，写不进去也不改变判定——判定已经做完了，
	// 因为一次统计写失败而放行才是错的那一边。
	if _, denyErr := r.client.ExecContext(ctx, supplyOverflowDenySQL, dayKey); denyErr != nil {
		slog.Warn("[SupplyPool] failed to record a denied overflow", "error", denyErr, "day", dayKey)
	}
	return false, nil
}

// GetDailyOverflowUsage 见 service.SupplyOverflowCounter。当日无记录返回零值而非错误。
func (r *supplyOverflowRepository) GetDailyOverflowUsage(ctx context.Context, day time.Time) (*service.SupplyOverflowUsage, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("supply overflow counter unavailable")
	}

	dayKey := supplyOverflowDayKey(day)
	usage := &service.SupplyOverflowUsage{Day: dayKey}

	rows, err := r.client.QueryContext(ctx, supplyOverflowUsageSQL, dayKey)
	if err != nil {
		return nil, fmt.Errorf("read supply overflow usage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// 当日无记录 = 今天还没溢出过，返回零值而不是错误。
	if rows.Next() {
		if err := rows.Scan(&usage.OverflowCount, &usage.DeniedCount, &usage.ExhaustedCount); err != nil {
			return nil, fmt.Errorf("scan supply overflow usage: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read supply overflow usage: %w", err)
	}
	return usage, nil
}

// RecordOverflowExhausted 见 service.SupplyOverflowCounter。
func (r *supplyOverflowRepository) RecordOverflowExhausted(ctx context.Context, day time.Time) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("supply overflow counter unavailable")
	}
	if _, err := r.client.ExecContext(ctx, supplyOverflowExhaustedSQL, supplyOverflowDayKey(day)); err != nil {
		return fmt.Errorf("record exhausted overflow: %w", err)
	}
	return nil
}

// supplyOverflowDayKey 取平台时区下的自然日。
//
// 传字符串而不是 time.Time 给驱动：day 列是 DATE，交给驱动去截断的话，
// 截断用的是 time.Time 自带的时区，而调用方传进来的已经是平台时区的时刻——
// 这里显式格式化，「今天」是哪一天就不依赖驱动的行为。
func supplyOverflowDayKey(day time.Time) string {
	return day.Format("2006-01-02")
}

// supplyOverflowLimitBound 把 limit <= 0（不限量）翻译成 SQL 能比的上界。
func supplyOverflowLimitBound(limit int) int64 {
	if limit <= 0 {
		return math.MaxInt64
	}
	return int64(limit)
}
