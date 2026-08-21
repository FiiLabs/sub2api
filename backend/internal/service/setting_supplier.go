// APEXONE-EXT: 双边市场——结算参数配置。
//
// 三个参数装在**一个 JSON key** 里（`supplier_settlement_settings`），而不是散成三个
// 标量 key。理由是这三个数必须一起看：比例调高而冻结窗不动，等于放大了拒付敞口；
// 单独打开总开关而比例还是 0，等于白跑一遍结算路径。装在一起，读是一次、写是一次、
// 审计是一条，运营改配置时看到的就是完整的一组。
//
// 这也让本文件与上游的 settings 主链路完全解耦：没有动 domain_constants.go 的 key 清单、
// 没有动 setting_parse.go 的默认值表、没有动 admin 那个巨大的 SettingsUpdateRequest。
// 上游合并时这里是纯新增文件，冲突面积为零。形态照抄 rectifier / beta_policy 那几组
// JSON key 设置（见 setting_features.go）。
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

// SettingKeySupplierSettlement 结算参数的 settings key。
const SettingKeySupplierSettlement = "supplier_settlement_settings"

// 参数边界。clamp 而不是报错：这些值出现在计费热路径上，
// 一个畸形的配置值不该让线上所有请求的计费失败。
const (
	// SupplierShareRatioMax 分成比例上限。1.0 = 全额转给供给者（平台零毛利），
	// 超过 1.0 就是每笔请求平台倒贴，任何情况下都不该发生。
	SupplierShareRatioMax = 1.0
	// SupplierFreezeHoursMax 冻结窗上限 90 天。比大多数支付通道的拒付窗更长，
	// 再长就不是风控而是拖欠了。
	SupplierFreezeHoursMax = 24 * 90
	// SupplierFreezeHoursDefault 默认冻结窗（30 天）。
	//
	// 这个数必须 ≥ 支付通道实际拒付窗，不变量「已释放 = 拒付安全」才成立。
	// 代码侧只 clamp 到上面那个上限，**拦不住「配小了」**——配小了的表现是
	// 冻结期过后钱已经付给供给者，此后再被拒付平台自吃，而这件事要等到
	// 第一笔拒付才看得见。所以默认值必须自己是安全的那一侧。
	//
	// 30 天是折中，不是"安全值"：卡组织规则允许持卡人在结算后 120 天内发起
	// 争议，真要覆盖全部得 2880 小时，但那意味着供给者干完活四个月才拿得到
	// 钱，没有人会来供货。实际拒付的绝大多数落在交易后一个月内，漏掉的那条
	// 尾巴不是无声的——它落在 `payment_disputes.uncovered_basis` 上。
	// **上线一个月后按那一列的真实累计值再调**，而不是继续拍脑袋。
	SupplierFreezeHoursDefault = 24 * 30
	// SupplierShareRatioDefault 默认分成比例（**占位值**，运营期标定）。
	SupplierShareRatioDefault = 0.70
)

// SupplierSettlementSettings 是双边市场的结算参数。
//
// 零值即「功能完全关闭」，这是刻意的：设置行不存在、JSON 损坏、数据库读失败，
// 全都回退到零值，计费路径与上游原逻辑一字不差。供给结算宁可不发生，
// 也不能因为读配置出错而按一个猜出来的比例给钱。
type SupplierSettlementSettings struct {
	// Enabled 总开关。关闭时不入账、不走钱包，供给账号退化为普通自营账号。
	Enabled bool `json:"enabled"`
	// ShareRatio 供给者分成比例，基数是消费者实付金额（不是官方价）。
	ShareRatio float64 `json:"share_ratio"`
	// FreezeHours 入账冻结小时数。0 = 不冻结（仅测试/特例）。
	FreezeHours int `json:"freeze_hours"`
	// SpendFromWalletFirst 为真时，消费者的赚取钱包余额优先于 users.balance 被扣。
	//
	// 与 Enabled 分开是有意的：可以先只开入账、让供给者攒着，
	// 等钱包侧观察稳了再打开消费出口。反过来（只开消费不开入账）无害但无意义。
	SpendFromWalletFirst bool `json:"spend_from_wallet_first"`
}

