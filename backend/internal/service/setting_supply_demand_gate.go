// APEXONE-EXT: 双边市场——供需动态平衡门的阈值配置。
//
// 第七个 settings key。分家的理由与前几个一样（见 setting_supply_pool.go 头部）：
// 这组参数因第七种原因变动——它随「供给方与消费方谁多谁少」的实时对比而调整，
// 调整的人是运营，调整的时机是看见供需明显失衡之后，与其它几组的节奏毫无关系。
//
// # 这道门默认是关的，且必须默认关
//
// 它会**拒绝新用户注册**（供给远少于需求时）或**拒绝新共享者接入**（供给远多于
// 需求时）。这两件事都直接伤增长，只有运营在看过真实供需读数、并且确实想要用
// 「拒绝一侧」来维持平衡时才应该打开。所以 `Enabled` 默认 false，读失败也回退到
// 「关」——一道因为配置读不出来就把新用户挡在门外的闸，损失远大于它防的那点失衡。
//
// # 冷启动 fail-open
//
// `SampleFloor`（活跃用户样本下限）以下这道门整个不生效：平台早期活跃用户是个位数
// 时，任何比值都是噪声——0 个用户时供给比会是无穷大，会把所有新共享者都挡了；
// 反过来也一样。样本太小就别用比值做决策，直接放行。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// SettingKeySupplyDemandGate 供需平衡门的 settings key。
const SettingKeySupplyDemandGate = "supply_demand_gate_settings"

// 阈值上界。夹回区间而不是报错，与其它 settings 一致。
const (
	// supplyDemandGateSampleFloorMax 样本下限最大可配到 100 万活跃用户。
	supplyDemandGateSampleFloorMax = 1_000_000
	// supplyDemandGateRatioMax 两个比值最大可配到 1000（等同于「几乎不触发」）。
	supplyDemandGateRatioMax = 1000.0
)

// 默认值。门默认关，比值给一个中性起点——真正生效前运营必须按实测调。
const (
	// supplyDemandGateDefaultSampleFloor 活跃用户少于 20 时门不生效（冷启动）。
	supplyDemandGateDefaultSampleFloor = 20
	// supplyDemandGateDefaultMaxSuppliersPerUser 供给门起点：某平台活跃供给号数
	// 超过「活跃用户数 × 1.0」判为供给过剩，拒绝新共享者接入该平台。
	//
	// 只是起点。供给号能扛多少用户、用户有多活跃都是估的，运营要按健康度读数调。
	supplyDemandGateDefaultMaxSuppliersPerUser = 1.0
	// supplyDemandGateDefaultMinSuppliersPerUser 消费门起点：全局活跃供给号数
	// 低于「活跃用户数 × 0.05」（不到每 20 用户 1 个供给号）判为供给不足，拒绝新用户注册。
	supplyDemandGateDefaultMinSuppliersPerUser = 0.05
)

// SupplyDemandGateSettings 供需平衡门的阈值。
//
// 衡量口径是**头数比**（活跃供给号数 vs 活跃消费用户数），见 supply_demand_balance.go。
type SupplyDemandGateSettings struct {
	// Enabled 总开关。默认 false——见文件头，这道门伤增长，必须显式打开。
	Enabled bool `json:"enabled"`

	// SampleFloor 活跃用户样本下限。低于此值门整个不生效（冷启动 fail-open）。
	SampleFloor int `json:"sample_floor"`

	// MaxSuppliersPerUser 供给门阈值：某平台 `供给号数 / 活跃用户数 > 此值` → 供给过剩，
	// 拒绝新共享者接入该平台。0 = 关闭供给门（不因过剩拒绝任何人）。
	MaxSuppliersPerUser float64 `json:"max_suppliers_per_user"`
	// MinSuppliersPerUser 消费门阈值：全局 `供给号数 / 活跃用户数 < 此值` → 供给不足，
	// 拒绝新用户注册。0 = 关闭消费门（不因不足拒绝任何人）。
	MinSuppliersPerUser float64 `json:"min_suppliers_per_user"`
}

// DefaultSupplyDemandGateSettings 返回默认阈值：门关、样本下限 20、供给比 1.0、消费比 0.05。
func DefaultSupplyDemandGateSettings() *SupplyDemandGateSettings {
	return &SupplyDemandGateSettings{
		Enabled:             false,
		SampleFloor:         supplyDemandGateDefaultSampleFloor,
		MaxSuppliersPerUser: supplyDemandGateDefaultMaxSuppliersPerUser,
		MinSuppliersPerUser: supplyDemandGateDefaultMinSuppliersPerUser,
	}
}

// normalize 把越界值夹回区间。负数一律夹成 0（该侧门关），比值/样本上界夹回。
func (s *SupplyDemandGateSettings) normalize() {
	if s == nil {
		return
	}
	if s.SampleFloor < 0 {
		s.SampleFloor = 0
	}
	if s.SampleFloor > supplyDemandGateSampleFloorMax {
		s.SampleFloor = supplyDemandGateSampleFloorMax
	}
	if s.MaxSuppliersPerUser < 0 {
		s.MaxSuppliersPerUser = 0
	}
	if s.MaxSuppliersPerUser > supplyDemandGateRatioMax {
		s.MaxSuppliersPerUser = supplyDemandGateRatioMax
	}
	if s.MinSuppliersPerUser < 0 {
		s.MinSuppliersPerUser = 0
	}
	if s.MinSuppliersPerUser > supplyDemandGateRatioMax {
		s.MinSuppliersPerUser = supplyDemandGateRatioMax
	}
}

