// APEXONE-EXT: 双边市场——提现参数（开关、起提额、渠道、未决单上限、告知）。
//
// 第五个 settings key。分家的理由与前四个一样（见 setting_supply_pool.go 头部）。
// 这一组的独特之处是它决定**钱能不能离开系统**，因此校验的方向与协议那组一致：
// 写路径越界一律拒绝，读路径尽量保住能用的部分。数值型字段（起提额、未决单上限）
// 是唯二在读路径上做 clamp 的——一个被手工改成 -1 的起提额如果原样返回，
// 等于任何金额都能提。
//
// # 为什么「没配渠道」等于「不能提现」
//
// Channels 默认是空的，空列表在 SupplierWithdrawalService 里是硬拒绝
// （ErrSupplierWithdrawalNotConfigured）而不是「随便填」。收款渠道是运营真的要
// 拿着去打款的东西：让供给者自由输入，运营就会收到一堆自己根本没法打的渠道，
// 而供给者在等一笔永远不会到账的钱。做成下拉，是把「我们能打什么」这件事说清楚。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// SettingKeySupplyWithdrawal 提现参数的 settings key。
const SettingKeySupplyWithdrawal = "supply_withdrawal_settings"

const (
	// SupplyWithdrawalMinAmountFloor 起提额下限。0 = 不设门槛。
	SupplyWithdrawalMinAmountFloor = 0
	// SupplyWithdrawalMinAmountMax 起提额上限。
	//
	// 设上限是为了挡住「多打一个零」：起提额填成 10000 而不是 1000，界面上不会有
	// 任何异常，只是从此再没有人能提现，而且没人会来报这个 bug——他们只会以为
	// 提现坏了，然后不再挂号。
	SupplyWithdrawalMinAmountMax = 100000
	// SupplyWithdrawalMaxPendingCap 每人未决单数上限的上限。
	SupplyWithdrawalMaxPendingCap = 20
	// SupplyWithdrawalMaxPendingDefault 默认每人只能挂一张未决单。
	//
	// 一张就够：钱在申请时已经从可用区扣走，多提几张只是把同一笔余额切成几张单子，
	// 徒增运营的打款次数与手续费。
	SupplyWithdrawalMaxPendingDefault = 1
	// SupplyWithdrawalChannelsMax 最多配几个渠道。
	SupplyWithdrawalChannelsMax = 20
	// SupplyWithdrawalChannelMaxLen 单个渠道名的长度上限，与建表的 VARCHAR(64) 一致。
	SupplyWithdrawalChannelMaxLen = 64
	// SupplyWithdrawalNoticeMaxLen 告知文案长度上限。
	SupplyWithdrawalNoticeMaxLen = 2000
)

// SupplyWithdrawalSettings 是提现的全部可配内容。
type SupplyWithdrawalSettings struct {
	// Enabled 总开关。关闭时不接受新申请，但**已经挂着的单子照常能被处理**——
	// 关开关是停止收单，不是没收已经扣下来的钱。
	Enabled bool `json:"enabled"`
	// MinAmount 起提额。低于它的申请被拒绝（不是夹到它：那等于替供给者决定提多少）。
	MinAmount float64 `json:"min_amount"`
	// MaxPending 每人同时最多几张未决单。
	MaxPending int `json:"max_pending"`
	// Channels 可选的收款渠道。空 = 提现不可用。
	Channels []string `json:"channels"`
	// Notice 打款时效、手续费、工作日等告知，展示在申请表单上。按纯文本渲染。
	Notice string `json:"notice"`
}

// DefaultSupplyWithdrawalSettings 返回「关闭」状态的默认配置。
//
// 与结算总开关同一个上线策略：代码先进生产，由管理员显式打开。
func DefaultSupplyWithdrawalSettings() *SupplyWithdrawalSettings {
	return &SupplyWithdrawalSettings{
		Enabled:    false,
		MinAmount:  0,
		MaxPending: SupplyWithdrawalMaxPendingDefault,
		Channels:   []string{},
	}
}

// Available 提现此刻是否真的可用：开关开着，且至少配了一个收款渠道。
func (s *SupplyWithdrawalSettings) Available() bool {
	return s != nil && s.Enabled && len(s.Channels) > 0
}

