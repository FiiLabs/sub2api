// APEXONE-EXT: 双边市场——供给者自设的每日共享上限。
//
// 一位真实供给者问的就是这件事："can I set a token limit that I want to share?
// i want to keep some and not have all of it consumed by users."
//
// 在此之前，挂号是全有或全无：接入之后唯一的控制手段是暂停和解绑。一个愿意分享
// 闲置额度、但想给自己留一部分的人，中间没有任何档位——这会劝退一批本来愿意来的人。
//
// # 这道闸为什么与 checkWindowCostGate 长得像但不一样
//
// 它是那道闸的近亲（同样读用量、同样预取优先、同样失败开放），但有三处**刻意**的
// 不同，每一处都对应一个会静默出错的坑：
//
//  1. **不做账号类型判断。** window_cost 那道闸开头是 IsAnthropicOAuthOrSetupToken()。
//     照抄会静默跳过每一个中转接入的号——OAuth 接入建的是 AccountTypeSetupToken
//     （supplier_onboarding_service.go），中转接入建的是 AccountTypeAPIKey
//     （supplier_relay.go）。判据只有一条：这个号上有没有设过上限。
//
//  2. **窗口起点是 UTC 零点，不是滚动会话窗。** 也刻意不用 timezone.Today()——那是
//     **配置的**平台时区，运营改一次设置就会把所有供给者的重置点悄悄挪走。
//     供给者被告知的是「UTC 零点重置」，那就必须真的是 UTC 零点。
//
//  3. **不接 Redis 缓存。** window_cost 那道闸缓存 30 秒。这里不缓存，因为跨零点的
//     那 30 秒会读到昨天的总量，把一个本该已经恢复接单的号继续挡在外面——而供给者
//     看到的现象是「说好零点恢复，但没有」。查询本身走
//     idx_usage_logs_account_created_at (account_id, created_at)，且只对**设了上限的
//     号**发起，批量预取后每轮调度一次。用一点点延迟换掉一个会被投诉的正确性问题。
//
// # 归属校验在哪
//
// 不在这里。owner_user_id 不在内存里的 Account 上、也不在调度快照里，为了在热路径
// 上查一次归属而把它一路穿下来不值得。而且没必要：这两个 extra 键只有供给者本人能写
// （写路径上 getOwnedAccount 强制），所以**上限存在本身就是归属信号**。
//
// # 触顶之后
//
// 什么都不写。这道闸只在选号时刻做判断，不去改 accounts.schedulable——那会在调度
// 热路径上写库、与供给者自己的暂停/恢复打架，而且没有任何人负责在零点把它改回来。
// 「次日自动恢复」是靠窗口起点自然前移实现的，不需要定时任务。
package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// supplyDailyWindowStart 返回今天的 UTC 零点。
//
// 做成变量是为了单测能改，不是为了运行时可配。它必须是 UTC：见文件头第 2 条。
var supplyDailyWindowStart = func() time.Time {
	return time.Now().UTC().Truncate(24 * time.Hour)
}

// supplyDailyUsage 是一个供给账号今天已经被用掉的量。
type supplyDailyUsage struct {
	// Cost 官方牌价口径（usage_logs.total_cost 之和），不是消费者实付。
	// 供给者要保护的是自己订阅额度的消耗。
	Cost float64
	// Tokens 输入+输出+缓存创建+缓存读取之和。
	Tokens int64
}

type supplyDailyCapPrefetchContextKeyType struct{}

// 独立的 context key 类型。复用 windowCostPrefetchContextKey 会让这道闸读到
// 5 小时窗口的费用，从而按一个完全不同的数字去卡上限，且不会有任何报错。
var supplyDailyCapPrefetchContextKey = supplyDailyCapPrefetchContextKeyType{}

func supplyDailyUsageFromPrefetchContext(ctx context.Context, accountID int64) (supplyDailyUsage, bool) {
	if ctx == nil {
		return supplyDailyUsage{}, false
	}
	m, ok := ctx.Value(supplyDailyCapPrefetchContextKey).(map[int64]supplyDailyUsage)
	if !ok {
		return supplyDailyUsage{}, false
	}
	usage, exists := m[accountID]
	return usage, exists
}

