// APEXONE-EXT: 双边市场——供给者协议（版本、正文、链接）。
//
// 第四个 settings key。分家的理由与前三个一样（见 setting_supply_pool.go 头部），
// 而这一组的变更原因是最独特的一个：**它一变，所有人就得重新点一次同意**。
// 把它塞进结算参数里，运营改一个分成比例的小数点就会顺手把全站的同意记录作废掉。
//
// # 为什么「没配协议」等于「不能接入」
//
// Version 默认是空的，而空版本在 SupplierOnboardingService 里是一条硬拒绝
// （ErrSupplierAgreementNotConfigured），不是「跳过同意」。理由：这一步的全部意义
// 就是让平台在收下一个陌生人的订阅凭证之前，先把双方的权利义务写下来并留下证据。
// 「没配置就放行」会让这套机制在最需要它的那个部署上（运营还没顾上写协议的那个）
// 正好不生效——那和没做是一样的，还多了一份"我们有协议流程"的错觉。
//
// 代价是开源部署第一次打开供给池时会撞上这个错误。这是刻意的：自助接入本来就
// 默认关着（supply_pool_settings.enabled），运营打开它的那一刻正是他该决定
// 「我拿什么条款收别人的订阅」的时刻。
//
// # 校验的方向：写路径拒绝，读路径丢弃
//
// 同一份非法数据在两条路径上的处理刻意相反。写路径（管理员在表单里填）越界一律
// 报错，因为这是法律文本——静默把正文截断到十万字、或者把版本号截短，比报错糟得多。
// 读路径（库里已经有一份手工改坏的数据）则只丢掉出问题的那个字段：一个
// `javascript:` 的 URL 会被清空，但正文和版本号照常返回。整份协议因为一个坏链接
// 而消失，会连带把所有人挡在接入之外。
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

// SettingKeySupplyAgreement 供给者协议的 settings key。
const SettingKeySupplyAgreement = "supply_agreement_settings"

// 各字段长度上限。
const (
	// SupplyAgreementVersionMaxLen 版本号长度上限。
	//
	// 它会作为字符串存进每一条同意记录（supplier_agreement_acceptances.version），
	// 是那张表的查询键之一，所以要短。够写下 `2026-08-19` 或 `v1.3-zh` 就行。
	SupplyAgreementVersionMaxLen = 64
	// SupplyAgreementURLMaxLen 协议链接长度上限。
	SupplyAgreementURLMaxLen = 512
	// SupplyAgreementBodyMaxLen 正文长度上限。
	//
	// 十万字符大约是一份很长的服务条款。设上限不是怕存不下（settings 值是 TEXT），
	// 是怕有人把整个网页粘进来之后，这份配置在每一次接入页加载时都被读一遍。
	SupplyAgreementBodyMaxLen = 100000
)

// SupplyAgreementSettings 是供给者协议的全部可配内容。
type SupplyAgreementSettings struct {
	// Version 协议版本号。空 = **尚未发布协议**，自助接入会被拒绝。
	//
	// 同意记录按 (user_id, version) 存，所以改动这个值 = 要求所有人重新同意。
	// 改错别字不该动它；只有条款实质变化时才改。
	Version string `json:"version"`
	// URL 协议全文的外部链接，可空。
	//
	// 与 Body 是「或」的关系而不是二选一：有自己官网的部署填链接，没有的填正文，
	// 两个都填就是「页面上能读到，也能点出去看排版好的那一份」。
	URL string `json:"url"`
	// Body 协议正文，可空。**按纯文本渲染**——前端不跑 markdown，也不插 HTML，
	// 一份从别处粘来的条款里的 `<script>` 不该因为它是"协议"就获得执行权。
	Body string `json:"body"`
}

// DefaultSupplyAgreementSettings 返回「尚未发布协议」的默认配置。
func DefaultSupplyAgreementSettings() *SupplyAgreementSettings {
	return &SupplyAgreementSettings{}
}

// Published 协议是否已经发布（有版本号）。没有版本号的协议不能拿去要求别人同意。
func (s *SupplyAgreementSettings) Published() bool {
	return s != nil && strings.TrimSpace(s.Version) != ""
}

// sanitize 读路径的容错：只丢掉出问题的字段，不动其余的。
//
// 与 validate（写路径）刻意不同，理由见文件头。
func (s *SupplyAgreementSettings) sanitize() {
	if s == nil {
		return
	}
	s.Version = strings.TrimSpace(s.Version)
	if len(s.Version) > SupplyAgreementVersionMaxLen {
		// 版本号是同意记录的键。截断会让新的同意记录挂在一个截短的版本上，
		// 而 gate 比对的是同一个截短值——能自洽，但库里那份配置和记录对不上。
		// 宁可当作没发布：接入被拒，运营会立刻发现。
		s.Version = ""
	}
	s.URL = strings.TrimSpace(s.URL)
	if !isSafeAgreementURL(s.URL) {
		s.URL = ""
	}
	if len(s.Body) > SupplyAgreementBodyMaxLen {
		s.Body = s.Body[:SupplyAgreementBodyMaxLen]
	}
}

