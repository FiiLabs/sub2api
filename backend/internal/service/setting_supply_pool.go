// APEXONE-EXT: 双边市场——供给池/自营池的路由配置。
//
// 只装两个分组 id 和一个开关，形态与 setting_supplier.go 完全一样（同样的
// atomic + singleflight + fail-closed），理由也一样：这是计费/调度热路径旁边的读，
// 且读不到时必须退回「不启用」而不是猜。
//
// **刻意与结算参数分开成两个 key**：这两组配置因完全不同的原因变动——结算参数动的
// 是钱（比例、冻结窗），池配置动的是路由（哪个分组溢出到哪个分组）。合成一个 key
// 会让「调一下分成比例」和「换一个兜底池」共用一次审计记录，事后翻账分不清是谁改了什么。
//
// 为什么不是 Group 上的一个 `overflow_group_id` 字段（那样更通用）：那要改 ent schema、
// 加迁移，还要把字段一路穿过 admin 的 group 创建/更新 DTO、mapper、repo 的 set/clear、
// 分组复制——七八处上游文件，全是合并冲突热区。而首版切片只有**一个**供给池和**一个**
// 自营池，一对 id 就够了。等真需要「任意分组各自配溢出目标」时再加字段不迟，那时
// 这个 key 退化成默认值即可。
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

// SettingKeySupplyPool 供给池路由配置的 settings key。
const SettingKeySupplyPool = "supply_pool_settings"

// SupplyPoolPair 描述一条「供给池干涸时溢出到自营池」的路由规则（一个平台一对）。
type SupplyPoolPair struct {
	// SupplyGroupID 供给池分组 id。只有解析后落在这个分组上的请求才会溢出。
	SupplyGroupID int64 `json:"supply_group_id"`
	// OverflowGroupID 兜底分组 id（自营池）。
	OverflowGroupID int64 `json:"overflow_group_id"`
	// DailyOverflowLimit 当日最多溢出多少次，0 = 不限量（仍然计数）。
	DailyOverflowLimit int `json:"daily_overflow_limit"`
}

// SupplyPoolSettings 描述各平台「供给池干涸时溢出到自营池」的路由规则。
//
// 零值 = 不启用，调度行为与上游原逻辑一字不差。
//
// **按平台多对**（2026-09 加）：顶层的三个单字段是 anthropic（Claude）那一对，保持不变、
// 向后兼容——存量配置无 Pools 时行为与从前逐字一致。其它平台（openai 等）的池放进 Pools，
// 键是账号 Platform 值。这样一个部署可以同时跑 Claude 供给池 + OpenAI 供给池，各自的
// 供给组/兜底组/每日上限互不相干。文件头当年预告的「下一刀」就是这个。
type SupplyPoolSettings struct {
	// Enabled 总开关（对所有平台池生效）。
	Enabled bool `json:"enabled"`
	// SupplyGroupID / OverflowGroupID / DailyOverflowLimit 是 anthropic 默认池（向后兼容）。
	//
	// 这个门开得很窄是有意的：如果「任何分组没号都往自营池溢」，一个配错的空分组
	// 会静默地拿平台自有账号服务，成本全由平台吃，而且现象是「一切正常」。
	SupplyGroupID int64 `json:"supply_group_id"`
	// OverflowGroupID 兜底分组 id（自营池）。
	OverflowGroupID int64 `json:"overflow_group_id"`
	// DailyOverflowLimit 当日最多溢出多少次，0 = 不限量（仍然计数）。见 supply_overflow_budget.go。
	DailyOverflowLimit int `json:"daily_overflow_limit"`
	// Pools 是非 anthropic 平台的池，键 = 账号 Platform（如 "openai"）。可空。
	Pools map[string]SupplyPoolPair `json:"pools,omitempty"`
}

// DefaultSupplyPoolSettings 返回「不启用」的默认配置。
func DefaultSupplyPoolSettings() *SupplyPoolSettings {
	return &SupplyPoolSettings{}
}

// poolFor 返回某平台的池对。anthropic（或空串）用顶层单字段；其它平台查 Pools。
// 第二返回值表示这个平台是否配了池（供给组 id 为正）。
func (s *SupplyPoolSettings) poolFor(platform string) (SupplyPoolPair, bool) {
	if s == nil {
		return SupplyPoolPair{}, false
	}
	if platform == "" || platform == PlatformAnthropic {
		pair := SupplyPoolPair{
			SupplyGroupID:      s.SupplyGroupID,
			OverflowGroupID:    s.OverflowGroupID,
			DailyOverflowLimit: s.DailyOverflowLimit,
		}
		return pair, pair.SupplyGroupID > 0
	}
	pair, ok := s.Pools[platform]
	return pair, ok && pair.SupplyGroupID > 0
}