// DefaultSupplierSettlementSettings 返回「关闭」状态的默认配置。
//
// 默认关就是本功能的上线策略：代码先随版本进生产、在计费主链路上待着，
// 由管理员显式打开开关才开始动钱。比例/冻结窗给出占位值，是为了管理员打开开关时
// 不会因为忘填而按 0 分成。
func DefaultSupplierSettlementSettings() *SupplierSettlementSettings {
	return &SupplierSettlementSettings{
		Enabled:              false,
		ShareRatio:           SupplierShareRatioDefault,
		FreezeHours:          SupplierFreezeHoursDefault,
		SpendFromWalletFirst: false,
	}
}

// normalize 把越界/畸形值夹回合法区间。就地修改，返回自身方便链式调用。
func (s *SupplierSettlementSettings) normalize() *SupplierSettlementSettings {
	if s == nil {
		return DefaultSupplierSettlementSettings()
	}
	if math.IsNaN(s.ShareRatio) || math.IsInf(s.ShareRatio, 0) || s.ShareRatio < 0 {
		s.ShareRatio = 0
	}
	if s.ShareRatio > SupplierShareRatioMax {
		s.ShareRatio = SupplierShareRatioMax
	}
	if s.FreezeHours < 0 {
		s.FreezeHours = 0
	}
	if s.FreezeHours > SupplierFreezeHoursMax {
		s.FreezeHours = SupplierFreezeHoursMax
	}
	return s
}

// ToBillingParams 把配置翻译成计费命令携带的结算参数。
//
// 总开关关闭时返回全零值 —— 也就是 applyUsageBillingEffects 里
// 「什么都不做」的那一支。开关的语义收敛在这一个函数里，
// 计费侧不需要知道「开关」这个概念的存在。
func (s *SupplierSettlementSettings) ToBillingParams() UsageBillingSupplierParams {
	if s == nil || !s.Enabled {
		return UsageBillingSupplierParams{}
	}
	return UsageBillingSupplierParams{
		ShareRatio:           s.ShareRatio,
		FreezeHours:          s.FreezeHours,
		SpendFromWalletFirst: s.SpendFromWalletFirst,
	}
}

// ============================================================================
// 进程内缓存
//
// 结算参数在计费热路径上每笔请求读一次，直接查库会给 settings 表加上与网关等量的
// QPS。缓存形态照抄本包已有的 account_scheduling_thresholds / gateway_forwarding：
// atomic 快照 + singleflight 防击穿 + 独立超时的 DB context。
//
// 60s 陈旧窗是可接受的：改比例后最多一分钟生效，而这一分钟内的入账用的是
// 一个曾经真实存在过的配置值，流水里的「基数 × 比例 = 金额」依旧自洽。
// ============================================================================

type cachedSupplierSettlementSettings struct {
	settings  *SupplierSettlementSettings
	expiresAt int64 // unix nano
}

var supplierSettlementCache atomic.Value // *cachedSupplierSettlementSettings
var supplierSettlementSF singleflight.Group

const supplierSettlementCacheTTL = 60 * time.Second
const supplierSettlementErrorTTL = 5 * time.Second
const supplierSettlementDBTimeout = 5 * time.Second

// invalidateSupplierSettlementCache 让缓存立即失效。写路径调用，
// 使管理员改完配置马上能在下一笔请求上看到效果，而不必等 TTL 到期。
func invalidateSupplierSettlementCache() {
	supplierSettlementCache.Store(&cachedSupplierSettlementSettings{})
	supplierSettlementSF.Forget(SettingKeySupplierSettlement)
}

