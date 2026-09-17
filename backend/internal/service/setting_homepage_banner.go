// APEXONE-EXT: 首页活动横幅的展示配置。
//
// 第九个 settings key。它存在的理由只有一条：**让一次活动的上下架不再需要发版**。
//
// 首页上所有文案都写死在前端的 i18n 分片里，改一个字要重新构建镜像、重新部署 CVM、
// 重新读回 composeHash、重新发一份 attestation reference。为了挂一条「活动开始了」
// 走完那一整套，代价与风险都不成比例——而活动是会反复办的。
//
// # 与 homepage_stats_settings 的分工
//
// 那一组是「首页展示什么**数据**」，这一组是「首页展示什么**话**」。两者变更的
// 理由不同：数据偏移动的是长期的对外形象，横幅动的是这一期活动的起止。合成一个
// key 会让「开一次活动」和「调一次展示基数」共用一条审计记录。
//
// # 为什么文案要中英各存一份
//
// 站点是双语的，而横幅是营销文案——机器翻译或只出一种语言，会在另一种语言的访客
// 那里变成一块突兀的外语补丁。存两份的代价是管理端多两个输入框，收益是两种语言的
// 访客看到的都是写给他们的话。只填一种时，另一种语言的访客看到已填的那份，
// 而不是一块空白（见 TextFor）。
//
// # 读路径 fail-closed，但方向与供给那几组不同
//
// 供给那几组 fail-closed 是为了「读不到就不要发钱」。这里是为了「读不到就不要在
// 首页挂一块坏掉的横幅」——一个渲染成空条、或者 CTA 指向空链接的横幅，比没有横幅
// 更伤信任，而它出现在站点最显眼的位置上。
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

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"golang.org/x/sync/singleflight"
)

// SettingKeyHomepageBanner 首页活动横幅的 settings key。
const SettingKeyHomepageBanner = "homepage_banner_settings"

// 长度上限。超长一律夹回（与 URL、variant 的「直接拒绝」相反，理由见 validate）。
const (
	// HomepageBannerTextMaxLen 正文长度上限。
	//
	// 横幅是一行字，不是一段文案。200 字符已经是两行半，再长会把首页顶部挤成一堵墙，
	// 而真正要说的东西应该在 CTA 后面那一页。
	HomepageBannerTextMaxLen = 200
	// HomepageBannerCTATextMaxLen 按钮文字长度上限。
	HomepageBannerCTATextMaxLen = 40
	// HomepageBannerURLMaxLen CTA 链接长度上限。
	HomepageBannerURLMaxLen = 512
)

// 横幅样式。刻意只有两种：一种是活动（醒目），一种是通知（克制）。
//
// 不做成自由色值：运营能调颜色的那天，首页就会出现一块与整站配色无关的东西，
// 而那不是配置问题，是设计问题。
const (
	// HomepageBannerVariantPromo 活动横幅，醒目配色。
	HomepageBannerVariantPromo = "promo"
	// HomepageBannerVariantInfo 普通通知，克制配色。
	HomepageBannerVariantInfo = "info"
)

var (
	// ErrHomepageBannerEmptyText 打开了横幅却没有任何文案。
	//
	// 必须拒绝：这会在首页最显眼的位置渲染一个空条。夹回成「关闭」也不行——
	// 管理员点了保存、看到开关是开的，却什么都没出现，他会以为是前端坏了。
	ErrHomepageBannerEmptyText = errors.New("banner text cannot be empty when enabled")
	// ErrHomepageBannerInvalidVariant variant 不是已知样式。
	ErrHomepageBannerInvalidVariant = fmt.Errorf("banner variant must be %q or %q",
		HomepageBannerVariantPromo, HomepageBannerVariantInfo)
	// ErrHomepageBannerInvalidURL CTA 链接不是合法的 http/https。
	//
	// 拒绝而不是清空：一个有按钮但点不动的横幅，比没有按钮更像故障。
	ErrHomepageBannerInvalidURL = errors.New("banner cta_url must be a valid http(s) URL")
	// ErrHomepageBannerCTAWithoutURL 配了按钮文字却没有链接。
	ErrHomepageBannerCTAWithoutURL = errors.New("banner cta_text requires cta_url")
)

