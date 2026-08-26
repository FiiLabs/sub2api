// APEXONE-EXT: 双边市场——供给池干涸时溢出到自营池。
//
// # 交接件在这一点上写错了，以本文件为准
//
// ticket 07 判定「溢出用现成 `fallback_group_id`，零 core 改动」。在 0.1.177 上不成立：
// `fallback_group_id` 只被 `resolveGatewayGroup` 读，触发条件是**分组开了
// `claude_code_only` 而请求不是 Claude Code 客户端**——那是按客户端类型做的静态降级，
// 发生在选号**之前**，跟有没有号一点关系都没有。供给池号被抽干时，请求照样在供给池里
// 走完整个调度然后拿 `ErrNoAvailableAccounts` 出来，没有任何东西会把它送去自营池。
//
// 借用那个字段也不行：管理员为了「非 CC 客户端降级」配的 fallback，会因此附赠一条
// 「没号就溢出」的语义。一个字段两种触发条件，事后没人说得清某次溢出是哪条规则干的。
// 所以另起一条独立规则（配置见 setting_supply_pool.go），并接受一处 core 侵入。
//
// # 为什么计费不用跟着换池
//
// 白捡的一条性质：消费者价来自 `apiKey.Group.RateMultiplier`（见
// gateway_usage_billing.go:806-813），而调度用的 group 是函数内部的局部变量，
// 从不回传给计费。所以溢出**只换供货来源，不换价签**——消费者按自己买的档位付钱，
// 平台自吃「按自营成本供货却按供给池价收费」的差额。
//
// 这既是对的（消费者买的是 0.5× 档，供给干涸是平台的问题，不该让他按 2 倍结账），
// 也不是我新造的语义：已有的 claude-code 降级早就在跑同一条路——账号来自 fallback 分组、
// 计价来自 apiKey 分组。溢出只是复用了一条已经被生产验证过的状态。
//
// **代价要写在明面上**：每一次溢出，平台都在亏钱供货。所以溢出率必须是被盯着的指标
// （下面 Warn 级日志），且溢出门开得很窄——只有被显式指定为供给池的那一个分组会溢出。
// 否则消费者只要有办法把供给池打空，就能用 0.5× 的价长期买到 1.0× 成本的服务。
package service

import (
	"context"
	"errors"
	"log/slog"
)

// SelectAccountWithLoadAwareness 在原调度之外加一层供给池溢出。
//
// 溢出只在**硬耗尽**（`ErrNoAvailableAccounts`）时发生，不在「有号但都忙」时发生：
// 后者原逻辑会返回一个等待计划，那说明供给还在、只是拥挤，把它送去自营池等于用平台
// 成本去买一点排队延迟。
//
// 只重试一次，不成链。多级溢出的收益在首版切片里不存在（只有两个池），而一条会
// 越走越贵的重试链在供给全面干涸时会把每个失败请求的调度开销乘上链长。
func (s *GatewayService) SelectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, metadataUserID string, sub2apiUserID int64) (*AccountSelectionResult, error) {
	result, err := s.selectAccountInPoolWithLoadAwareness(ctx, groupID, sessionHash, requestedModel, excludedIDs, metadataUserID, sub2apiUserID)
	if err == nil || !errors.Is(err, ErrNoAvailableAccounts) {
		return result, err
	}

	overflowGroupID, dailyLimit, ok := s.resolveSupplyOverflowGroupID(ctx, groupID)
	if !ok {
		return result, err
	}

	// 日配额闸门。放在解析之后是有意的：只有确实该溢出的请求才去消耗配额，
	// 否则任何一个空分组的耗尽都会把供给池的预算吃掉。
	if !allowSupplyOverflow(ctx, dailyLimit) {
		// Warn 而非 Error：配额生效时平台在**省钱**，这不是故障。但它同时说明
		// 供给侧规模已经明显跟不上需求，是要人看的经营信号。
		slog.Warn("[SupplyPool] daily overflow budget exhausted, not overflowing",
			"supply_group_id", derefGroupID(groupID),
			"overflow_group_id", overflowGroupID,
			"daily_limit", dailyLimit,
			"model", requestedModel)
		return result, err
	}

	// Warn 而非 Info：每条都代表平台正在亏钱供货。这条日志的频率就是溢出率，
	// 它涨起来意味着供给侧规模跟不上需求，是个要人介入的经营信号，不是背景噪音。
	slog.Warn("[SupplyPool] supply pool exhausted, overflowing to first-party pool",
		"supply_group_id", derefGroupID(groupID),
		"overflow_group_id", overflowGroupID,
		"model", requestedModel,
		"reason", err)

	overflowResult, overflowErr := s.selectAccountInPoolWithLoadAwareness(ctx, &overflowGroupID, sessionHash, requestedModel, excludedIDs, metadataUserID, sub2apiUserID)
	if overflowErr != nil {
		slog.Error("[SupplyPool] first-party overflow pool is exhausted too",
			"supply_group_id", derefGroupID(groupID),
			"overflow_group_id", overflowGroupID,
			"model", requestedModel,
			"error", overflowErr)
		// 这一刻消费者会拿到 "No available accounts"。数下来（迁移 236）——
		// 它是「兜底账号够不够」的唯一信号，只留一条日志等于没人会回答这个问题。
		recordSupplyOverflowExhausted(ctx)
		// 还回**原始**错误：请求打的是消费者自己的分组，报一个指向自营池的错误
		// 会把排查的人引到错误的池子上去。溢出池的失败已经单独记在上面那条日志里。
		return result, err
	}
	return overflowResult, nil
}

// resolveSupplyOverflowGroupID 判定本次失败是否该溢出、溢出到哪里、以及当日配额上限。
//
// 只在失败路径上调用，所以这里多做一次分组解析对热路径零成本；换来的是精确：
// 拿**解析后**的分组去比对供给池 id，而不是拿 API key 上那个原始 id。两者可能不同——
// claude-code 降级会在选号前就把分组换掉，那种情况下耗尽的是降级分组，不是供给池，
// 不该按供给干涸处理。
//
// 配额上限跟着一起返回，是为了让调用方不必再读一次配置：这两个值必须来自**同一份**
// 快照，否则一次配置变更落在两次读中间时，会拿新的上限去管旧的目标分组。
func (s *GatewayService) resolveSupplyOverflowGroupID(ctx context.Context, groupID *int64) (int64, int, bool) {
	if s == nil || groupID == nil || s.settingService == nil {
		return 0, 0, false
	}
	settings := s.settingService.GetSupplyPoolSettings(ctx)
	if settings == nil || !settings.Enabled {
		return 0, 0, false
	}

	_, resolvedGroupID, err := s.resolveGatewayGroup(ctx, groupID)
	if err != nil || resolvedGroupID == nil {
		return 0, 0, false
	}
	target, ok := settings.overflowTargetFor(*resolvedGroupID)
	if !ok {
		return 0, 0, false
	}
	return target, settings.DailyOverflowLimit, true
}
