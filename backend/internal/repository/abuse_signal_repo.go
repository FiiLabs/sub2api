// APEXONE-EXT: 异常使用模式检测的扫描查询。
//
// 判据与阈值的含义写在 internal/service/abuse_detection.go 的文件头，那里是这件事
// 的权威说明；本文件只放 SQL。
package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// abuseSignalScanQuery 按用户聚合观察窗内的流量画像。
//
// # 为什么过滤下推到 SQL
//
// HAVING 把「请求数不够」的用户挡在数据库里。全表拉回 Go 再筛，会随用量线性变慢；
// 而这是个周期任务，慢下去不会有人立刻发现——直到某天它跑不完一轮。
//
// # 两个分母的 NULLIF 保护
//
// cache_hit_ratio 的分母是「总输入量」，avg_input 的分母是请求数。前者在一批
// 全是空请求的极端情况下可以为 0，后者被 HAVING 保证 > 0 但仍然写上——一个
// 除零会让整轮扫描报错退出，而它防的只是一行没有意义的读数。
//
// # 为什么 cache_creation 算进分母
//
// 缓存写入也是「这次请求真的送上去的内容」。只用 cache_read/(input+cache_read)
// 会让一个正在**建立**缓存的长会话（第一轮，写多读少）看起来命中率极低，
// 而那恰恰是最不该被判可疑的时刻。
const abuseSignalScanQuery = `
	SELECT
		ul.user_id,
		COUNT(*) AS requests,
		COALESCE(SUM(ul.cache_read_tokens), 0)::float8
			/ NULLIF(COALESCE(SUM(ul.input_tokens + ul.cache_read_tokens + ul.cache_creation_tokens), 0), 0)
			AS cache_hit_ratio,
		COALESCE(SUM(ul.input_tokens), 0)::float8 / NULLIF(COUNT(*), 0) AS avg_input_tokens,
		COUNT(DISTINCT ul.session_id) AS distinct_sessions,
		COALESCE(SUM(ul.total_cost), 0) AS standard_cost
	FROM usage_logs ul
	WHERE ul.created_at >= $1
	  AND ul.user_id IS NOT NULL
	GROUP BY ul.user_id
	HAVING COUNT(*) >= $2
	ORDER BY COUNT(*) DESC
	LIMIT $3
`

// ScanAbuseSignals 返回观察窗内请求数达标的用户画像。
//
// 只读，不做任何判定——「可疑与否」由 service 层的阈值决定，仓储层不该知道
// 那件事，否则调阈值就要改 SQL。
func (r *usageLogRepository) ScanAbuseSignals(ctx context.Context, since time.Time, minRequests int64, limit int) ([]service.AbuseSignal, error) {
	if limit <= 0 {
		limit = 100
	}
	if minRequests <= 0 {
		minRequests = 1
	}

	rows, err := r.sql.QueryContext(ctx, abuseSignalScanQuery, since, minRequests, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	signals := make([]service.AbuseSignal, 0, 16)
	for rows.Next() {
		var sig service.AbuseSignal
		// 两个比值用可空类型接：NULLIF 在分母为 0 时给出 NULL，
		// 直接扫进 float64 会报错并中断整轮扫描。
		var ratio, avgInput *float64
		if err := rows.Scan(
			&sig.UserID,
			&sig.Requests,
			&ratio,
			&avgInput,
			&sig.DistinctSessions,
			&sig.StandardCost,
		); err != nil {
			return nil, err
		}
		if ratio != nil {
			sig.CacheHitRatio = *ratio
		}
		if avgInput != nil {
			sig.AvgInputTokens = *avgInput
		}
		sig.WindowStart = since
		signals = append(signals, sig)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return signals, nil
}