// GetSupplierSettlementSettings 读结算参数，永不返回错误。
//
// 任何异常（缺失、JSON 损坏、DB 故障）都回退到「关闭」的默认值：这是 fail-closed，
// 与本包其他 fail-open 的网关行为设置刻意相反。网关设置读不到时放行请求最多是
// 少一层加工；结算参数读不到时按猜测值给钱，错的是账。
func (s *SettingService) GetSupplierSettlementSettings(ctx context.Context) *SupplierSettlementSettings {
	if s == nil || s.settingRepo == nil {
		return DefaultSupplierSettlementSettings()
	}
	if cached, ok := supplierSettlementCache.Load().(*cachedSupplierSettlementSettings); ok {
		if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
			return cloneSupplierSettlementSettings(cached.settings)
		}
	}

	result, err, _ := supplierSettlementSF.Do(SettingKeySupplierSettlement, func() (any, error) {
		if cached, ok := supplierSettlementCache.Load().(*cachedSupplierSettlementSettings); ok {
			if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
				return cloneSupplierSettlementSettings(cached.settings), nil
			}
		}

		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), supplierSettlementDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeySupplierSettlement)
		if err != nil {
			settings := DefaultSupplierSettlementSettings()
			ttl := supplierSettlementErrorTTL
			if errors.Is(err, ErrSettingNotFound) {
				// 从未配置过是正常状态（功能没开），按正常 TTL 缓存，
				// 不要每 5 秒重查一次一个注定不存在的 key。
				ttl = supplierSettlementCacheTTL
			} else {
				slog.Warn("[Supplier] failed to read settlement settings, settlement stays disabled",
					"error", err, "key", SettingKeySupplierSettlement)
			}
			storeSupplierSettlementCache(settings, ttl)
			return cloneSupplierSettlementSettings(settings), nil
		}

		settings := parseSupplierSettlementSettings(raw)
		storeSupplierSettlementCache(settings, supplierSettlementCacheTTL)
		return cloneSupplierSettlementSettings(settings), nil
	})
	if err != nil {
		return DefaultSupplierSettlementSettings()
	}
	if settings, ok := result.(*SupplierSettlementSettings); ok && settings != nil {
		return settings
	}
	return DefaultSupplierSettlementSettings()
}

// SetSupplierSettlementSettings 写结算参数。
//
// 与读路径的 clamp 不同，写路径对「开着开关却配了不可能的值」直接报错：
// 管理员在面板上按下保存时，越界值是笔误，静默夹回去反而会让人以为存的是自己填的数。
func (s *SettingService) SetSupplierSettlementSettings(ctx context.Context, settings *SupplierSettlementSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	if settings.Enabled {
		if settings.ShareRatio <= 0 || settings.ShareRatio > SupplierShareRatioMax {
			return fmt.Errorf("share_ratio must be in (0, %g] when settlement is enabled", SupplierShareRatioMax)
		}
		if settings.FreezeHours < 0 || settings.FreezeHours > SupplierFreezeHoursMax {
			return fmt.Errorf("freeze_hours must be in [0, %d]", SupplierFreezeHoursMax)
		}
	}
	settings.normalize()

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal supplier settlement settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeySupplierSettlement, string(data)); err != nil {
		return fmt.Errorf("save supplier settlement settings: %w", err)
	}
	invalidateSupplierSettlementCache()
	return nil
}

// parseSupplierSettlementSettings 解析存储值。损坏时退回默认（关闭）并告警——
// 静默按默认值跑会让「开关明明是开的却不入账」变成一个查不出原因的现象。
func parseSupplierSettlementSettings(raw string) *SupplierSettlementSettings {
	settings := DefaultSupplierSettlementSettings()
	if raw == "" {
		return settings
	}
	var parsed SupplierSettlementSettings
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[Supplier] settlement settings JSON is corrupt, settlement stays disabled",
			"error", err, "key", SettingKeySupplierSettlement)
		return settings
	}
	return parsed.normalize()
}

func storeSupplierSettlementCache(settings *SupplierSettlementSettings, ttl time.Duration) {
	supplierSettlementCache.Store(&cachedSupplierSettlementSettings{
		settings:  cloneSupplierSettlementSettings(settings),
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

// cloneSupplierSettlementSettings 每次出手都给副本：缓存里那份是共享的，
// 调用方拿到指针后改一个字段就会污染所有后续请求的结算参数。
func cloneSupplierSettlementSettings(settings *SupplierSettlementSettings) *SupplierSettlementSettings {
	if settings == nil {
		return DefaultSupplierSettlementSettings()
	}
	clone := *settings
	return &clone
}
