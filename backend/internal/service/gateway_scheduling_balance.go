// APEXONE-EXT: 双边市场——新会话的产出均衡。
//
// # 要修的是什么
//
// 生产上量过一次（2026-09-02，6 个在役共享号，priority 与并发完全相同）：
//
//	#3  接单 6299 次（91%）→ 被打到限流，停摆 3 天
//	#6  接单 0 次（整个生命周期）
//	#4  接单 1 次
//
// 直觉上这像是负载均衡坏了，其实不是。既有的分层选择（优先级 → 负载率 → LRU）
// 在**会话**这个粒度上是公平的：每天约 10 个新会话，LRU 会把它们轮流发给各个号。
//
// 真正的缺口在于**会话之间的体量差着三个数量级**：43 个会话里最大的一个跑了 1,269
// 次请求、第二个 613 次，而它们都因为粘性钉在同一个号上。LRU 对这件事**没有记忆**——
// 它只看「谁最久没被碰过」，而那个刚吃下上千次请求的号只要闲几分钟就又排到最前面。
//
// 于是会话数很均匀、收益差十倍。而共享者看到的是「我挂了一个月，颗粒无收」。
//
// # 为什么判据是「今日官方牌价消耗」而不是「今日接单数」
//
// 共享者的收益正比于用量，不正比于请求数（一次 9 万 token 的请求和一次 500 token
// 的请求都算「一单」）。要均衡的是收益，判据就得是用量。
//
// 而且这个数已经有人在算了：供给者每日上限那道闸读的就是同一个口径、同一个窗口
// （UTC 零点起、usage_logs.total_cost 之和），连批量预取都是现成的。这里复用它，
// 不引入第二个「今天用了多少」的定义——两个定义迟早会漂移，而漂移的表现是
// 「上限说我用满了，均衡说我还没开张」。
//
// # 为什么分档而不是严格选最小
//
// 这个判据带 60 秒缓存（每请求一次聚合查询在热路径上不可接受）。严格「选最小」
// 会让缓存刷新前的所有新会话一起涌向同一个号——把一种不均衡换成另一种。
//
// 分档之后，「最少」放宽成「最少的那一档」，档内交回给既有的 LRU 打散。带宽默认
// $5：比单次请求的量级大得多，又远小于一个号的日产能，所以它既不会被噪声抖动，
// 也不会把真正的差距抹平。
//
// # 不碰粘性会话
//
// 这一层只在 Layer 2 生效，而粘性命中在 Layer 1.5 就返回了。中途换号会毁掉
// 上游的缓存命中——那是共享者和消费者一起承担的成本，不能为了均衡去换。
package service

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	// schedulingBalanceDefaultBandUSD 分档带宽默认值（美元，官方牌价）。
	schedulingBalanceDefaultBandUSD = 5.0
	// schedulingBalanceCacheTTL 今日产出的缓存时长。
	//
	// 均衡是个「大方向对就行」的判据：60 秒内的偏差最多让一个新会话落错号，
	// 而下一轮就会纠正回来。用这点精度换掉每请求一次聚合查询是划算的。
	schedulingBalanceCacheTTL = 60 * time.Second
)

type schedulingBalancePrefetchContextKeyType struct{}

// 独立的 context key。复用供给者每日上限那个 key 会读到**只包含设了上限的号**的
// 那份 map——均衡需要的是全部候选，两者的集合不同，混用的表现是大多数号被当成
// 「今天零产出」而一直排在最前面。
var schedulingBalancePrefetchContextKey = schedulingBalancePrefetchContextKeyType{}

// schedulingBalanceCacheEntry 一个账号今日产出的缓存项。
type schedulingBalanceCacheEntry struct {
	cost      float64
	expiresAt time.Time
}

var (
	schedulingBalanceCacheMu sync.RWMutex
	schedulingBalanceCache   = map[int64]schedulingBalanceCacheEntry{}
)

// resetSchedulingBalanceCache 清空缓存。测试用。
func resetSchedulingBalanceCache() {
	schedulingBalanceCacheMu.Lock()
	schedulingBalanceCache = map[int64]schedulingBalanceCacheEntry{}
	schedulingBalanceCacheMu.Unlock()
}

func readSchedulingBalanceCache(accountIDs []int64, now time.Time) (map[int64]float64, []int64) {
	hits := make(map[int64]float64, len(accountIDs))
	var misses []int64

	schedulingBalanceCacheMu.RLock()
	for _, id := range accountIDs {
		entry, ok := schedulingBalanceCache[id]
		if ok && now.Before(entry.expiresAt) {
			hits[id] = entry.cost
			continue
		}
		misses = append(misses, id)
	}
	schedulingBalanceCacheMu.RUnlock()
	return hits, misses
}

