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
	// SupplyWithdrawalNotifyEmailsMax 提现通知最多发给几个运营邮箱。
	//
	// 设上限不是怕发信贵，是怕这个字段变成一份全公司通讯录：收件人一多，
	// 每个人都会假设"另一个人会处理"，于是没有人处理。
	SupplyWithdrawalNotifyEmailsMax = 10
	// SupplyWithdrawalNotifyEmailMaxLen 单个邮箱长度上限（RFC 5321 的 254）。
	SupplyWithdrawalNotifyEmailMaxLen = 254
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
	// NotifyEmails 打款异常时通知谁。空 = 不通知任何人。
	//
	// M6b（链上-only）后它唯一的消费者是 NotifyPayoutFailed：worker 把打不动的
	// 单子停靠到 failed、钱扣着等人工裁决——没人收到这封信，那笔钱就卡在一张
	// 没人知道的单子上。「新申请」的运营邮件已停发（自动结算下每单一封只会
	// 被过滤）。**这个字段为空仍是坏状态**，管理端照旧画出来。
	//
	// 与账号配额告警的收件人（SettingKeyAccountQuotaNotifyEmails）刻意分开：
	// 收打款异常的是财务，收配额告警的是运维，合成一份的下场是两边都开始过滤邮件。
	NotifyEmails []string `json:"notify_emails"`
}

// DefaultSupplyWithdrawalSettings 返回「关闭」状态的默认配置。
//
// 与结算总开关同一个上线策略：代码先进生产，由管理员显式打开。
func DefaultSupplyWithdrawalSettings() *SupplyWithdrawalSettings {
	return &SupplyWithdrawalSettings{
		Enabled:      false,
		MinAmount:    0,
		MaxPending:   SupplyWithdrawalMaxPendingDefault,
		Channels:     []string{},
		NotifyEmails: []string{},
	}
}

// Available 提现开关是否开着（M6b 起它只答这一半，见方法内注释）。
func (s *SupplyWithdrawalSettings) Available() bool {
	// M6b 起渠道由金库能力派生，这里看不到金库——「真能提吗」由
	// SupplierWithdrawalService.GetOptions 用 settleableChannels 回答；
	// 这个方法只剩「开关开没开」这一半，供管理端设置卡显示。
	return s != nil && s.Enabled
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

// sanitizeWithdrawalNotifyEmails 清洗运营收件人：去空白、丢空串、丢超长、
// 丢明显不是邮箱的、按小写去重、截断数量。
//
// 格式只查一件事：有且仅有一个 `@`，且两侧都非空。这个门槛刻意定得低——
// 严格的邮箱正则会挡掉合法的地址（带 + 号的、非 ASCII 域名的），而这里的收件人
// 是管理员自己填的，真正要防的是「把手机号填进来了」这种一眼可见的错。
func sanitizeWithdrawalNotifyEmails(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || len(trimmed) > SupplyWithdrawalNotifyEmailMaxLen || !looksLikeEmail(trimmed) {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, dup := seen[lower]; dup {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, trimmed)
		if len(out) >= SupplyWithdrawalNotifyEmailsMax {
			break
		}
	}
	return out
}

// looksLikeEmail 见 sanitizeWithdrawalNotifyEmails 的注释：故意宽松。
func looksLikeEmail(s string) bool {
	at := strings.IndexByte(s, '@')
	if at <= 0 || at != strings.LastIndexByte(s, '@') || at == len(s)-1 {
		return false
	}
	return !strings.ContainsAny(s, " \t\r\n")
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
	s.NotifyEmails = sanitizeWithdrawalNotifyEmails(s.NotifyEmails)
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
	if len(s.NotifyEmails) > SupplyWithdrawalNotifyEmailsMax {
		return fmt.Errorf("at most %d notification emails are allowed", SupplyWithdrawalNotifyEmailsMax)
	}
	// 收件人格式**报错而不是静默丢弃**：悄悄丢掉一个填错的邮箱，管理员会看到
	// 「已保存」然后一直等一封永远不会来的信。渠道那边可以静默清洗，是因为
	// 渠道少一个供给者立刻就看得见；少一个收件人没有任何可见症状。
	for _, item := range s.NotifyEmails {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > SupplyWithdrawalNotifyEmailMaxLen {
			return fmt.Errorf("notification email must be at most %d characters", SupplyWithdrawalNotifyEmailMaxLen)
		}
		if !looksLikeEmail(trimmed) {
			return fmt.Errorf("%q is not a valid notification email", trimmed)
		}
	}
	// 清洗**在校验之后**，于是「填了三个空白渠道」不会被静默当成没填：
	// 上面几条越界该报的都报了，这里只做去空白/去重这类不改变意图的整理。
	s.Channels = sanitizeWithdrawalChannels(s.Channels)
	s.NotifyEmails = sanitizeWithdrawalNotifyEmails(s.NotifyEmails)
	s.Notice = strings.TrimSpace(s.Notice)
	// M6b 起不再要求渠道：渠道由链上金库的能力派生（settleableChannels），
	// 这份白名单只是还躺在旧 JSON 里的遗留字段。「开着但金库没配好」的坏状态
	// 改由金库那张卡的 status 说话，这里拦不到也不该拦。
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
	clone.NotifyEmails = append([]string(nil), settings.NotifyEmails...)
	return &clone
}
