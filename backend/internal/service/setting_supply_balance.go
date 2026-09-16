// APEXONE-EXT: 双边市场——新会话产出均衡的开关与带宽。
//
// 第八个 settings key。行为本身在 gateway_scheduling_balance.go，这里只管「开没开、
// 分档多宽」两个数。
//
// # 为什么不是 config.yaml
//
// 这两个数最初放在 gateway.scheduling 下面，与 prefer_soonest_reset 那批邻居同处。
// 那是照着代码的形状放的，不是照着**部署的形状**放的：现网跑在 TEE（Phala CVM）里，
// 配置经 compose 进容器，改一个字段意味着 composeHash 变 → 重新发 proof reference →
// 整套重新部署并重新远程证明。为一个「先开着看两天，不行就关掉」的调优开关付这个
// 代价，实际结果一定是没人敢动它——那它就等于不可配。
//
// 放进 settings 表之后它与另外七个 supply key 同一个脾气：admin 界面改、60 秒生效、
// 镜像与证明都不动。
//
// # 读失败时关闭，而不是保持上一次
//
// 与其余几个 key 的 fail-closed 同向，但这里的"closed"含义要说清楚：它退回的是
// **改动之前的调度行为**（优先级 → 负载率 → LRU），不是"拒绝服务"。均衡是一层
// 锦上添花的排序，数据库抖动时最安全的答案是当它不存在。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// SettingKeySupplyBalance 产出均衡配置的 settings key。
const SettingKeySupplyBalance = "supply_balance_settings"

const (
	// SupplyBalanceBandUSDDefault 分档带宽默认值（美元，按官方牌价）。
	//
	// 比单次请求的量级大得多（一次 opus 长上下文请求约 $0.2 牌价），又远小于
	// 一个号的日产能（满额 Max 20× 约 $188/天）。所以它既不会被噪声抖动，
	// 也不会把真正的差距抹平。
	SupplyBalanceBandUSDDefault = 5.0
	// SupplyBalanceBandUSDMax 带宽上限。
	//
	// 设上限是为了挡住「多打一个零」：填成 500 而不是 50，界面上不会有任何异常，
	// 只是从此所有号都落在同一档里，均衡静默退化成纯 LRU——而这正是它要修的那个毛病。
	SupplyBalanceBandUSDMax = 1000
)

// SupplyBalanceSettings 是产出均衡的全部可配内容。
type SupplyBalanceSettings struct {
	// Enabled 总开关。默认 false，行为与改动前逐字一致。
	Enabled bool `json:"enabled"`
	// BandUSD 分档带宽。同一档内仍按 LRU 选。
	//
	// 不做成「严格选最小」是因为这个判据带缓存：严格最小会让一批并发的新会话
	// 在缓存刷新前全部涌向同一个号，把一种不均衡换成另一种。
	BandUSD float64 `json:"band_usd"`
}

// DefaultSupplyBalanceSettings 返回「不做均衡」的默认配置。
func DefaultSupplyBalanceSettings() *SupplyBalanceSettings {
	return &SupplyBalanceSettings{Enabled: false, BandUSD: SupplyBalanceBandUSDDefault}
}

// Band 实际生效的带宽，非正数/非有限值取默认。
//
// 读路径也夹：一个被手工改成 0 的带宽会让 floor(cost/band) 变成 ±Inf，
// 分档退化成「严格选最小」——正是这层刻意避开的那个涌向同一个号的形态，
// 而且不会有任何报错。
func (s *SupplyBalanceSettings) Band() float64 {
	if s == nil {
		return SupplyBalanceBandUSDDefault
	}
	if s.BandUSD <= 0 || math.IsNaN(s.BandUSD) || math.IsInf(s.BandUSD, 0) {
		return SupplyBalanceBandUSDDefault
	}
	if s.BandUSD > SupplyBalanceBandUSDMax {
		return SupplyBalanceBandUSDMax
	}
	return s.BandUSD
}

func (s *SupplyBalanceSettings) normalize() {
	if s == nil {
		return
	}
	s.BandUSD = s.Band()
}

// ============================================================================
// 进程内缓存。形态与 setting_supply_probation.go 一致。
// ============================================================================