// HasChannel 渠道是否在白名单里。比对前两边都 TrimSpace，但**区分大小写**——
// 渠道名是运营自己写的标签，"USDT" 与 "usdt" 在打款台账上就是两个词。
func (s *SupplyWithdrawalSettings) HasChannel(channel string) bool {
	if s == nil {
		return false
	}
	trimmed := strings.TrimSpace(channel)
	for _, known := range s.Channels {
		if known == trimmed {
			return true
		}
	}
	return false
}

// sanitizeWithdrawalChannels 清洗渠道列表：去空白、丢空串、丢超长、去重、截断数量。
//
// 去重是必要的而不是洁癖：列表会被原样渲染成下拉框，重复项让供给者以为两个
// 同名选项有什么区别。
func sanitizeWithdrawalChannels(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || len(trimmed) > SupplyWithdrawalChannelMaxLen {
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
		if len(out) >= SupplyWithdrawalChannelsMax {
			break
		}
	}
	return out
}

// sanitize 读路径的容错。数值 clamp、列表清洗、文案截断，都不报错。
func (s *SupplyWithdrawalSettings) sanitize() {
	if s == nil {
		return
	}
	if s.MinAmount < SupplyWithdrawalMinAmountFloor {
		s.MinAmount = SupplyWithdrawalMinAmountFloor
	}
	if s.MinAmount > SupplyWithdrawalMinAmountMax {
		s.MinAmount = SupplyWithdrawalMinAmountMax
	}
	if s.MaxPending <= 0 {
		s.MaxPending = SupplyWithdrawalMaxPendingDefault
	}
	if s.MaxPending > SupplyWithdrawalMaxPendingCap {
		s.MaxPending = SupplyWithdrawalMaxPendingCap
	}
	s.Channels = sanitizeWithdrawalChannels(s.Channels)
	s.Notice = strings.TrimSpace(s.Notice)
	if len(s.Notice) > SupplyWithdrawalNoticeMaxLen {
		s.Notice = s.Notice[:SupplyWithdrawalNoticeMaxLen]
	}
}

// validate 写路径的校验：越界拒绝保存，不夹回去。
//
// 与观察期那组（clamp 后保存）刻意相反：那组的每一个参数配错了只是探测慢一点，
// 这一组配错了是钱。管理员保存了一个 -1 的起提额，界面回一句「已保存」，
// 而库里躺的是 0——他会以为自己关掉了门槛，实际上从来没有。
func (s *SupplyWithdrawalSettings) validate() error {
	if s == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	if s.MinAmount < SupplyWithdrawalMinAmountFloor {
		return fmt.Errorf("minimum withdrawal amount cannot be negative")
	}
	if s.MinAmount > SupplyWithdrawalMinAmountMax {
		return fmt.Errorf("minimum withdrawal amount must be at most %d", SupplyWithdrawalMinAmountMax)
	}
	if s.MaxPending < 1 {
		return fmt.Errorf("max pending withdrawals must be at least 1")
	}
	if s.MaxPending > SupplyWithdrawalMaxPendingCap {
		return fmt.Errorf("max pending withdrawals must be at most %d", SupplyWithdrawalMaxPendingCap)
	}
	if len(s.Channels) > SupplyWithdrawalChannelsMax {
		return fmt.Errorf("at most %d payout channels are allowed", SupplyWithdrawalChannelsMax)
	}
	for _, item := range s.Channels {
		if len(strings.TrimSpace(item)) > SupplyWithdrawalChannelMaxLen {
			return fmt.Errorf("payout channel must be at most %d characters", SupplyWithdrawalChannelMaxLen)
		}
	}
	if len(s.Notice) > SupplyWithdrawalNoticeMaxLen {
		return fmt.Errorf("withdrawal notice must be at most %d characters", SupplyWithdrawalNoticeMaxLen)
	}
	// 清洗**在校验之后**，于是「填了三个空白渠道」不会被静默当成没填：
	// 上面几条越界该报的都报了，这里只做去空白/去重这类不改变意图的整理。
	s.Channels = sanitizeWithdrawalChannels(s.Channels)
	s.Notice = strings.TrimSpace(s.Notice)
	// 开着开关却一个渠道都没有，是一个只会在供给者点下申请时才暴露的错。
	if s.Enabled && len(s.Channels) == 0 {
		return fmt.Errorf("at least one payout channel is required before withdrawals can be enabled")
	}
	return nil
}

// ============================================================================
// 进程内缓存。形态与 setting_supply_agreement.go 一致。
// ============================================================================

type cachedSupplyWithdrawalSettings struct {
	settings  *SupplyWithdrawalSettings
	expiresAt int64 // unix nano
}