// HomepageBannerSettings 是首页横幅的全部可配内容。
type HomepageBannerSettings struct {
	// Enabled 总开关。默认 false——不打开首页不出现这一块。
	Enabled bool `json:"enabled"`
	// TextZH / TextEN 正文，按访客语言取其一（见 TextFor）。
	TextZH string `json:"text_zh"`
	TextEN string `json:"text_en"`
	// CTATextZH / CTATextEN 按钮文字。留空则只显示文案、不显示按钮。
	CTATextZH string `json:"cta_text_zh"`
	CTATextEN string `json:"cta_text_en"`
	// CTAURLZH / CTAURLEN 按钮跳转地址，按语言各指各的。只接受 http/https。
	//
	// 一开始这里是**一个**共用的 URL，注释里写着「两种语言指向不同落地页是有道理的
	// 需求，但现在还没有」。2026-09-17 就有了：文档站开了双语，中文访客该落到
	// /zh-cn/... 而不是英文页。拆开的代价是两处校验、两条失效路径，收益是双语站上
	// 「点了按钮落到看不懂的语言」这件事不再发生。
	//
	// 回退规则与文案一致：缺的那一种回退到另一种，而不是渲染一个没有链接的按钮。
	CTAURLZH string `json:"cta_url_zh"`
	CTAURLEN string `json:"cta_url_en"`
	// Variant 样式，promo / info。空视为 info。
	Variant string `json:"variant"`
}

// DefaultHomepageBannerSettings 返回「首页不显示横幅」的默认配置。
func DefaultHomepageBannerSettings() *HomepageBannerSettings {
	return &HomepageBannerSettings{Enabled: false, Variant: HomepageBannerVariantInfo}
}

// Active 横幅是否真的会显示。
func (s *HomepageBannerSettings) Active() bool {
	if s == nil || !s.Enabled {
		return false
	}
	return strings.TrimSpace(s.TextZH) != "" || strings.TrimSpace(s.TextEN) != ""
}

// TextFor 按语言取正文。缺的那一种回退到另一种，而不是返回空串。
//
// 回退是刻意的：一次只填了英文的活动，中文访客看到英文横幅是可接受的；
// 看到一块空白不是。空白会被当成页面坏了。
func (s *HomepageBannerSettings) TextFor(lang string) string {
	if s == nil {
		return ""
	}
	zh, en := strings.TrimSpace(s.TextZH), strings.TrimSpace(s.TextEN)
	if isChineseLang(lang) {
		if zh != "" {
			return zh
		}
		return en
	}
	if en != "" {
		return en
	}
	return zh
}

// CTATextFor 按语言取按钮文字，回退规则同 TextFor。
func (s *HomepageBannerSettings) CTATextFor(lang string) string {
	if s == nil {
		return ""
	}
	zh, en := strings.TrimSpace(s.CTATextZH), strings.TrimSpace(s.CTATextEN)
	if isChineseLang(lang) {
		if zh != "" {
			return zh
		}
		return en
	}
	if en != "" {
		return en
	}
	return zh
}

// CTAURLFor 按语言取按钮链接，回退规则同 TextFor。
//
// 有回退才敢让运营只填一个 URL：活动页只做了英文时，中文访客点过去看英文页，
// 好过点了一个没反应的按钮。
func (s *HomepageBannerSettings) CTAURLFor(lang string) string {
	if s == nil {
		return ""
	}
	zh, en := strings.TrimSpace(s.CTAURLZH), strings.TrimSpace(s.CTAURLEN)
	if isChineseLang(lang) {
		if zh != "" {
			return zh
		}
		return en
	}
	if en != "" {
		return en
	}
	return zh
}

// isChineseLang 只认前缀。Accept-Language 会带上 zh-CN / zh-Hant / zh-TW 等等，
// 精确匹配会让除了 "zh" 之外的所有中文访客都走到英文分支。
func isChineseLang(lang string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh")
}

