// APEXONE-EXT: 首页公开数据的展示配置（真实数 + 可配基数偏移）。
//
// 第八个 settings key。首页要展示「共享号数 / 活跃用户 / 累计请求 / 已付贡献者收益」
// 来吸引供给方与使用方，但平台早期这些真实数字都很小、不好看。这组配置让运营给
// 每一项配一个**基数偏移**：展示值 = 真实数 + 偏移。既看着有规模，又会随真实活动
// 增长——比纯造一个静止的假数字诚实（那种数字过一阵就会露馅：永远不动）。
//
// # 默认关、零偏移
//
// `Enabled` 默认 false：不打开就不在首页展示这一段。所有偏移默认 0：即便打开，
// 不配偏移就只展示真实数。这两条让「上了代码但没配」= 现网无变化。
//
// # 偏移是对外展示的美化，运营自负其责
//
// 这组数字会公开展示以招徕用户，偏移把它抬高本质上是营销包装。已与需求方确认走
// 「真实数 + 偏移」而不是纯造假；偏移由运营在面板上掌握、可随时归零。代码只负责
// 「真实数 + 偏移」这个透明的加法，不替运营决定该抬多少。
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

// SettingKeyHomepageStats 首页公开数据展示配置的 settings key。
const SettingKeyHomepageStats = "homepage_stats_settings"

// HomepageStatsSettings 首页公开数据的展示配置。
type HomepageStatsSettings struct {
	// Enabled 总开关。默认 false——不打开首页不展示这一段。
	Enabled bool `json:"enabled"`

	// SharedAccountsOffset 共享号数的基数偏移（展示值 = 真实活跃共享号数 + 此值）。
	SharedAccountsOffset int64 `json:"shared_accounts_offset"`
	// ActiveUsersOffset 活跃用户数的基数偏移。
	ActiveUsersOffset int64 `json:"active_users_offset"`
	// TotalRequestsOffset 累计请求数的基数偏移。
	TotalRequestsOffset int64 `json:"total_requests_offset"`
	// TotalTokensOffset 累计处理 tokens 数的基数偏移。
	TotalTokensOffset int64 `json:"total_tokens_offset"`
	// ContributorEarningsOffset 已付贡献者收益（USDT）的基数偏移。float 以承载金额小数。
	ContributorEarningsOffset float64 `json:"contributor_earnings_offset"`
}

// DefaultHomepageStatsSettings 返回默认配置：关、零偏移。
func DefaultHomepageStatsSettings() *HomepageStatsSettings {
	return &HomepageStatsSettings{}
}

// normalize 负偏移一律夹成 0：偏移的语义是「往上抬」，负数最可能是手滑，
// 且一个把真实数字往下压的偏移会让展示值可能变成负数——一个负的「活跃用户数」。
func (s *HomepageStatsSettings) normalize() {
	if s == nil {
		return
	}
	if s.SharedAccountsOffset < 0 {
		s.SharedAccountsOffset = 0
	}
	if s.ActiveUsersOffset < 0 {
		s.ActiveUsersOffset = 0
	}
	if s.TotalRequestsOffset < 0 {
		s.TotalRequestsOffset = 0
	}
	if s.TotalTokensOffset < 0 {
		s.TotalTokensOffset = 0
	}
	if s.ContributorEarningsOffset < 0 {
		s.ContributorEarningsOffset = 0
	}
}

// ============================================================================
// 进程内缓存。形态与其它 settings 一致。
// ============================================================================

type cachedHomepageStatsSettings struct {
	settings  *HomepageStatsSettings
	expiresAt int64
}

var homepageStatsCache atomic.Value // *cachedHomepageStatsSettings
var homepageStatsSF singleflight.Group

const homepageStatsCacheTTL = 60 * time.Second
const homepageStatsErrorTTL = 5 * time.Second
const homepageStatsDBTimeout = 5 * time.Second

func invalidateHomepageStatsCache() {
	homepageStatsCache.Store(&cachedHomepageStatsSettings{})
	homepageStatsSF.Forget(SettingKeyHomepageStats)
}

// GetHomepageStatsSettings 读首页展示配置，永不返回错误（读失败回退默认=关）。
func (s *SettingService) GetHomepageStatsSettings(ctx context.Context) *HomepageStatsSettings {
	if s == nil || s.settingRepo == nil {
		return DefaultHomepageStatsSettings()
	}
	if cached, ok := homepageStatsCache.Load().(*cachedHomepageStatsSettings); ok {
		if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
			return cloneHomepageStatsSettings(cached.settings)
		}
	}

	result, err, _ := homepageStatsSF.Do(SettingKeyHomepageStats, func() (any, error) {
		if cached, ok := homepageStatsCache.Load().(*cachedHomepageStatsSettings); ok {
			if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
				return cloneHomepageStatsSettings(cached.settings), nil
			}
		}

		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), homepageStatsDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyHomepageStats)
		if err != nil {
			settings := DefaultHomepageStatsSettings()
			ttl := homepageStatsErrorTTL
			if errors.Is(err, ErrSettingNotFound) {
				ttl = homepageStatsCacheTTL
			} else {
				slog.Warn("[HomepageStats] failed to read settings, falling back to defaults",
					"error", err, "key", SettingKeyHomepageStats)
			}
			storeHomepageStatsCache(settings, ttl)
			return cloneHomepageStatsSettings(settings), nil
		}

		settings := parseHomepageStatsSettings(raw)
		storeHomepageStatsCache(settings, homepageStatsCacheTTL)
		return cloneHomepageStatsSettings(settings), nil
	})
	if err != nil {
		return DefaultHomepageStatsSettings()
	}
	if settings, ok := result.(*HomepageStatsSettings); ok && settings != nil {
		return settings
	}
	return DefaultHomepageStatsSettings()
}

// SetHomepageStatsSettings 写首页展示配置。负偏移夹成 0 后回读。
func (s *SettingService) SetHomepageStatsSettings(ctx context.Context, settings *HomepageStatsSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	settings.normalize()

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal homepage stats settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyHomepageStats, string(data)); err != nil {
		return fmt.Errorf("save homepage stats settings: %w", err)
	}
	invalidateHomepageStatsCache()
	return nil
}

func parseHomepageStatsSettings(raw string) *HomepageStatsSettings {
	settings := DefaultHomepageStatsSettings()
	if raw == "" {
		return settings
	}
	var parsed HomepageStatsSettings
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[HomepageStats] settings JSON is corrupt, falling back to defaults",
			"error", err, "key", SettingKeyHomepageStats)
		return settings
	}
	parsed.normalize()
	return &parsed
}

func storeHomepageStatsCache(settings *HomepageStatsSettings, ttl time.Duration) {
	homepageStatsCache.Store(&cachedHomepageStatsSettings{
		settings:  cloneHomepageStatsSettings(settings),
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

func cloneHomepageStatsSettings(settings *HomepageStatsSettings) *HomepageStatsSettings {
	if settings == nil {
		return DefaultHomepageStatsSettings()
	}
	clone := *settings
	return &clone
}
