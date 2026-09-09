// APEXONE-EXT: 双边市场——供需动态平衡门的判定枢纽。
//
// 两侧共用一套读数，避免两边各读一遍、各写一遍失衡逻辑：
//   - 供给远多于需求 → AllowNewSupplier 拒绝新共享者接入（按平台判）。
//   - 需求远多于供给 → AllowNewConsumer 拒绝新用户注册（全局判）。
//
// # 衡量口径：头数比
//
//   - 需求 = 今日活跃消费用户数（DashboardStats.ActiveUsers，全局，已缓存）。
//   - 供给 = 当前可调度的共享账号数（按平台；见 CountActiveSupplyAccounts）。
//
// # 一切读取失败一律 fail-open（放行）
//
// 这道门存在的目的是「在供需明显失衡时踩一脚刹车」，不是「守住某条硬约束」。
// 读不到活跃用户数、读不到供给号数、设置服务不可用——任一情况下正确的默认动作
// 都是放行：一个因为 dashboard 缓存抖动就把所有新用户或新共享者挡在门外的闸，
// 造成的损失远大于它防的那点失衡。所以本文件没有任何一条 `return err` 是因为
// 读取失败——读取失败只会让门「这一次不生效」。
package service

import (
	"context"
	"log/slog"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

var (
	// ErrSupplierRejectedOversupplied 供给过剩时拒绝新共享者接入。
	//
	// 文案刻意说「暂停」而不是「不允许」：这是一个随供需变动的临时状态，
	// 供给者过一阵再来、或换个还缺供给的平台就能接上。
	ErrSupplierRejectedOversupplied = infraerrors.BadRequest(
		"SUPPLIER_REJECTED_OVERSUPPLIED",
		"this platform has enough shared capacity for now; new sharing is paused until demand catches up")
	// ErrRegistrationRejectedUndersupplied 供给不足时拒绝新用户注册。
	ErrRegistrationRejectedUndersupplied = infraerrors.BadRequest(
		"REGISTRATION_REJECTED_UNDERSUPPLIED",
		"we are at capacity right now; new signups are paused until more shared capacity comes online")
)

// supplyDemandGateReader 读平衡门阈值。由 *SettingService 实现。
type supplyDemandGateReader interface {
	GetSupplyDemandGateSettings(ctx context.Context) *SupplyDemandGateSettings
}

// supplyDemandStatsReader 读需求侧读数（活跃用户）。由 *DashboardService 实现。
type supplyDemandStatsReader interface {
	GetDashboardStats(ctx context.Context) (*usagestats.DashboardStats, error)
}

// supplyAccountCounter 读供给侧读数（按平台的可调度共享账号数）。
type supplyAccountCounter interface {
	CountActiveSupplyAccounts(ctx context.Context) (map[string]int, error)
}

// SupplyDemandBalanceService 供需平衡门的判定枢纽。
type SupplyDemandBalanceService struct {
	settings supplyDemandGateReader
	demand   supplyDemandStatsReader
	supply   supplyAccountCounter
}

// NewSupplyDemandBalanceService 构造判定枢纽。任一依赖为 nil 时对应读取会被判空并 fail-open。
func NewSupplyDemandBalanceService(
	settings supplyDemandGateReader,
	demand supplyDemandStatsReader,
	supply supplyAccountCounter,
) *SupplyDemandBalanceService {
	return &SupplyDemandBalanceService{settings: settings, demand: demand, supply: supply}
}

// activeUsers 读今日活跃用户数；读不到返回 (0,false) → 调用方按 fail-open 处理。
func (b *SupplyDemandBalanceService) activeUsers(ctx context.Context) (int, bool) {
	if b == nil || b.demand == nil {
		return 0, false
	}
	stats, err := b.demand.GetDashboardStats(ctx)
	if err != nil || stats == nil {
		if err != nil {
			slog.Warn("[SupplyDemandGate] failed to read demand stats, gate inactive this call", "error", err)
		}
		return 0, false
	}
	return int(stats.ActiveUsers), true
}

// supplyCounts 读按平台的可调度供给号数；读不到返回 (nil,false)。
func (b *SupplyDemandBalanceService) supplyCounts(ctx context.Context) (map[string]int, bool) {
	if b == nil || b.supply == nil {
		return nil, false
	}
	counts, err := b.supply.CountActiveSupplyAccounts(ctx)
	if err != nil {
		slog.Warn("[SupplyDemandGate] failed to count supply accounts, gate inactive this call", "error", err)
		return nil, false
	}
	return counts, true
}

// gateSettings 读阈值；nil 依赖时返回默认（门关）。
func (b *SupplyDemandBalanceService) gateSettings(ctx context.Context) *SupplyDemandGateSettings {
	if b == nil || b.settings == nil {
		return DefaultSupplyDemandGateSettings()
	}
	return b.settings.GetSupplyDemandGateSettings(ctx)
}

// AllowNewSupplier 判断某平台此刻是否还接受新共享者接入。
//
// 返回 nil = 放行；返回 ErrSupplierRejectedOversupplied = 供给过剩，拒绝。
// 门关、样本不足、任一读取失败都放行（见文件头）。
func (b *SupplyDemandBalanceService) AllowNewSupplier(ctx context.Context, platform string) error {
	if b == nil {
		return nil
	}
	gate := b.gateSettings(ctx)
	if !gate.supplierGateActive() {
		return nil
	}
	demand, ok := b.activeUsers(ctx)
	if !ok || demand < gate.SampleFloor {
		return nil // 冷启动 / 读不到需求：放行
	}
	counts, ok := b.supplyCounts(ctx)
	if !ok {
		return nil
	}
	supplyP := counts[normalizeSupplyPlatform(platform)]
	// 供给号数 > 活跃用户数 × 阈值 → 供给过剩。
	if float64(supplyP) > gate.MaxSuppliersPerUser*float64(demand) {
		return ErrSupplierRejectedOversupplied
	}
	return nil
}

// AllowNewConsumer 判断此刻是否还接受新用户注册（全局）。
//
// 返回 nil = 放行；返回 ErrRegistrationRejectedUndersupplied = 供给不足，拒绝。
func (b *SupplyDemandBalanceService) AllowNewConsumer(ctx context.Context) error {
	if b == nil {
		return nil
	}
	gate := b.gateSettings(ctx)
	if !gate.consumerGateActive() {
		return nil
	}
	demand, ok := b.activeUsers(ctx)
	if !ok || demand < gate.SampleFloor {
		return nil
	}
	counts, ok := b.supplyCounts(ctx)
	if !ok {
		return nil
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	// 全局供给号数 < 活跃用户数 × 阈值 → 供给不足。
	if float64(total) < gate.MinSuppliersPerUser*float64(demand) {
		return ErrRegistrationRejectedUndersupplied
	}
	return nil
}