// isAccountSchedulableForSupplyDailyCap 检查账号今日是否已用满供给者自设的上限。
//
// isSticky 目前不参与判断（硬上限，不留 sticky 余量，理由见
// CheckSupplyDailyCapSchedulability），但保留在签名里：一是与另外两道闸签名一致、
// dynamicLimitGate 不必为它开特例，二是日后若真要加按比例的宽限，只改一个函数。
func (s *GatewayService) isAccountSchedulableForSupplyDailyCap(ctx context.Context, account *Account, isSticky bool) bool {
	// 绝大多数账号在这里返回：不碰 ctx、不碰数据库。
	// 没有任何供给者设过上限的部署里，这道闸的净成本是两次 map 查找。
	if account == nil || !account.HasSupplyDailyCap() {
		return true
	}

	usage, ok := s.resolveSupplyDailyUsage(ctx, account.ID)
	if !ok {
		// 失败开放，与另外两道闸同向：查不出用量时，宁可让这个号继续接单，
		// 也不要因为一次数据库抖动就把所有设了上限的号一起踢出池子。
		return true
	}

	return account.CheckSupplyDailyCapSchedulability(usage.Cost, usage.Tokens) != WindowCostNotSchedulable
}

// resolveSupplyDailyUsage 取某个号今天的用量。预取命中则零成本，否则单点查询。
func (s *GatewayService) resolveSupplyDailyUsage(ctx context.Context, accountID int64) (supplyDailyUsage, bool) {
	if usage, ok := supplyDailyUsageFromPrefetchContext(ctx, accountID); ok {
		return usage, true
	}
	if s.usageLogRepo == nil {
		return supplyDailyUsage{}, false
	}
	stats, err := s.usageLogRepo.GetAccountWindowStats(ctx, accountID, supplyDailyWindowStart())
	if err != nil || stats == nil {
		return supplyDailyUsage{}, false
	}
	return supplyDailyUsage{Cost: stats.StandardCost, Tokens: stats.Tokens}, true
}

// withSupplyDailyCapPrefetch 批量预取本轮候选里「设了上限的那些号」今日用量。
//
// 过滤条件必须与 isAccountSchedulableForSupplyDailyCap 的早返回**完全一致**
// （都是 HasSupplyDailyCap）。两边一旦不同步，被漏掉的号会退化成每请求一次单点
// 查询——不给错答案，只是慢，且没人会归因到这里。
func (s *GatewayService) withSupplyDailyCapPrefetch(ctx context.Context, accounts []Account) context.Context {
	if ctx == nil || len(accounts) == 0 || s.usageLogRepo == nil {
		return ctx
	}

	accountIDs := make([]int64, 0, len(accounts))
	for i := range accounts {
		if accounts[i].HasSupplyDailyCap() {
			accountIDs = append(accountIDs, accounts[i].ID)
		}
	}
	// 没有任何号设过上限——也就是今天绝大多数部署的情况——一次查询也不发。
	if len(accountIDs) == 0 {
		return ctx
	}

	// 批量接口不在 UsageLogRepository 主接口上，走可选能力断言——与
	// queryGrokFreeQuotaWindowStats 同一个套路。拿不到批量能力就不预取，
	// 闸门那边会退回单点查询，正确性不受影响。
	batch, ok := s.usageLogRepo.(accountWindowStatsBatchReader)
	if !ok {
		return ctx
	}

	statsByID, err := batch.GetAccountWindowStatsBatch(ctx, accountIDs, supplyDailyWindowStart())
	if err != nil {
		// 不塞 context：闸门那边会退回单点查询，再失败就失败开放。
		logger.LegacyPrintf("service.gateway", "supply daily cap batch usage read failed: %v", err)
		return ctx
	}

	usages := make(map[int64]supplyDailyUsage, len(accountIDs))
	for _, id := range accountIDs {
		// 批量接口对未命中的账号返回零值统计，这里也就自然是「今天还没用过」。
		if stats := statsByID[id]; stats != nil {
			usages[id] = supplyDailyUsage{Cost: stats.StandardCost, Tokens: stats.Tokens}
		} else {
			usages[id] = supplyDailyUsage{}
		}
	}
	return context.WithValue(ctx, supplyDailyCapPrefetchContextKey, usages)
}
