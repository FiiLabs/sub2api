// APEXONE-EXT: 双边市场——观察期与下线排空的参数。
//
// 第三个 settings key，理由与前两个分家的理由一样（见 setting_supply_pool.go 头部）：
// 这组参数因第三种原因变动——结算参数动的是钱，池配置动的是路由，这里动的是
// **一个陌生人的账号什么时候可以站到付费消费者面前**。三者的变更节奏、变更人、
// 出事时要回看的审计记录都不一样，合成一个 key 就分不清了。
//
// # 为什么默认是关的
//
// `Enabled` 默认 false，也就是**默认没有任何东西会自动入池**。这不是保守过头：
// 入池 = 把一个平台没验证过的、别人的订阅推到真实付费流量前面。交接件定的起步
// 形态是邀请制人工核验，自动入池是运营者在看清楚封禁率之后才该打开的开关。
// 关着的时候观察期流程照常记录探测结果，只是不做最后那一下 promote——
// 管理员在账号页手工把号设成可调度，本服务会把 supply_state 对齐成 active（见
// supplier_lifecycle_service.go 的 reconcile），所以「手工放行」这条路一直是通的。
//
// 读失败一律回退到这里的默认值（Enabled=false）：读不到配置时不promote，
// 与其他两个 key 的 fail-closed 同向。排空窗是个例外——它不是安全属性，
// 读不到就用默认的 10 分钟，而不是 0（0 会让「优雅下线」静默退化成「立即拔出」）。
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

// SettingKeySupplyProbation 观察期参数的 settings key。
const SettingKeySupplyProbation = "supply_probation_settings"

// 参数上下限。写路径夹回区间而不是报错的那几个，理由见 SetSupplyProbationSettings。
const (
	// SupplyProbationMinObservationMinutesMax 观察窗上限 30 天。
	// 比这更长的观察期在运营上等于「不打算放行」，那应该关掉自动入池而不是设一个天文数字。
	SupplyProbationMinObservationMinutesMax = 30 * 24 * 60
	// SupplyProbationRequiredSuccessesMax 连续成功次数上限。
	SupplyProbationRequiredSuccessesMax = 20
	// SupplyProbationProbeIntervalMinutesMin 探测间隔下限。
	//
	// 每一次探测都是一个真实的上游推理请求，花的是**供给者自己**的额度。
	// 不设下限的话，一个填成 1 分钟的配置会在观察期里把人家的额度当探针耗材烧。
	SupplyProbationProbeIntervalMinutesMin = 5
	// SupplyProbationProbeIntervalMinutesMax 探测间隔上限 24 小时。
	SupplyProbationProbeIntervalMinutesMax = 24 * 60
	// SupplyProbationDrainWindowMinutesMax 排空窗上限 24 小时。
	//
	// 排空窗的意义是「等在途请求自己结束」，那是分钟级的事。设成几天只会让
	// 供给者的号在一个既不接单、也没真正下线的中间态里挂很久。
	SupplyProbationDrainWindowMinutesMax = 24 * 60
)

// 默认值。
const (
	supplyProbationDefaultMinObservationMinutes = 60
	supplyProbationDefaultRequiredSuccesses     = 2
	supplyProbationDefaultProbeIntervalMinutes  = 15
	supplyProbationDefaultDrainWindowMinutes    = 10
)

