// APEXONE-EXT: 双边市场 OpenAI 侧——供给池干涸时溢出到自营池。
//
// Claude 网关的溢出层在 gateway_supply_overflow.go；OpenAI 网关此前完全没有这一层
// （只会在供给池打空时直接把 ErrNoAvailableAccounts 抛给消费者）。本文件把同一套
// 「先在供给池选，空了再转自营池，并计入每日预算 / 耗尽信号」逻辑补给 OpenAI，
// 与 Claude 侧一比一对应。
//
// 分组解析用 *groupID 直取（不走 Claude 的 resolveGatewayGroup 默认组回落）：供给组是
// 具体分组 id，路由到它的请求都带着这个 id；nil（默认组）几乎不可能正好是供给组，
// 直取足够且省一次分组查询。溢出目标与每日上限都按平台多对配置解析（见 setting_supply_pool.go）。
package service

import (
	"context"
	"errors"
	"log/slog"
)

// resolveSupplyOverflowGroupID 解析某分组应溢出到的自营分组及其当日上限。
// 返回 (overflowGroupID, dailyLimit, ok)。ok=false 表示不该溢出。
func (s *OpenAIGatewayService) resolveSupplyOverflowGroupID(ctx context.Context, groupID *int64) (int64, int, bool) {
	if s == nil || groupID == nil || s.settingService == nil {
		return 0, 0, false
	}
	settings := s.settingService.GetSupplyPoolSettings(ctx)
	if settings == nil || !settings.Enabled {
		return 0, 0, false
	}
	target, ok := settings.overflowTargetFor(*groupID)
	if !ok {
		return 0, 0, false
	}
	return target, settings.overflowLimitFor(*groupID), true
}

// overflowToFirstParty 是选号的溢出后处理：供给池给出 ErrNoAvailableAccounts 时，
// 在预算内转到自营池再选一次。非该错误 / 未配置溢出 / 预算耗尽 → 原样返回原结果。
//
// 复用 gateway 层已有的包级预算函数 allowSupplyOverflow / recordSupplyOverflowExhausted。
// 溢出选号复用同一个 ctx（隐私/利润门已按原组装好）——与 Claude 侧一致。
func (s *OpenAIGatewayService) overflowToFirstParty(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	result *AccountSelectionResult,
	err error,
) (*AccountSelectionResult, error) {
	if err == nil || !errors.Is(err, ErrNoAvailableAccounts) {
		return result, err
	}

	overflowGroupID, dailyLimit, ok := s.resolveSupplyOverflowGroupID(ctx, groupID)
	if !ok {
		return result, err
	}

	if !allowSupplyOverflow(ctx, dailyLimit) {
		slog.Warn("[SupplyPool][openai] daily overflow budget exhausted, not overflowing",
			"supply_group_id", derefGroupID(groupID),
			"overflow_group_id", overflowGroupID,
			"daily_limit", dailyLimit,
			"model", requestedModel)
		return result, err
	}

	slog.Warn("[SupplyPool][openai] supply pool exhausted, overflowing to first-party pool",
		"supply_group_id", derefGroupID(groupID),
		"overflow_group_id", overflowGroupID,
		"model", requestedModel,
		"reason", err)

	overflowResult, overflowErr := s.selectAccountWithLoadAwareness(
		ctx, &overflowGroupID, PlatformOpenAI, sessionHash, requestedModel, excludedIDs, false, "", true)
	if overflowErr != nil {
		slog.Error("[SupplyPool][openai] first-party overflow pool is exhausted too",
			"supply_group_id", derefGroupID(groupID),
			"overflow_group_id", overflowGroupID,
			"model", requestedModel,
			"error", overflowErr)
		recordSupplyOverflowExhausted(ctx)
		// 还回原始错误：请求打的是消费者自己的分组，报一个指向自营池的错误会误导排查。
		return result, err
	}
	return overflowResult, nil
}
