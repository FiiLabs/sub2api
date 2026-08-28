//go:build unit

// APEXONE-EXT: 调度快照 extra 白名单的守卫测试。
//
// 这个文件存在的唯一理由，是这里的遗漏**不会以任何方式表现出来**。
//
// filterSchedulerExtra 是一份白名单：不在名单里的 extra 键会被快照静默丢弃。
// 而 listSchedulableAccounts 在快照可用时直接返回、不回落数据库，所以任何一道
// 「读 extra 阈值」的调度闸，只要它的键不在名单里，就会永远读到零值——
// 对上限类的判据而言，零值恰好是「不限」。功能整个失效，没有报错、没有日志、
// 没有测试变红，只有一个「我明明设了上限，怎么没生效」的工单。
//
// 这不是假想：base_rpm / rpm_strategy 就曾经漏在名单外（补进来的那次提交也是
// 加供给者每日上限的那次），RPM 那道闸因此对走快照的账号长期是个空操作。
package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 每一个键都必须能举出「谁在读它」。加新键时把读者一并写进这张表，
// 让下一个人不必猜这里为什么要有这一行。
var schedulerExtraKeysUnderGuard = map[string]string{
	"window_cost_limit":                "checkWindowCostGate → Account.GetWindowCostLimit",
	"window_cost_sticky_reserve":       "Account.GetWindowCostStickyReserve",
	"base_rpm":                         "checkRPMGate → Account.GetBaseRPM",
	"rpm_strategy":                     "Account.GetRPMStrategy",
	"apexone_supply_daily_cost_limit":  "isAccountSchedulableForSupplyDailyCap → Account.GetSupplyDailyCostLimit",
	"apexone_supply_daily_token_limit": "isAccountSchedulableForSupplyDailyCap → Account.GetSupplyDailyTokenLimit",
	"quota_daily_limit":                "Account.IsQuotaExceeded",
	"quota_daily_used":                 "Account.IsQuotaExceeded",
}

func TestFilterSchedulerExtraKeepsGateKeys(t *testing.T) {
	extra := make(map[string]any, len(schedulerExtraKeysUnderGuard))
	for key := range schedulerExtraKeysUnderGuard {
		// 用非零值：如果哪天过滤逻辑改成「丢掉零值」，零值填充会让这条测试
		// 假装通过。
		extra[key] = 1
	}

	filtered := filterSchedulerExtra(extra)

	for key, reader := range schedulerExtraKeysUnderGuard {
		assert.Containsf(t, filtered, key,
			"extra 键 %q 被调度快照丢掉了，而 %s 会读它。"+
				"后果是那道闸永远读到零值（对上限类判据 = 不限），静默失效。"+
				"修法：把这个键加进 scheduler_cache.go 的 filterSchedulerExtra。", key, reader)
	}
}

// 白名单必须还是白名单：任意键不能穿透。
// 这条与上一条成对——只测「该留的留下了」，一个 `return extra` 也能通过。
func TestFilterSchedulerExtraDropsUnknownKeys(t *testing.T) {
	filtered := filterSchedulerExtra(map[string]any{
		"window_cost_limit": 1,
		"credentials":       "SECRET",
		"some_future_key":   "x",
	})

	assert.Contains(t, filtered, "window_cost_limit")
	assert.NotContains(t, filtered, "credentials")
	assert.NotContains(t, filtered, "some_future_key")
}