func storeSchedulingBalanceCache(costs map[int64]float64, now time.Time) {
	if len(costs) == 0 {
		return
	}
	expiresAt := now.Add(schedulingBalanceCacheTTL)
	schedulingBalanceCacheMu.Lock()
	for id, cost := range costs {
		schedulingBalanceCache[id] = schedulingBalanceCacheEntry{cost: cost, expiresAt: expiresAt}
	}
	schedulingBalanceCacheMu.Unlock()
}

// schedulingBalanceEnabled 均衡这一层开没开。
func (s *GatewayService) schedulingBalanceEnabled() bool {
	return s.schedulingConfig().BalanceNewSessions
}

// schedulingBalanceBandUSD 分档带宽，非正数取默认值。
//
// 读路径夹回而不是信任配置：一个被填成 0 的带宽会让分档退化成「严格选最小」，
// 也就是这个文件开头刻意避开的那个涌向同一个号的形态。
func (s *GatewayService) schedulingBalanceBandUSD() float64 {
	band := s.schedulingConfig().BalanceBandUSD
	if band <= 0 || math.IsNaN(band) || math.IsInf(band, 0) {
		return schedulingBalanceDefaultBandUSD
	}
	return band
}

// withSchedulingBalancePrefetch 批量预取本轮候选的今日产出。
//
// 关掉时一次查询也不发（连 context 都不动），与供给者每日上限那道闸同样的
// 「没开启就是零成本」约定。
func (s *GatewayService) withSchedulingBalancePrefetch(ctx context.Context, accounts []Account) context.Context {
	if ctx == nil || len(accounts) == 0 || s.usageLogRepo == nil || !s.schedulingBalanceEnabled() {
		return ctx
	}

	accountIDs := make([]int64, 0, len(accounts))
	for i := range accounts {
		accountIDs = append(accountIDs, accounts[i].ID)
	}

	now := time.Now()
	costs, misses := readSchedulingBalanceCache(accountIDs, now)
	if len(misses) > 0 {
		// 批量接口走可选能力断言，与 withSupplyDailyCapPrefetch 同一个套路：
		// 拿不到批量能力就不预取，均衡这一层自然退化成「全部视为未知」→ 不参与排序。
		if batch, ok := s.usageLogRepo.(accountWindowStatsBatchReader); ok {
			// 窗口起点复用供给者每日上限那一个：同一个「今天」的定义只能有一处。
			statsByID, err := batch.GetAccountWindowStatsBatch(ctx, misses, supplyDailyWindowStart())
			if err != nil {
				logger.LegacyPrintf("service.gateway", "scheduling balance batch usage read failed: %v", err)
			} else {
				fetched := make(map[int64]float64, len(misses))
				for _, id := range misses {
					// 批量接口对未命中的账号返回零值统计——今天还没开张，正是该优先给的那些。
					if stats := statsByID[id]; stats != nil {
						fetched[id] = stats.StandardCost
					} else {
						fetched[id] = 0
					}
				}
				storeSchedulingBalanceCache(fetched, now)
				for id, cost := range fetched {
					costs[id] = cost
				}
			}
		}
	}

	if len(costs) == 0 {
		return ctx
	}
	return context.WithValue(ctx, schedulingBalancePrefetchContextKey, costs)
}

// schedulingBalanceCosts 取本轮预取到的今日产出表。没有预取就返回 nil。
func schedulingBalanceCosts(ctx context.Context) map[int64]float64 {
	if ctx == nil {
		return nil
	}
	costs, _ := ctx.Value(schedulingBalancePrefetchContextKey).(map[int64]float64)
	return costs
}

// filterByBalanceBand 只留下今日产出落在最低一档里的候选。
//
// 三种情况原样返回全部候选（**不做任何筛选**），都是刻意的：
//   - 均衡没开；
//   - 没有预取到产出（查询失败/缓存全空）——查不出用量时不该改变调度行为，
//     与三道用量闸「失败开放」同向；
//   - 候选里有账号查不到产出。这一条最容易被写反：把查不到的当成 0 会让它
//     永远排在最前面、吃下全部新会话，而那恰恰是一个**数据缺失**的号。
func (s *GatewayService) filterByBalanceBand(ctx context.Context, accounts []accountWithLoad) []accountWithLoad {
	if len(accounts) <= 1 || !s.schedulingBalanceEnabled() {
		return accounts
	}
	costs := schedulingBalanceCosts(ctx)
	if len(costs) == 0 {
		return accounts
	}

	band := s.schedulingBalanceBandUSD()
	minBand := math.MaxInt64
	bands := make([]int, len(accounts))
	for i, acc := range accounts {
		cost, ok := costs[acc.account.ID]
		if !ok {
			return accounts
		}
		if cost < 0 {
			cost = 0
		}
		b := int(math.Floor(cost / band))
		bands[i] = b
		if b < minBand {
			minBand = b
		}
	}

	filtered := make([]accountWithLoad, 0, len(accounts))
	for i, acc := range accounts {
		if bands[i] == minBand {
			filtered = append(filtered, acc)
		}
	}
	if len(filtered) == 0 {
		return accounts
	}
	return filtered
}
