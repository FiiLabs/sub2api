// APEXONE-EXT: 双边市场——把结算参数挂到计费命令上。
//
// 这是「配置」与「计费」之间唯一的接缝。放在这里而不是 buildUsageBillingCommand 里，
// 有两个原因：
//
//  1. buildUsageBillingCommand 是个纯函数（无 ctx、无依赖），保持它纯的，
//     单测就不必为了构造一条命令而准备一个 SettingService；
//  2. 结算参数不参与请求指纹、也不参与金额量化，晚于 Normalize() 赋值完全安全，
//     因此不需要挤进那个函数。
//
// 参数在这里取一次快照、随命令走完全程。计费事务内部不再读配置——那里再读一次
// 既慢，又可能读到与本次请求开头不同的值，导致「基数 × 比例 = 金额」对不上。
package service

import "context"

// applySupplierSettlementParams 给命令填上本次请求的供给结算参数。
//
// 读不到配置、配置关闭、依赖缺失，统统留零值 —— 也就是
// applyUsageBillingEffects 里「什么都不做」的那一支。这条路径上不存在错误返回：
// 供给结算是增量能力，它的任何故障都不该让一笔正常的计费失败。
func applySupplierSettlementParams(ctx context.Context, cmd *UsageBillingCommand, deps *billingDeps) {
	if cmd == nil || deps == nil || deps.settingService == nil {
		return
	}
	cmd.Supplier = deps.settingService.GetSupplierSettlementSettings(ctx).ToBillingParams()
}
