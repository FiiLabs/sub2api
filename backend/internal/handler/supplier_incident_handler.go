// APEXONE-EXT: 双边市场——供给号失效事件的 HTTP 层。
//
// 两条只读接口，长在 SupplierAdminHandler 上（理由见那个文件顶部：躲开
// AdminHandlers / ProvideAdminHandlers / handler/wire.go 三处上游合并热区）。
//
// # 为什么明细和报表是两条接口而不是一条
//
// 它们回答的是两个不同的问题，被打开的频率也差一个量级：
//
//   - 报表（summary）回答「供给侧健康吗、谁在反复坏」。它是一屏聚合，
//     每天被看一两次，参数只有窗口。
//   - 明细（list）回答「这个人/这个号具体发生了什么」。它是分页的、带一串筛选，
//     从报表里点进来。
//
// 合成一条的话，那次聚合会跟着每一次翻页重算一遍——而聚合扫的是全表。
package handler

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ListIncidents 读失效事件明细。
// GET /api/v1/admin/supply/incidents?user_id=&account_id=&open=&start_at=&end_at=&page=&page_size=
//
// open=true 是运营巡检的默认视图：「现在还有哪些号坏着」。
func (h *SupplierAdminHandler) ListIncidents(c *gin.Context) {
	page, pageSize := supplyAdminPaging(c)
	userID, _ := strconv.ParseInt(c.DefaultQuery("user_id", "0"), 10, 64)
	accountID, _ := strconv.ParseInt(c.DefaultQuery("account_id", "0"), 10, 64)

	incidents, total, err := h.incidentService.List(c.Request.Context(), service.SupplierIncidentFilter{
		UserID:    userID,
		AccountID: accountID,
		OpenOnly:  parseSupplyBoolQuery(c.Query("open")),
		StartAt:   parseSupplyAdminTimeQuery(c.Query("start_at")),
		EndAt:     parseSupplyAdminTimeQuery(c.Query("end_at")),
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if incidents == nil {
		incidents = []service.SupplierAccountIncident{}
	}
	response.Paginated(c, incidents, total, page, pageSize)
}

// GetIncidentSummary 读封禁率报表。
// GET /api/v1/admin/supply/incidents/summary?window_days=30&top=10
func (h *SupplierAdminHandler) GetIncidentSummary(c *gin.Context) {
	windowDays, _ := strconv.Atoi(c.DefaultQuery("window_days", "0"))
	topN, _ := strconv.Atoi(c.DefaultQuery("top", "0"))

	summary, err := h.incidentService.Summary(c.Request.Context(), windowDays, topN)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, summary)
}

// parseSupplyBoolQuery 解析一个「开关型」查询参数。
//
// 认 true / 1 / yes / on，其余一律当 false——**包括畸形值**。与
// parseSupplyAdminTimeQuery 同一条理由的反面：那里放宽筛选是安全的（多显示几行），
// 这里放宽（把认不出的值当成 true）会把一个全量列表悄悄变成只剩未结的那几行，
// 而运营会以为历史事件都不见了。认不出就是没筛。
func parseSupplyBoolQuery(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}