var supplyWithdrawalCache atomic.Value // *cachedSupplyWithdrawalSettings
var supplyWithdrawalSF singleflight.Group

const supplyWithdrawalCacheTTL = 60 * time.Second
const supplyWithdrawalErrorTTL = 5 * time.Second
const supplyWithdrawalDBTimeout = 5 * time.Second

func invalidateSupplyWithdrawalCache() {
	supplyWithdrawalCache.Store(&cachedSupplyWithdrawalSettings{})
	supplyWithdrawalSF.Forget(SettingKeySupplyWithdrawal)
}

// GetSupplyWithdrawalSettings 读提现配置，永不返回错误。
//
// 读不到一律回「关闭」：申请被拒是一次可以重试的失败，而按一个猜出来的配置
// 放行一笔提现是不可逆的。
func (s *SettingService) GetSupplyWithdrawalSettings(ctx context.Context) *SupplyWithdrawalSettings {
	if s == nil || s.settingRepo == nil {
		return DefaultSupplyWithdrawalSettings()
	}
	if cached, ok := supplyWithdrawalCache.Load().(*cachedSupplyWithdrawalSettings); ok {
		if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
			return cloneSupplyWithdrawalSettings(cached.settings)
		}
	}

	result, err, _ := supplyWithdrawalSF.Do(SettingKeySupplyWithdrawal, func() (any, error) {
		if cached, ok := supplyWithdrawalCache.Load().(*cachedSupplyWithdrawalSettings); ok {
			if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
				return cloneSupplyWithdrawalSettings(cached.settings), nil
			}
		}

		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), supplyWithdrawalDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeySupplyWithdrawal)
		if err != nil {
			settings := DefaultSupplyWithdrawalSettings()
			ttl := supplyWithdrawalErrorTTL
			if errors.Is(err, ErrSettingNotFound) {
				ttl = supplyWithdrawalCacheTTL
			} else {
				slog.Warn("[SupplyWithdrawal] failed to read withdrawal settings, withdrawals stay closed",
					"error", err, "key", SettingKeySupplyWithdrawal)
			}
			storeSupplyWithdrawalCache(settings, ttl)
			return cloneSupplyWithdrawalSettings(settings), nil
		}

		settings := parseSupplyWithdrawalSettings(raw)
		storeSupplyWithdrawalCache(settings, supplyWithdrawalCacheTTL)
		return cloneSupplyWithdrawalSettings(settings), nil
	})
	if err != nil {
		return DefaultSupplyWithdrawalSettings()
	}
	if settings, ok := result.(*SupplyWithdrawalSettings); ok && settings != nil {
		return settings
	}
	return DefaultSupplyWithdrawalSettings()
}

// SetSupplyWithdrawalSettings 写提现配置。越界一律拒绝。
func (s *SettingService) SetSupplyWithdrawalSettings(ctx context.Context, settings *SupplyWithdrawalSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if err := settings.validate(); err != nil {
		return err
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal supply withdrawal settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeySupplyWithdrawal, string(data)); err != nil {
		return fmt.Errorf("save supply withdrawal settings: %w", err)
	}
	invalidateSupplyWithdrawalCache()
	return nil
}

func parseSupplyWithdrawalSettings(raw string) *SupplyWithdrawalSettings {
	settings := DefaultSupplyWithdrawalSettings()
	if raw == "" {
		return settings
	}
	var parsed SupplyWithdrawalSettings
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[SupplyWithdrawal] withdrawal settings JSON is corrupt, withdrawals stay closed",
			"error", err, "key", SettingKeySupplyWithdrawal)
		return settings
	}
	parsed.sanitize()
	return &parsed
}

func storeSupplyWithdrawalCache(settings *SupplyWithdrawalSettings, ttl time.Duration) {
	supplyWithdrawalCache.Store(&cachedSupplyWithdrawalSettings{
		settings:  cloneSupplyWithdrawalSettings(settings),
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

// cloneSupplyWithdrawalSettings 深拷贝。Channels 是切片，浅拷贝会让缓存里那一份
// 与调用方拿到的那一份共享底层数组——调用方 append 一下就改了全进程的配置。
func cloneSupplyWithdrawalSettings(settings *SupplyWithdrawalSettings) *SupplyWithdrawalSettings {
	if settings == nil {
		return DefaultSupplyWithdrawalSettings()
	}
	clone := *settings
	clone.Channels = append([]string(nil), settings.Channels...)
	return &clone
}
