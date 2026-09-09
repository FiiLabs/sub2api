package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// PublicStatsHandler 首页公开数据（无鉴权）——共享号数 / 活跃用户 / 累计请求 /
// 已付贡献者收益，用于招徕供给方与使用方。数据为真实聚合 + 运营配的基数偏移，
// 见 service.HomepageStatsService 与 setting_homepage_stats.go。
type PublicStatsHandler struct {
	statsService *service.HomepageStatsService
}

// NewPublicStatsHandler 创建公开数据 handler。
func NewPublicStatsHandler(statsService *service.HomepageStatsService) *PublicStatsHandler {
	return &PublicStatsHandler{statsService: statsService}
}

// GetPublicStats 返回首页公开数据。
// GET /api/v1/stats/public
//
// 总开关关时返回 Enabled=false（前端隐藏这一段）。不返回错误——这是营销展示端点，
// 任一真实读数抖动都在 service 层被 fail-soft 吸收。
func (h *PublicStatsHandler) GetPublicStats(c *gin.Context) {
	if h.statsService == nil {
		response.Success(c, &service.PublicHomepageStats{Enabled: false})
		return
	}
	response.Success(c, h.statsService.GetPublicStats(c.Request.Context()))
}
