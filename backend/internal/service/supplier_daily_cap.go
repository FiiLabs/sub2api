// APEXONE-EXT: 双边市场——供给者设置每日共享上限的写路径。
//
// 判定逻辑在 gateway_supply_daily_cap.go，这里只管「谁能写、能写什么值」。
//
// 单起一个文件而不是塞进 supplier_onboarding_service.go：那个文件是接入流程
// （OAuth 换码、观察期、下线解绑）的主场，而这是一个与接入无关的设置项。
package service

import (
	"context"
	"math"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// 上限的上限。纯防手滑，不是业务策略——它回答的是「你是不是多打了个零」，
// 不是「我们允许你分享多少」。
const (
	// SupplyDailyCostLimitMaxUSD 每日金额上限最多填到 10000 美元。
	// 按官方牌价算，这个数远超任何一份订阅一天可能产出的用量。
	SupplyDailyCostLimitMaxUSD = 10000.0
	// SupplyDailyTokenLimitMax 每日 token 上限最多填到 1 万亿。
	SupplyDailyTokenLimitMax = int64(1_000_000_000_000)
)

// ErrSupplyDailyCapInvalid 上限值不合法。
//
// 刻意不把具体越界的那个字段拼进消息里：两个字段的合法区间都写在界面上，
// 而这条错误只会在界面被绕过或有人手工调接口时出现。
var ErrSupplyDailyCapInvalid = infraerrors.BadRequest(
	"SUPPLY_DAILY_CAP_INVALID", "daily cap must be a non-negative number within the allowed range")

// SetDailyCap 设置某个供给账号的每日共享上限。
//
// 两个参数都是指针，语义是**三态**：
//   - nil  = 这一项不改
//   - 0    = 取消这一项的上限
//   - 正数 = 设成这个值
//
// 用值类型的话，「把金额上限清成 0」和「我只想改 token 上限」会发出同一个请求体，
// 服务端无从分辨——而这两件事的结果完全相反。
//
// 归属校验走 getOwnedAccount：别人的号和平台自营的号一律返回「找不到」，
// 不区分，因为「这个号存在但不归你」本身就是不该泄漏的信息。
func (s *SupplierOnboardingService) SetDailyCap(ctx context.Context, userID, accountID int64, costLimitUSD *float64, tokenLimit *int64) (*SupplierAccountView, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrSupplierOnboardingDisabled
	}

	account, err := s.getOwnedAccount(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}

	updates := make(map[string]any, 2)
	if costLimitUSD != nil {
		v := *costLimitUSD
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > SupplyDailyCostLimitMaxUSD {
			return nil, ErrSupplyDailyCapInvalid
		}
		// 截到分。存一个 0.006 美元的上限没有意义，而它会让界面显示的数字
		// 和实际生效的数字对不上。
		updates[SupplyDailyCostLimitExtraKey] = math.Round(v*100) / 100
	}
	if tokenLimit != nil {
		v := *tokenLimit
		if v < 0 || v > SupplyDailyTokenLimitMax {
			return nil, ErrSupplyDailyCapInvalid
		}
		updates[SupplyDailyTokenLimitExtraKey] = v
	}
	if len(updates) == 0 {
		// 两个都没传：不是错误，就是一次空操作。回读当前状态即可。
		return newSupplierAccountView(account, s.probationSettings(ctx)), nil
	}

	// UpdateExtra 是 JSONB 的 `extra || $1` 合并，不是整体替换——所以这次写入
	// 不会碰到另一个上限键，也不会碰到 apexone_supply_* 那一族生命周期状态
	// （它们由观察期状态机在并发地改）。任何时候都不要把这条路径换成整体替换。
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		return nil, err
	}

	// 回读而不是把 updates 拍回内存里的 account：调度快照的同步发生在
	// UpdateExtra 内部，回读能顺带确认写确实落库了。
	updated, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	return newSupplierAccountView(updated, s.probationSettings(ctx)), nil
}

// supplierDailyUsageReader 是取「今日已用」需要的那点能力。
//
// 单独一个窄接口而不是让 SupplierOnboardingService 依赖整个 UsageLogRepository：
// 这里只读一个批量统计，声明成能装配的最小面。
type supplierDailyUsageReader interface {
	GetAccountWindowStatsBatch(ctx context.Context, accountIDs []int64, startTime time.Time) (map[int64]*usagestats.AccountStats, error)
}

// SetDailyUsageReader 装配用量读取器。不装 = 上限照常显示，只是「今日已用」恒为 0。
//
// 做成可选（而不是构造函数的必填参数）与 SetIncidentGuard 同一个理由：
// 用量读不到时这一页的其余部分仍然完全可用，不该因此让整个服务装配失败。
func (s *SupplierOnboardingService) SetDailyUsageReader(r supplierDailyUsageReader) {
	if s == nil {
		return
	}
	s.dailyUsageReader = r
}

// applyDailyCapUsage 给一批视图补上「今日已用」和「是否触顶」。
//
// best-effort：查不到就保持零值，上限本身照常显示。理由与调度闸的失败开放一致——
// 一次数据库抖动不该让供给者以为自己的号被限住了。
//
// 触顶判定复用 Account.CheckSupplyDailyCapSchedulability，与调度闸走**同一个函数**：
// 若这里自己写一遍 used >= limit，两处的边界语义迟早会漂移，而现象是界面说
// 「还能接单」但实际已经不接了。
func (s *SupplierOnboardingService) applyDailyCapUsage(ctx context.Context, views []SupplierAccountView, accounts map[int64]*Account) {
	if s == nil || s.dailyUsageReader == nil || len(views) == 0 {
		return
	}
	ids := make([]int64, 0, len(views))
	for i := range views {
		if views[i].DailyCostLimitUSD > 0 || views[i].DailyTokenLimit > 0 {
			ids = append(ids, views[i].ID)
		}
	}
	// 没有任何号设过上限——今天绝大多数供给者的情况——一次查询也不发。
	if len(ids) == 0 {
		return
	}
	statsByID, err := s.dailyUsageReader.GetAccountWindowStatsBatch(ctx, ids, supplyDailyWindowStart())
	if err != nil {
		return
	}
	for i := range views {
		stats := statsByID[views[i].ID]
		if stats == nil {
			continue
		}
		views[i].DailyCostUsedUSD = stats.StandardCost
		views[i].DailyTokensUsed = stats.Tokens
		if account := accounts[views[i].ID]; account != nil {
			views[i].DailyCapReached =
				account.CheckSupplyDailyCapSchedulability(stats.StandardCost, stats.Tokens) == WindowCostNotSchedulable
		}
	}
}