// validate 写路径校验。
//
// 结构性错误（空文案、未知样式、坏链接）**拒绝**，长度超限**夹回**。这个分界线
// 是按「运营会不会察觉」划的：文案被截断他一眼看得见，而一个被悄悄清空的 CTA 链接
// 他看不见——他只会以为自己配好了，然后横幅在首页上挂着一个点不动的按钮。
func (s *HomepageBannerSettings) validate() error {
	if s == nil {
		return errors.New("settings cannot be nil")
	}

	s.TextZH = clampRunes(strings.TrimSpace(s.TextZH), HomepageBannerTextMaxLen)
	s.TextEN = clampRunes(strings.TrimSpace(s.TextEN), HomepageBannerTextMaxLen)
	s.CTATextZH = clampRunes(strings.TrimSpace(s.CTATextZH), HomepageBannerCTATextMaxLen)
	s.CTATextEN = clampRunes(strings.TrimSpace(s.CTATextEN), HomepageBannerCTATextMaxLen)
	s.CTAURLZH = strings.TrimSpace(s.CTAURLZH)
	s.CTAURLEN = strings.TrimSpace(s.CTAURLEN)
	s.Variant = strings.ToLower(strings.TrimSpace(s.Variant))

	if s.Variant == "" {
		s.Variant = HomepageBannerVariantInfo
	}
	if s.Variant != HomepageBannerVariantPromo && s.Variant != HomepageBannerVariantInfo {
		return fmt.Errorf("%w: got %q", ErrHomepageBannerInvalidVariant, s.Variant)
	}

	// 只在打开时才要求有文案：关着的横幅允许留着上一期的空壳，
	// 否则「先关掉、下次再改文案」这条很自然的操作会被拦住。
	if s.Enabled && s.TextZH == "" && s.TextEN == "" {
		return ErrHomepageBannerEmptyText
	}

	// allowInsecureHTTP=true：有些活动会指向合作方还没上 HTTPS 的页面。
	// 真正要挡的是 javascript: / data: 这类协议头，那才是首页上的一个洞。
	for _, u := range []*string{&s.CTAURLZH, &s.CTAURLEN} {
		if *u == "" {
			continue
		}
		if len(*u) > HomepageBannerURLMaxLen {
			return fmt.Errorf("%w: longer than %d", ErrHomepageBannerInvalidURL, HomepageBannerURLMaxLen)
		}
		normalized, err := urlvalidator.ValidateURLFormat(*u, true)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrHomepageBannerInvalidURL, err)
		}
		*u = normalized
	}

	// 按钮文字要求**至少有一个**链接，而不是要求同语言的那个：只配了英文链接时，
	// 中文按钮会回退到英文 URL（见 CTAURLFor），那是可用的，不该被拦下。
	hasCTAText := s.CTATextZH != "" || s.CTATextEN != ""
	if hasCTAText && s.CTAURLZH == "" && s.CTAURLEN == "" {
		return ErrHomepageBannerCTAWithoutURL
	}

	return nil
}

// normalize 读路径夹回：把一份手工改坏的 JSON 收拾成「可以照着渲染」的样子。
//
// 与 validate 的分工同其他几组——这里**丢弃**不合法的部分而不是报错。具体到每一条：
// 坏掉的 CTA 链接连同按钮文字一起清掉（留着文字没有链接会渲染成一个死按钮），
// 未知样式回落到 info，打开但没文案直接视为关闭。
func (s *HomepageBannerSettings) normalize() {
	if s == nil {
		return
	}
	s.TextZH = clampRunes(strings.TrimSpace(s.TextZH), HomepageBannerTextMaxLen)
	s.TextEN = clampRunes(strings.TrimSpace(s.TextEN), HomepageBannerTextMaxLen)
	s.CTATextZH = clampRunes(strings.TrimSpace(s.CTATextZH), HomepageBannerCTATextMaxLen)
	s.CTATextEN = clampRunes(strings.TrimSpace(s.CTATextEN), HomepageBannerCTATextMaxLen)
	s.CTAURLZH = strings.TrimSpace(s.CTAURLZH)
	s.CTAURLEN = strings.TrimSpace(s.CTAURLEN)
	s.Variant = strings.ToLower(strings.TrimSpace(s.Variant))

	if s.Variant != HomepageBannerVariantPromo {
		s.Variant = HomepageBannerVariantInfo
	}
	for _, u := range []*string{&s.CTAURLZH, &s.CTAURLEN} {
		if *u == "" {
			continue
		}
		normalized, err := urlvalidator.ValidateURLFormat(*u, true)
		if err != nil || len(*u) > HomepageBannerURLMaxLen {
			slog.Warn("[HomepageBanner] dropping unusable cta url", "error", err)
			*u = ""
		} else {
			*u = normalized
		}
	}
	if s.CTAURLZH == "" && s.CTAURLEN == "" {
		// 没有链接就不该留下按钮文字——否则前端要么渲染死按钮，要么各自判空。
		s.CTATextZH, s.CTATextEN = "", ""
	}
	if s.Enabled && s.TextZH == "" && s.TextEN == "" {
		slog.Warn("[HomepageBanner] banner is enabled but has no text, treating as disabled")
		s.Enabled = false
	}
}

