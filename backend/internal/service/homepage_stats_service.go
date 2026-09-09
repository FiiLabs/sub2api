// APEXONE-EXT: 首页公开数据的装配（真实数 + 可配基数偏移）。
//
// 供一个**无鉴权**的公开端点用，招徕供给方与使用方。展示值 = 真实聚合数 + 运营配的
// 偏移（见 setting_homepage_stats.go）。总开关关时返回 Enabled=false，前端隐藏这一段。
//
// # fail-soft：任一真实读数取不到就按 0 计，仍叠加偏移
//
// 这是一个营销展示端点，不是账务接口——某个聚合查询抖一下不该让整段消失或报错。
// 读不到就把那一项的真实分量当 0（仍加偏移），页面照常展示；比整段 500 或整段空白
// 都好。
//
// # 60 秒缓存
//
// dashboard 读数与设置各自已有缓存，但供给号计数与累计入账是两条直连 DB 的聚合。
// 这是公开端点，会被匿名流量反复打，套一层 60 秒缓存把这两条查询摊薄。
package service

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// PublicHomepageStats 首页公开数据的对外读数（已叠加偏移）。
type PublicHomepageStats struct {
	// Enabled 总开关。false 时其余字段无意义，前端应隐藏这一段。
	Enabled bool `json:"enabled"`
	// SharedAccounts 展示用共享号数 = 真实活跃共享号数 + 偏移。
	SharedAccounts int64 `json:"shared_accounts"`
	// ActiveUsers 展示用活跃用户数。
	ActiveUsers int64 `json:"active_users"`
	// TotalRequests 展示用累计请求数。
	TotalRequests int64 `json:"total_requests"`
	// ContributorEarningsUSDT 展示用已付贡献者收益（USDT）。
	ContributorEarningsUSDT float64 `json:"contributor_earnings_usdt"`
	// SupplyByPlatform 按平台的**真实**可调度供给号数（环形图占比用）。
	// 不叠偏移——环形图展示的是占比，真实即可；首页数据带忽略此字段。
	SupplyByPlatform map[string]int64 `json:"supply_by_platform,omitempty"`
}

// homepageStatsSettingsReader 读展示配置（偏移 + 开关）。由 *SettingService 实现。
type homepageStatsSettingsReader interface {
	GetHomepageStatsSettings(ctx context.Context) *HomepageStatsSettings
}

// homepageEarningsReader 读累计贡献者入账。由接入仓储实现（SumContributorEarnings）。
type homepageEarningsReader interface {
	SumContributorEarnings(ctx context.Context) (float64, error)
}

// HomepageStatsService 装配首页公开数据。
type HomepageStatsService struct {
	settings homepageStatsSettingsReader
	demand   supplyDemandStatsReader // 复用 supply_demand_balance.go 的接口
	supply   supplyAccountCounter    // 复用
	earnings homepageEarningsReader

	cache atomic.Value // *cachedPublicHomepageStats
}

type cachedPublicHomepageStats struct {
	stats     *PublicHomepageStats
	expiresAt int64
}

const homepageStatsResultTTL = 60 * time.Second

// NewHomepageStatsService 构造装配服务。任一依赖 nil 时对应真实分量按 0 计（fail-soft）。
func NewHomepageStatsService(
	settings homepageStatsSettingsReader,
	demand supplyDemandStatsReader,
	supply supplyAccountCounter,
	earnings homepageEarningsReader,
) *HomepageStatsService {
	return &HomepageStatsService{settings: settings, demand: demand, supply: supply, earnings: earnings}
}

// GetPublicStats 返回首页公开数据（含缓存）。总开关关时返回 Enabled=false。
func (h *HomepageStatsService) GetPublicStats(ctx context.Context) *PublicHomepageStats {
	if h == nil {
		return &PublicHomepageStats{Enabled: false}
	}
	if cached, ok := h.cache.Load().(*cachedPublicHomepageStats); ok {
		if cached != nil && cached.stats != nil && time.Now().UnixNano() < cached.expiresAt {
			clone := *cached.stats
			return &clone
		}
	}

	cfg := DefaultHomepageStatsSettings()
	if h.settings != nil {
		cfg = h.settings.GetHomepageStatsSettings(ctx)
	}
	if cfg == nil || !cfg.Enabled {
		stats := &PublicHomepageStats{Enabled: false}
		h.store(stats)
		return stats
	}

	supplyByPlatform := h.realSupplyByPlatform(ctx)
	var supplyTotal int64
	for _, c := range supplyByPlatform {
		supplyTotal += c
	}
	stats := &PublicHomepageStats{
		Enabled:                 true,
		SharedAccounts:          supplyTotal + cfg.SharedAccountsOffset,
		ActiveUsers:             cfg.ActiveUsersOffset,
		TotalRequests:           cfg.TotalRequestsOffset,
		ContributorEarningsUSDT: h.realEarnings(ctx) + cfg.ContributorEarningsOffset,
		SupplyByPlatform:        supplyByPlatform,
	}
	if au, tr, ok := h.realDemand(ctx); ok {
		stats.ActiveUsers += au
		stats.TotalRequests += tr
	}
	h.store(stats)
	return stats
}

func (h *HomepageStatsService) store(stats *PublicHomepageStats) {
	clone := *stats
	h.cache.Store(&cachedPublicHomepageStats{stats: &clone, expiresAt: time.Now().Add(homepageStatsResultTTL).UnixNano()})
}

// realSupplyByPlatform 真实按平台可调度供给号数；读不到返回 nil（环形图隐藏 + 标量只剩偏移）。
func (h *HomepageStatsService) realSupplyByPlatform(ctx context.Context) map[string]int64 {
	if h.supply == nil {
		return nil
	}
	counts, err := h.supply.CountActiveSupplyAccounts(ctx)
	if err != nil {
		slog.Warn("[HomepageStats] failed to count supply accounts, showing offset only", "error", err)
		return nil
	}
	if len(counts) == 0 {
		return nil
	}
	out := make(map[string]int64, len(counts))
	for platform, c := range counts {
		out[platform] = int64(c)
	}
	return out
}

// realDemand 真实活跃用户数与累计请求数；读不到返回 ok=false。
func (h *HomepageStatsService) realDemand(ctx context.Context) (activeUsers, totalRequests int64, ok bool) {
	if h.demand == nil {
		return 0, 0, false
	}
	stats, err := h.demand.GetDashboardStats(ctx)
	if err != nil || stats == nil {
		if err != nil {
			slog.Warn("[HomepageStats] failed to read dashboard stats, showing offset only", "error", err)
		}
		return 0, 0, false
	}
	return stats.ActiveUsers, stats.TotalRequests, true
}

// realEarnings 真实累计贡献者入账；读不到按 0。
func (h *HomepageStatsService) realEarnings(ctx context.Context) float64 {
	if h.earnings == nil {
		return 0
	}
	total, err := h.earnings.SumContributorEarnings(ctx)
	if err != nil {
		slog.Warn("[HomepageStats] failed to sum contributor earnings, showing offset only", "error", err)
		return 0
	}
	return total
}
