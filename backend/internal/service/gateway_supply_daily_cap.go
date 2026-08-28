// APEXONE-EXT: 双边市场——供给者自设的每日共享上限。
//
// 一位真实供给者问的是这件事："can I set a token limit that I want to share?
// i want to keep some and not have all of it consumed by users."
//
// 在此之前，挂号是全有或全无：接入之后唯一的控制手段是暂停和解绑。一个愿意分享
// 闲置额度、但想给自己留一部分的人，中间没有任何档位——这会劝退一批本来愿意来的人。
//
// # 这道闸为什么长这样
//
// 它是 checkWindowCostGate 的近亲（同样读用量、同样三级取数、同样失败开放），
// 但有三处**刻意**的不同，每一处都对应一个会静默出错的坑：
//
//  1. **不做账号类型判断。** window_cost 那道闸开头是 IsAnthropicOAuthOrSetupToken()。
//     照抄会静默跳过每一个中转接入的号——OAuth 接入建的是 AccountTypeSetupToken，
//     中转接入建的是 AccountTypeAPIKey（supplier_relay.go）。判据只有一条：
//     这个号上有没有设过上限。
//
//  2. **窗口起点是 UTC 零点，不是会话窗。** 也刻意不用 timezone.Today()——那是
//     **配置的**平台时区，运营改一次设置就会把所有供给者的重置点挪走。供给者被告知的是
//     「UTC 零点重置」，那就必须真的是 UTC 零点。
//
//  3. **不做 sticky 预留。** 详见 CheckSupplyDailyCapSchedulability 的注释。
//
// # 归属校验在哪
//
// 不在这里。owner_user_id 不在内存里的 Account 上、也不在调度快照里，为了在热路径上
// 查一次归属而把它一路穿下来是不值得的。而且没必要：这两个 extra 键只有供给者本人
// 能写（写路径上 getOwnedAccount 强制），所以**上限存在本身就是归属信号**。
package service

import (
	"context"
	"time"
)

// supplyDailyWindowStart 返回今天的 UTC 零点。
//
// 做成变量是为了单测能改——不是为了运行时可配。它必须是 UTC：见文件头第 2 条。
var supplyDailyWindowStart = func() time.Time {
	return time.Now().UTC().Truncate(24 * time.Hour)
}

// isAccountSchedulableForSupplyDailyCap 检查账号今日是否已用满供给者自设的上限。
//
// isSticky 目前不参与判断（硬上限，不留 sticky 余量），但保留在签名里：一是与
// 另外两道闸签名一致、dynamicLimitGate 不必为它开特例，二是日后若真要加按比例的
// 宽限，只改这一个函数。
func (s *GatewayService) isAccountSchedulableForSupplyDailyCap(ctx context.Context, account *Account, isSticky bool) bool {
	// 绝大多数账号在这里返回：不碰 ctx、不碰缓存、不碰数据库。
	// 没有任何供给者设过上限的部署里，这道闸的净成本是一次 map 查找。
	if account == nil || !account.HasSupplyDailyCap() {
		return true
	}
	// TODO(下一个提交): 取今日用量并判定。本次提交只做重构，行为必须一字不变。
	return true
}

// withSupplyDailyCapPrefetch 批量预取本轮候选里「设了上限的那些号」今日用量。
func (s *GatewayService) withSupplyDailyCapPrefetch(ctx context.Context, accounts []Account) context.Context {
	// TODO(下一个提交): 批量取数并塞进 context。
	return ctx
}