type cachedSupplyBalanceSettings struct {
	settings  *SupplyBalanceSettings
	expiresAt int64 // unix nano
}

var supplyBalanceCache atomic.Value // *cachedSupplyBalanceSettings
var supplyBalanceSF singleflight.Group

const supplyBalanceCacheTTL = 60 * time.Second
const supplyBalanceErrorTTL = 5 * time.Second
const supplyBalanceDBTimeout = 5 * time.Second

func invalidateSupplyBalanceCache() {
	supplyBalanceCache.Store(&cachedSupplyBalanceSettings{})
	supplyBalanceSF.Forget(SettingKeySupplyBalance)
}

// GetSupplyBalanceSettings 读均衡配置，永不返回错误。读不到 = 不做均衡。
//
// 这是调度热路径上的读，所以与 setting_supplier.go 同样的 atomic + singleflight：
// 缓存命中时没有锁、没有分配；未命中时同一时刻只有一个 goroutine 去查库。
func (s *SettingService) GetSupplyBalanceSettings(ctx context.Context) *SupplyBalanceSettings {
	if s == nil || s.settingRepo == nil {
		return DefaultSupplyBalanceSettings()
	}
	if cached, ok := supplyBalanceCache.Load().(*cachedSupplyBalanceSettings); ok {
		if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
			return cloneSupplyBalanceSettings(cached.settings)
		}
	}

	result, err, _ := supplyBalanceSF.Do(SettingKeySupplyBalance, func() (any, error) {
		if cached, ok := supplyBalanceCache.Load().(*cachedSupplyBalanceSettings); ok {
			if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
				return cloneSupplyBalanceSettings(cached.settings), nil
			}
		}

		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), supplyBalanceDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeySupplyBalance)
		if err != nil {
			settings := DefaultSupplyBalanceSettings()
			ttl := supplyBalanceErrorTTL
			if errors.Is(err, ErrSettingNotFound) {
				ttl = supplyBalanceCacheTTL
			} else {
				slog.Warn("[SupplyBalance] failed to read balance settings, scheduling stays on the legacy order",
					"error", err, "key", SettingKeySupplyBalance)
			}
			storeSupplyBalanceCache(settings, ttl)
			return cloneSupplyBalanceSettings(settings), nil
		}

		settings := parseSupplyBalanceSettings(raw)
		storeSupplyBalanceCache(settings, supplyBalanceCacheTTL)
		return cloneSupplyBalanceSettings(settings), nil
	})
	if err != nil {
		return DefaultSupplyBalanceSettings()
	}
	if settings, ok := result.(*SupplyBalanceSettings); ok && settings != nil {
		return settings
	}
	return DefaultSupplyBalanceSettings()
}

// SetSupplyBalanceSettings 写均衡配置。
//
// 越界**夹回**而不是报错（与观察期那组同向、与奖励那组相反）：这一组不决定发多少钱，
// 只决定新会话落到哪个号上，填过头的带宽收敛成一个安全值比拦下整次保存更顺手。
// 调用方回读返回值即可看到真正生效的数。
func (s *SettingService) SetSupplyBalanceSettings(ctx context.Context, settings *SupplyBalanceSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	settings.normalize()

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal supply balance settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeySupplyBalance, string(data)); err != nil {
		return fmt.Errorf("save supply balance settings: %w", err)
	}
	invalidateSupplyBalanceCache()
	return nil
}

func parseSupplyBalanceSettings(raw string) *SupplyBalanceSettings {
	settings := DefaultSupplyBalanceSettings()
	if raw == "" {
		return settings
	}
	var parsed SupplyBalanceSettings
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[SupplyBalance] balance settings JSON is corrupt, scheduling stays on the legacy order",
			"error", err, "key", SettingKeySupplyBalance)
		return settings
	}
	parsed.normalize()
	return &parsed
}

func storeSupplyBalanceCache(settings *SupplyBalanceSettings, ttl time.Duration) {
	supplyBalanceCache.Store(&cachedSupplyBalanceSettings{
		settings:  cloneSupplyBalanceSettings(settings),
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

func cloneSupplyBalanceSettings(settings *SupplyBalanceSettings) *SupplyBalanceSettings {
	if settings == nil {
		return DefaultSupplyBalanceSettings()
	}
	clone := *settings
	return &clone
}