// clampRunes 按**字符**而不是字节截断。
//
// 按字节截断会把一个多字节汉字劈成两半，产出一个非法 UTF-8 串——它会一路穿过
// JSON 编码到浏览器，在首页上显示成一个替换字符。
func clampRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// ============================================================================
// 进程内缓存。形态与 setting_homepage_stats.go 一致。
// ============================================================================

type cachedHomepageBannerSettings struct {
	settings  *HomepageBannerSettings
	expiresAt int64 // unix nano
}

var homepageBannerCache atomic.Value // *cachedHomepageBannerSettings
var homepageBannerSF singleflight.Group

// homepageBannerCacheTTL 取 30 秒而不是其他几组的 60 秒：这是运营手动开关的东西，
// 点了保存之后会立刻去首页看效果。等一分钟他会以为没生效，然后再点一次。
const homepageBannerCacheTTL = 30 * time.Second
const homepageBannerErrorTTL = 5 * time.Second
const homepageBannerDBTimeout = 5 * time.Second

func invalidateHomepageBannerCache() {
	homepageBannerCache.Store(&cachedHomepageBannerSettings{})
	homepageBannerSF.Forget(SettingKeyHomepageBanner)
}

// GetHomepageBannerSettings 读横幅配置，永不返回错误。读不到 = 不显示。
func (s *SettingService) GetHomepageBannerSettings(ctx context.Context) *HomepageBannerSettings {
	if s == nil || s.settingRepo == nil {
		return DefaultHomepageBannerSettings()
	}
	if cached, ok := homepageBannerCache.Load().(*cachedHomepageBannerSettings); ok {
		if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
			return cloneHomepageBannerSettings(cached.settings)
		}
	}

	result, err, _ := homepageBannerSF.Do(SettingKeyHomepageBanner, func() (any, error) {
		if cached, ok := homepageBannerCache.Load().(*cachedHomepageBannerSettings); ok {
			if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
				return cloneHomepageBannerSettings(cached.settings), nil
			}
		}

		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), homepageBannerDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyHomepageBanner)
		if err != nil {
			settings := DefaultHomepageBannerSettings()
			ttl := homepageBannerErrorTTL
			if errors.Is(err, ErrSettingNotFound) {
				ttl = homepageBannerCacheTTL
			} else {
				slog.Warn("[HomepageBanner] failed to read banner settings, banner stays hidden",
					"error", err, "key", SettingKeyHomepageBanner)
			}
			storeHomepageBannerCache(settings, ttl)
			return cloneHomepageBannerSettings(settings), nil
		}

		settings := parseHomepageBannerSettings(raw)
		storeHomepageBannerCache(settings, homepageBannerCacheTTL)
		return cloneHomepageBannerSettings(settings), nil
	})
	if err != nil {
		return DefaultHomepageBannerSettings()
	}
	if settings, ok := result.(*HomepageBannerSettings); ok && settings != nil {
		return settings
	}
	return DefaultHomepageBannerSettings()
}

// SetHomepageBannerSettings 写横幅配置。结构性错误直接拒绝，理由见 validate。
func (s *SettingService) SetHomepageBannerSettings(ctx context.Context, settings *HomepageBannerSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	if err := settings.validate(); err != nil {
		return err
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal homepage banner settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyHomepageBanner, string(data)); err != nil {
		return fmt.Errorf("save homepage banner settings: %w", err)
	}
	invalidateHomepageBannerCache()
	return nil
}

func parseHomepageBannerSettings(raw string) *HomepageBannerSettings {
	settings := DefaultHomepageBannerSettings()
	if raw == "" {
		return settings
	}
	var parsed HomepageBannerSettings
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[HomepageBanner] banner settings JSON is corrupt, banner stays hidden",
			"error", err, "key", SettingKeyHomepageBanner)
		return settings
	}
	parsed.normalize()
	return &parsed
}

func storeHomepageBannerCache(settings *HomepageBannerSettings, ttl time.Duration) {
	homepageBannerCache.Store(&cachedHomepageBannerSettings{
		settings:  cloneHomepageBannerSettings(settings),
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

func cloneHomepageBannerSettings(settings *HomepageBannerSettings) *HomepageBannerSettings {
	if settings == nil {
		return DefaultHomepageBannerSettings()
	}
	clone := *settings
	return &clone
}