// validate 写路径的校验：任何一处越界都拒绝保存。
func (s *SupplyAgreementSettings) validate() error {
	if s == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	s.Version = strings.TrimSpace(s.Version)
	s.URL = strings.TrimSpace(s.URL)
	if len(s.Version) > SupplyAgreementVersionMaxLen {
		return fmt.Errorf("agreement version must be at most %d characters", SupplyAgreementVersionMaxLen)
	}
	if len(s.URL) > SupplyAgreementURLMaxLen {
		return fmt.Errorf("agreement url must be at most %d characters", SupplyAgreementURLMaxLen)
	}
	if !isSafeAgreementURL(s.URL) {
		return fmt.Errorf("agreement url must start with http:// or https://")
	}
	if len(s.Body) > SupplyAgreementBodyMaxLen {
		return fmt.Errorf("agreement body must be at most %d characters", SupplyAgreementBodyMaxLen)
	}
	// 发布了版本号，却既没有正文也没有链接，等于让人对着一个标题点同意。
	// 这一条拦在写路径上，因为那时还有人能改；拦在读路径上只会把接入停掉。
	if s.Version != "" && s.Body == "" && s.URL == "" {
		return fmt.Errorf("agreement must have a body or a url before it can be published")
	}
	return nil
}

// isSafeAgreementURL 只放行 http/https 的绝对链接（空串也放行 = 没填）。
//
// 这个函数存在的唯一理由是前端会把它渲染成一个 <a href>。`javascript:` 与 `data:`
// 在那个位置是可执行的，而这份配置的写入者是管理员——所以这不是防外部攻击，
// 是防「管理员账号被拿下之后，协议链接成了一个挂在每个供给者面前的执行点」。
func isSafeAgreementURL(raw string) bool {
	if raw == "" {
		return true
	}
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// ============================================================================
// 进程内缓存。形态与 setting_supply_probation.go 一致。
// ============================================================================

type cachedSupplyAgreementSettings struct {
	settings  *SupplyAgreementSettings
	expiresAt int64 // unix nano
}

var supplyAgreementCache atomic.Value // *cachedSupplyAgreementSettings
var supplyAgreementSF singleflight.Group

const supplyAgreementCacheTTL = 60 * time.Second
const supplyAgreementErrorTTL = 5 * time.Second
const supplyAgreementDBTimeout = 5 * time.Second

func invalidateSupplyAgreementCache() {
	supplyAgreementCache.Store(&cachedSupplyAgreementSettings{})
	supplyAgreementSF.Forget(SettingKeySupplyAgreement)
}

// GetSupplyAgreementSettings 读协议配置，永不返回错误。
//
// 读不到（库挂了、键不存在、JSON 坏了）一律回「尚未发布」，于是接入被拒。
// 与其余三个 key 的 fail-closed 同向，但这里的后果更直接：读一次失败就有人挂不上号。
// 仍然选这个方向——放行的那一边是「在没有协议的情况下收下了别人的凭证」。
func (s *SettingService) GetSupplyAgreementSettings(ctx context.Context) *SupplyAgreementSettings {
	if s == nil || s.settingRepo == nil {
		return DefaultSupplyAgreementSettings()
	}
	if cached, ok := supplyAgreementCache.Load().(*cachedSupplyAgreementSettings); ok {
		if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
			return cloneSupplyAgreementSettings(cached.settings)
		}
	}

	result, err, _ := supplyAgreementSF.Do(SettingKeySupplyAgreement, func() (any, error) {
		if cached, ok := supplyAgreementCache.Load().(*cachedSupplyAgreementSettings); ok {
			if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
				return cloneSupplyAgreementSettings(cached.settings), nil
			}
		}

		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), supplyAgreementDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeySupplyAgreement)
		if err != nil {
			settings := DefaultSupplyAgreementSettings()
			ttl := supplyAgreementErrorTTL
			if errors.Is(err, ErrSettingNotFound) {
				ttl = supplyAgreementCacheTTL
			} else {
				slog.Warn("[SupplyAgreement] failed to read agreement settings, onboarding stays closed",
					"error", err, "key", SettingKeySupplyAgreement)
			}
			storeSupplyAgreementCache(settings, ttl)
			return cloneSupplyAgreementSettings(settings), nil
		}

		settings := parseSupplyAgreementSettings(raw)
		storeSupplyAgreementCache(settings, supplyAgreementCacheTTL)
		return cloneSupplyAgreementSettings(settings), nil
	})
	if err != nil {
		return DefaultSupplyAgreementSettings()
	}
	if settings, ok := result.(*SupplyAgreementSettings); ok && settings != nil {
		return settings
	}
	return DefaultSupplyAgreementSettings()
}

// SetSupplyAgreementSettings 写协议配置。越界一律拒绝，理由见文件头。
func (s *SettingService) SetSupplyAgreementSettings(ctx context.Context, settings *SupplyAgreementSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if err := settings.validate(); err != nil {
		return err
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal supply agreement settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeySupplyAgreement, string(data)); err != nil {
		return fmt.Errorf("save supply agreement settings: %w", err)
	}
	invalidateSupplyAgreementCache()
	return nil
}

func parseSupplyAgreementSettings(raw string) *SupplyAgreementSettings {
	settings := DefaultSupplyAgreementSettings()
	if raw == "" {
		return settings
	}
	var parsed SupplyAgreementSettings
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[SupplyAgreement] agreement settings JSON is corrupt, onboarding stays closed",
			"error", err, "key", SettingKeySupplyAgreement)
		return settings
	}
	parsed.sanitize()
	return &parsed
}

func storeSupplyAgreementCache(settings *SupplyAgreementSettings, ttl time.Duration) {
	supplyAgreementCache.Store(&cachedSupplyAgreementSettings{
		settings:  cloneSupplyAgreementSettings(settings),
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

func cloneSupplyAgreementSettings(settings *SupplyAgreementSettings) *SupplyAgreementSettings {
	if settings == nil {
		return DefaultSupplyAgreementSettings()
	}
	clone := *settings
	return &clone
}