// SupplyProbationSettings 是观察期与排空的全部可调参数。
type SupplyProbationSettings struct {
	// Enabled 自动入池总开关。关着时只探测、只记录，不 promote。
	Enabled bool `json:"enabled"`
	// MinObservationMinutes 从进入观察期算起，最少要观察多久才可能入池。
	//
	// 与 RequiredSuccesses 是**并且**的关系：探测再顺利也得等满这段时间。
	// 只看成功次数的话，一个刚挂上来的号在几次探测之内就能入池，观察期就只是
	// 「连通性检查」而不是「观察」了——而我们真正想看的是它在一段时间里稳不稳。
	MinObservationMinutes int `json:"min_observation_minutes"`
	// RequiredSuccesses 需要连续成功几次探测。中间失败一次计数清零。
	RequiredSuccesses int `json:"required_successes"`
	// ProbeIntervalMinutes 同一个账号两次探测之间至少隔多久。
	ProbeIntervalMinutes int `json:"probe_interval_minutes"`
	// ProbeModel 探测用的模型 id。空 = 用平台默认测试模型。
	ProbeModel string `json:"probe_model"`
	// DrainWindowMinutes 优雅下线的排空窗：停止接新单后，等多久才转入终态。
	//
	// **这不是一个硬排空**——平台没有能力打断已经在流的请求，这个窗口是一段
	// 礼貌等待时间，也是供给者反悔（取消下线）的窗口。连接级 draining 是后续的事。
	DrainWindowMinutes int `json:"drain_window_minutes"`
}

// DefaultSupplyProbationSettings 返回「不自动入池」的默认配置。
func DefaultSupplyProbationSettings() *SupplyProbationSettings {
	return &SupplyProbationSettings{
		Enabled:               false,
		MinObservationMinutes: supplyProbationDefaultMinObservationMinutes,
		RequiredSuccesses:     supplyProbationDefaultRequiredSuccesses,
		ProbeIntervalMinutes:  supplyProbationDefaultProbeIntervalMinutes,
		DrainWindowMinutes:    supplyProbationDefaultDrainWindowMinutes,
	}
}

// normalize 把越界/缺省值夹回可用区间。
//
// 读路径也调它：一份手工改坏的 JSON（比如 probe_interval_minutes: 0）不该让
// 观察期任务每秒钟去戳一次别人的账号。配置的容错方向永远是「退回默认」。
func (s *SupplyProbationSettings) normalize() {
	if s == nil {
		return
	}
	if s.MinObservationMinutes < 0 {
		s.MinObservationMinutes = 0
	}
	if s.MinObservationMinutes > SupplyProbationMinObservationMinutesMax {
		s.MinObservationMinutes = SupplyProbationMinObservationMinutesMax
	}
	if s.RequiredSuccesses <= 0 {
		s.RequiredSuccesses = supplyProbationDefaultRequiredSuccesses
	}
	if s.RequiredSuccesses > SupplyProbationRequiredSuccessesMax {
		s.RequiredSuccesses = SupplyProbationRequiredSuccessesMax
	}
	if s.ProbeIntervalMinutes < SupplyProbationProbeIntervalMinutesMin {
		s.ProbeIntervalMinutes = SupplyProbationProbeIntervalMinutesMin
	}
	if s.ProbeIntervalMinutes > SupplyProbationProbeIntervalMinutesMax {
		s.ProbeIntervalMinutes = SupplyProbationProbeIntervalMinutesMax
	}
	if s.DrainWindowMinutes < 0 {
		s.DrainWindowMinutes = 0
	}
	if s.DrainWindowMinutes > SupplyProbationDrainWindowMinutesMax {
		s.DrainWindowMinutes = SupplyProbationDrainWindowMinutesMax
	}
}

// ObservationWindow 观察窗时长。
func (s *SupplyProbationSettings) ObservationWindow() time.Duration {
	if s == nil {
		return time.Duration(supplyProbationDefaultMinObservationMinutes) * time.Minute
	}
	return time.Duration(s.MinObservationMinutes) * time.Minute
}

// ProbeInterval 两次探测的最小间隔。
func (s *SupplyProbationSettings) ProbeInterval() time.Duration {
	if s == nil {
		return time.Duration(supplyProbationDefaultProbeIntervalMinutes) * time.Minute
	}
	return time.Duration(s.ProbeIntervalMinutes) * time.Minute
}

// DrainWindow 排空窗时长。
func (s *SupplyProbationSettings) DrainWindow() time.Duration {
	if s == nil {
		return time.Duration(supplyProbationDefaultDrainWindowMinutes) * time.Minute
	}
	return time.Duration(s.DrainWindowMinutes) * time.Minute
}

// ============================================================================
// 进程内缓存。形态与 setting_supplier.go / setting_supply_pool.go 一致。
// ============================================================================