// supplierGateActive 供给门是否真的在起作用（总开关开 且 阈值 > 0）。
func (s *SupplyDemandGateSettings) supplierGateActive() bool {
	return s != nil && s.Enabled && s.MaxSuppliersPerUser > 0
}

// consumerGateActive 消费门是否真的在起作用。
func (s *SupplyDemandGateSettings) consumerGateActive() bool {
	return s != nil && s.Enabled && s.MinSuppliersPerUser > 0
}

// ============================================================================
// 进程内缓存。形态与 setting_supply_onboarding.go 一致，见那里的说明。
// ============================================================================

type cachedSupplyDemandGateSettings struct {
	settings  *SupplyDemandGateSettings
	expiresAt int64 // unix nano
}

var supplyDemandGateCache atomic.Value // *cachedSupplyDemandGateSettings
var supplyDemandGateSF singleflight.Group

const supplyDemandGateCacheTTL = 60 * time.Second
const supplyDemandGateErrorTTL = 5 * time.Second
const supplyDemandGateDBTimeout = 5 * time.Second

func invalidateSupplyDemandGateCache() {
	supplyDemandGateCache.Store(&cachedSupplyDemandGateSettings{})
	supplyDemandGateSF.Forget(SettingKeySupplyDemandGate)
}

// GetSupplyDemandGateSettings 读供需平衡门阈值，永不返回错误（读失败回退默认=门关）。
func (s *SettingService) GetSupplyDemandGateSettings(ctx context.Context) *SupplyDemandGateSettings {
	if s == nil || s.settingRepo == nil {
		return DefaultSupplyDemandGateSettings()
	}
	if cached, ok := supplyDemandGateCache.Load().(*cachedSupplyDemandGateSettings); ok {
		if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
			return cloneSupplyDemandGateSettings(cached.settings)
		}
	}

	result, err, _ := supplyDemandGateSF.Do(SettingKeySupplyDemandGate, func() (any, error) {
		if cached, ok := supplyDemandGateCache.Load().(*cachedSupplyDemandGateSettings); ok {
			if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
				return cloneSupplyDemandGateSettings(cached.settings), nil
			}
		}

		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), supplyDemandGateDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeySupplyDemandGate)
		if err != nil {
			settings := DefaultSupplyDemandGateSettings()
			ttl := supplyDemandGateErrorTTL
			if errors.Is(err, ErrSettingNotFound) {
				ttl = supplyDemandGateCacheTTL
			} else {
				slog.Warn("[SupplyDemandGate] failed to read gate settings, falling back to defaults",
					"error", err, "key", SettingKeySupplyDemandGate)
			}
			storeSupplyDemandGateCache(settings, ttl)
			return cloneSupplyDemandGateSettings(settings), nil
		}

		settings := parseSupplyDemandGateSettings(raw)
		storeSupplyDemandGateCache(settings, supplyDemandGateCacheTTL)
		return cloneSupplyDemandGateSettings(settings), nil
	})
	if err != nil {
		return DefaultSupplyDemandGateSettings()
	}
	if settings, ok := result.(*SupplyDemandGateSettings); ok && settings != nil {
		return settings
	}
	return DefaultSupplyDemandGateSettings()
}

// SetSupplyDemandGateSettings 写阈值。越界夹回区间再回读（与接入上限一致）。
func (s *SettingService) SetSupplyDemandGateSettings(ctx context.Context, settings *SupplyDemandGateSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	settings.normalize()

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal supply demand gate settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeySupplyDemandGate, string(data)); err != nil {
		return fmt.Errorf("save supply demand gate settings: %w", err)
	}
	invalidateSupplyDemandGateCache()
	return nil
}

// parseSupplyDemandGateSettings 解析库里那份 JSON。坏掉退回默认（门关）。
func parseSupplyDemandGateSettings(raw string) *SupplyDemandGateSettings {
	settings := DefaultSupplyDemandGateSettings()
	if raw == "" {
		return settings
	}
	var parsed SupplyDemandGateSettings
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[SupplyDemandGate] gate settings JSON is corrupt, falling back to defaults",
			"error", err, "key", SettingKeySupplyDemandGate)
		return settings
	}
	parsed.normalize()
	return &parsed
}

func storeSupplyDemandGateCache(settings *SupplyDemandGateSettings, ttl time.Duration) {
	supplyDemandGateCache.Store(&cachedSupplyDemandGateSettings{
		settings:  cloneSupplyDemandGateSettings(settings),
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

func cloneSupplyDemandGateSettings(settings *SupplyDemandGateSettings) *SupplyDemandGateSettings {
	if settings == nil {
		return DefaultSupplyDemandGateSettings()
	}
	clone := *settings
	return &clone
}
