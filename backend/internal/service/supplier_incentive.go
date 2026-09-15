// APEXONE-EXT: 双边市场——挂号奖励的领域类型与仓储接口。
//
// 规则在 setting_supply_incentive.go，发放在 supplier_incentive_worker.go，
// SQL 在 internal/repository/supplier_incentive_repo.go。本文件只放三者之间传的东西。
package service

import (
	"context"
	"fmt"
	"strings"
)

// supplyIncentiveKeyPrefix 幂等键的固定前缀。
//
// 全键形如 `cmp:bind26q4:t2:a1234`，落在 supplier_credit_ledger 的
// `(action, request_id)` 部分唯一索引上——那个索引才是「同一档不会发两次」的
// 真正保证，worker 里的名额检查只是省一次无谓的写。
//
// 前缀刻意与 usage_log.request_id（uuid 形状）不同族：两者共用同一个唯一索引，
// 撞上一个真实请求 id 的后果是那笔用量的分成入不了账。
const supplyIncentiveKeyPrefix = "cmp"

// SupplyIncentiveRequestPrefix 拼出某一档的幂等键前缀（不含账号 id）。
//
// SQL 侧用 `request_id = prefix || a.id::text` 做等值命中，所以这里**不能**
// 带上账号 id，也不能有尾随分隔符之外的东西。
func SupplyIncentiveRequestPrefix(slug string, tierIndex int) string {
	return fmt.Sprintf("%s:%s:t%d:a", supplyIncentiveKeyPrefix, strings.ToLower(strings.TrimSpace(slug)), tierIndex)
}

// SupplyIncentiveRequestID 某个账号在某一档的完整幂等键。
func SupplyIncentiveRequestID(slug string, tierIndex int, accountID int64) string {
	return fmt.Sprintf("%s%d", SupplyIncentiveRequestPrefix(slug, tierIndex), accountID)
}

// SupplyIncentiveCandidate 是一个「够格拿这一档奖励、且还没拿过」的账号。
type SupplyIncentiveCandidate struct {
	AccountID int64
	// OwnerUserID 收款人。查询里已经排除了 NULL——无主的号不该有人收钱。
	OwnerUserID int64
	// ActiveDays 累计在线天数，写进流水备注供事后对账。
	ActiveDays int
}

// SupplyIncentiveCandidateQuery 候选账号的筛选条件。
type SupplyIncentiveCandidateQuery struct {
	// MinActiveDays 该档的天数门槛。
	MinActiveDays int
	// Platform 限定平台，空 = 不限。
	Platform string
	// RequestPrefix 该档的幂等键前缀，用于排除已发过的账号。
	RequestPrefix string
	// Limit 最多取多少个（= 该档剩余名额）。
	Limit int
}

// SupplierIncentiveRepository 是挂号奖励要用到的三个查询。
//
// 刻意不复用 SupplierOnboardingRepository：那个接口已经有十几个方法，而这三个
// 属于一个可以整体开关的功能。分开之后，「关掉活动」在依赖图上是可见的一整块。
type SupplierIncentiveRepository interface {
	// TickActiveDays 给所有在役供给号的在线天数 +1，同一天重复调用不重复累加。
	// day 是 UTC 日期串（YYYY-MM-DD）。返回本次真正累加的账号数。
	TickActiveDays(ctx context.Context, day string) (int64, error)

	// CountGranted 数某一档已经发出去多少份（= 已用名额）。
	CountGranted(ctx context.Context, requestPrefix string) (int, error)

	// ListCandidates 列出够格且未发过的账号，按在线天数降序（等价于「接入早的先拿」）。
	ListCandidates(ctx context.Context, query SupplyIncentiveCandidateQuery) ([]SupplyIncentiveCandidate, error)
}