// supplyGroupIDFor 返回某平台的供给组 id（接入时挂号用）。0 = 该平台未配供给池。
func (s *SupplyPoolSettings) supplyGroupIDFor(platform string) int64 {
	pair, ok := s.poolFor(platform)
	if !ok {
		return 0
	}
	return pair.SupplyGroupID
}

// allPools 汇总所有平台的池对（anthropic 顶层 + Pools），供溢出解析遍历。
func (s *SupplyPoolSettings) allPools() []SupplyPoolPair {
	if s == nil {
		return nil
	}
	out := make([]SupplyPoolPair, 0, 1+len(s.Pools))
	if s.SupplyGroupID > 0 {
		out = append(out, SupplyPoolPair{s.SupplyGroupID, s.OverflowGroupID, s.DailyOverflowLimit})
	}
	for _, p := range s.Pools {
		if p.SupplyGroupID > 0 {
			out = append(out, p)
		}
	}
	return out
}

// overflowTargetFor 返回 resolvedGroupID 这个分组应当溢出到的目标分组。
//
// 把「是否该溢出」的全部判据收在一个函数里，调用方不必知道任何一条规则。
// 现按平台多对：在所有已配置的池里找供给组匹配的那一对，返回它的兜底组。
func (s *SupplyPoolSettings) overflowTargetFor(resolvedGroupID int64) (int64, bool) {
	if s == nil || !s.Enabled {
		return 0, false
	}
	for _, p := range s.allPools() {
		if p.OverflowGroupID <= 0 {
			continue
		}
		// 自己溢出到自己 = 把一次失败的调度原样再跑一遍，纯浪费。
		if p.SupplyGroupID == p.OverflowGroupID {
			continue
		}
		if resolvedGroupID == p.SupplyGroupID {
			return p.OverflowGroupID, true
		}
	}
	return 0, false
}

// overflowLimitFor 返回某供给组的当日溢出上限（0 = 不限）。找不到对应池返回 0。
func (s *SupplyPoolSettings) overflowLimitFor(supplyGroupID int64) int {
	if s == nil {
		return 0
	}
	for _, p := range s.allPools() {
		if p.SupplyGroupID == supplyGroupID {
			return p.DailyOverflowLimit
		}
	}
	return 0
}

// ============================================================================
// 进程内缓存。形态与 setting_supplier.go 一致，见那里的说明。
// ============================================================================

type cachedSupplyPoolSettings struct {
	settings  *SupplyPoolSettings
	expiresAt int64 // unix nano
}

var supplyPoolCache atomic.Value // *cachedSupplyPoolSettings
var supplyPoolSF singleflight.Group

const supplyPoolCacheTTL = 60 * time.Second
const supplyPoolErrorTTL = 5 * time.Second
const supplyPoolDBTimeout = 5 * time.Second

func invalidateSupplyPoolCache() {
	supplyPoolCache.Store(&cachedSupplyPoolSettings{})
	supplyPoolSF.Forget(SettingKeySupplyPool)
}

// GetSupplyPoolSettings 读池路由配置，永不返回错误。
//
// fail-closed：读不到就不溢出。溢出的代价是平台按自营成本供货却按供给池价收费，
// 这个决定必须来自一份真的读到了的配置，不能来自一次猜测。
func (s *SettingService) GetSupplyPoolSettings(ctx context.Context) *SupplyPoolSettings {
	if s == nil || s.settingRepo == nil {
		return DefaultSupplyPoolSettings()
	}
	if cached, ok := supplyPoolCache.Load().(*cachedSupplyPoolSettings); ok {
		if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
			return cloneSupplyPoolSettings(cached.settings)
		}
	}

	result, err, _ := supplyPoolSF.Do(SettingKeySupplyPool, func() (any, error) {
		if cached, ok := supplyPoolCache.Load().(*cachedSupplyPoolSettings); ok {
			if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
				return cloneSupplyPoolSettings(cached.settings), nil
			}
		}

		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), supplyPoolDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeySupplyPool)
		if err != nil {
			settings := DefaultSupplyPoolSettings()
			ttl := supplyPoolErrorTTL
			if errors.Is(err, ErrSettingNotFound) {
				ttl = supplyPoolCacheTTL
			} else {
				slog.Warn("[SupplyPool] failed to read pool settings, overflow stays disabled",
					"error", err, "key", SettingKeySupplyPool)
			}
			storeSupplyPoolCache(settings, ttl)
			return cloneSupplyPoolSettings(settings), nil
		}

		settings := parseSupplyPoolSettings(raw)
		storeSupplyPoolCache(settings, supplyPoolCacheTTL)
		return cloneSupplyPoolSettings(settings), nil
	})
	if err != nil {
		return DefaultSupplyPoolSettings()
	}
	if settings, ok := result.(*SupplyPoolSettings); ok && settings != nil {
		return settings
	}
	return DefaultSupplyPoolSettings()
}