type cachedSupplyProbationSettings struct {
	settings  *SupplyProbationSettings
	expiresAt int64 // unix nano
}

var supplyProbationCache atomic.Value // *cachedSupplyProbationSettings
var supplyProbationSF singleflight.Group

const supplyProbationCacheTTL = 60 * time.Second
const supplyProbationErrorTTL = 5 * time.Second
const supplyProbationDBTimeout = 5 * time.Second

func invalidateSupplyProbationCache() {
	supplyProbationCache.Store(&cachedSupplyProbationSettings{})
	supplyProbationSF.Forget(SettingKeySupplyProbation)
}

// GetSupplyProbationSettings 读观察期参数，永不返回错误。
func (s *SettingService) GetSupplyProbationSettings(ctx context.Context) *SupplyProbationSettings {
	if s == nil || s.settingRepo == nil {
		return DefaultSupplyProbationSettings()
	}
	if cached, ok := supplyProbationCache.Load().(*cachedSupplyProbationSettings); ok {
		if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
			return cloneSupplyProbationSettings(cached.settings)
		}
	}

	result, err, _ := supplyProbationSF.Do(SettingKeySupplyProbation, func() (any, error) {
		if cached, ok := supplyProbationCache.Load().(*cachedSupplyProbationSettings); ok {
			if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
				return cloneSupplyProbationSettings(cached.settings), nil
			}
		}

		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), supplyProbationDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeySupplyProbation)
		if err != nil {
			settings := DefaultSupplyProbationSettings()
			ttl := supplyProbationErrorTTL
			if errors.Is(err, ErrSettingNotFound) {
				ttl = supplyProbationCacheTTL
			} else {
				slog.Warn("[SupplyProbation] failed to read probation settings, auto-promotion stays disabled",
					"error", err, "key", SettingKeySupplyProbation)
			}
			storeSupplyProbationCache(settings, ttl)
			return cloneSupplyProbationSettings(settings), nil
		}

		settings := parseSupplyProbationSettings(raw)
		storeSupplyProbationCache(settings, supplyProbationCacheTTL)
		return cloneSupplyProbationSettings(settings), nil
	})
	if err != nil {
		return DefaultSupplyProbationSettings()
	}
	if settings, ok := result.(*SupplyProbationSettings); ok && settings != nil {
		return settings
	}
	return DefaultSupplyProbationSettings()
}

// SetSupplyProbationSettings 写观察期参数。
//
// 越界值**夹回区间**而不是报错，与结算参数（那边越界直接拒）刻意不同：结算参数
// 越界改的是钱的分法，管理员必须知道自己填错了；这里越界改的是节奏，夹回一个
// 安全值再回读给他看，比拦下来更顺手。回读是接口契约的一部分——管理端会把返回值
// 写回表单，所以他看到的一定是库里真正生效的那份。
func (s *SettingService) SetSupplyProbationSettings(ctx context.Context, settings *SupplyProbationSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	settings.normalize()

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal supply probation settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeySupplyProbation, string(data)); err != nil {
		return fmt.Errorf("save supply probation settings: %w", err)
	}
	invalidateSupplyProbationCache()
	return nil
}

func parseSupplyProbationSettings(raw string) *SupplyProbationSettings {
	settings := DefaultSupplyProbationSettings()
	if raw == "" {
		return settings
	}
	var parsed SupplyProbationSettings
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[SupplyProbation] probation settings JSON is corrupt, auto-promotion stays disabled",
			"error", err, "key", SettingKeySupplyProbation)
		return settings
	}
	parsed.normalize()
	return &parsed
}

func storeSupplyProbationCache(settings *SupplyProbationSettings, ttl time.Duration) {
	supplyProbationCache.Store(&cachedSupplyProbationSettings{
		settings:  cloneSupplyProbationSettings(settings),
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

func cloneSupplyProbationSettings(settings *SupplyProbationSettings) *SupplyProbationSettings {
	if settings == nil {
		return DefaultSupplyProbationSettings()
	}
	clone := *settings
	return &clone
}
