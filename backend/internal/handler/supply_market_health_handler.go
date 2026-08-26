// APEXONE-EXT: 双边市场——定价健康度读数的管理端出口。
//
// 只读、只有一个端点。它与同组其它看板接口的唯一差别是**没有分页**：
// 这份读数是一整块（几个汇总数字 + 一张最多 200 行的榜），拆页会让
// 「中位数」「兜底占比」这些跨行的派生量算不出来。
package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetSupplyMarketHealth 读定价与供给健康度。
// GET /api/v1/admin/supply/health?window_days=30
//
// window_days 解析失败时传 0 交给 service 夹成默认值，不回 400：
// 这个参数来自界面上的一个窗口切换器，一个手敲坏了的查询串要的是
// 「给我看默认那档」，不是一页错误。
func (h *SupplierAdminHandler) GetSupplyMarketHealth(c *gin.Context) {
	if h == nil || h.healthService == nil {
		response.Error(c, 503, "supply market health service is not wired")
		return
	}
	windowDays, _ := strconv.Atoi(c.DefaultQuery("window_days", "0"))

	health, err := h.healthService.Get(c.Request.Context(), windowDays)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, health)
}