// SetSupplyPoolSettings 写池路由配置。
//
// 只在开关打开时校验：关着的时候两个 id 是什么都不影响任何请求，拦下来只会妨碍
// 管理员分两步（先填 id、再打开）配置。
//
// 这里**不**校验分组是否存在。分组可能在配置之后被删掉，配置侧校验给不出「以后
// 也一直有效」的保证，真正的兜底在调度侧：溢出目标解析不出来就退回原错误。
// 把存在性校验放在这里只会带来一种虚假的安全感，外加一个 groupRepo 依赖。
func (s *SettingService) SetSupplyPoolSettings(ctx context.Context, settings *SupplyPoolSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	// 负数在闸门那边与 0 同义（不限量），但存下去会让面板显示一个看起来像限制、
	// 实际不限制的数字。夹成 0，让「不限量」在库里只有一种写法。anthropic 顶层 + 各平台池同处理。
	if settings.DailyOverflowLimit < 0 {
		settings.DailyOverflowLimit = 0
	}
	for k, p := range settings.Pools {
		if p.DailyOverflowLimit < 0 {
			p.DailyOverflowLimit = 0
			settings.Pools[k] = p
		}
	}
	if settings.Enabled {
		// anthropic 顶层池：仅当填了供给组才校验（允许只配 openai 池、anthropic 不配）。
		if settings.SupplyGroupID > 0 {
			if settings.OverflowGroupID <= 0 {
				return fmt.Errorf("overflow_group_id must be a positive group id when supply_group_id is set")
			}
			if settings.SupplyGroupID == settings.OverflowGroupID {
				return fmt.Errorf("overflow_group_id must differ from supply_group_id")
			}
		}
		// 各平台池：配了就要成对且不自溢。
		for platform, p := range settings.Pools {
			if p.SupplyGroupID <= 0 {
				return fmt.Errorf("pools[%s].supply_group_id must be a positive group id", platform)
			}
			if p.OverflowGroupID <= 0 {
				return fmt.Errorf("pools[%s].overflow_group_id must be a positive group id", platform)
			}
			if p.SupplyGroupID == p.OverflowGroupID {
				return fmt.Errorf("pools[%s].overflow_group_id must differ from supply_group_id", platform)
			}
		}
		// 开着却一个池都没配 = 没有任何效果，挡下来避免管理员以为配好了。
		if settings.SupplyGroupID <= 0 && len(settings.Pools) == 0 {
			return fmt.Errorf("at least one pool (top-level anthropic or pools[...]) must be configured when enabled")
		}
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal supply pool settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeySupplyPool, string(data)); err != nil {
		return fmt.Errorf("save supply pool settings: %w", err)
	}
	invalidateSupplyPoolCache()
	return nil
}

func parseSupplyPoolSettings(raw string) *SupplyPoolSettings {
	settings := DefaultSupplyPoolSettings()
	if raw == "" {
		return settings
	}
	var parsed SupplyPoolSettings
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[SupplyPool] pool settings JSON is corrupt, overflow stays disabled",
			"error", err, "key", SettingKeySupplyPool)
		return settings
	}
	return &parsed
}

func storeSupplyPoolCache(settings *SupplyPoolSettings, ttl time.Duration) {
	supplyPoolCache.Store(&cachedSupplyPoolSettings{
		settings:  cloneSupplyPoolSettings(settings),
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

func cloneSupplyPoolSettings(settings *SupplyPoolSettings) *SupplyPoolSettings {
	if settings == nil {
		return DefaultSupplyPoolSettings()
	}
	clone := *settings
	// 深拷贝 Pools：浅拷贝会让缓存副本与调用方共享同一个 map，一方改动串到另一方。
	if settings.Pools != nil {
		clone.Pools = make(map[string]SupplyPoolPair, len(settings.Pools))
		for k, v := range settings.Pools {
			clone.Pools[k] = v
		}
	}
	return &clone
}
