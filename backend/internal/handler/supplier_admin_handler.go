// APEXONE-EXT: 双边市场——管理端运营视图的 HTTP 层。
//
// 为什么这个 handler 长在 SupplierHandler 上（h.Supplier.Admin.*）而不是进 AdminHandlers：
// `handler.go` 的 `AdminHandlers` 结构体、`ProvideAdminHandlers` 的形参表和
// `handler/wire.go` 的 ProviderSet 是三处上游合并热区，每加一个管理端 handler 就要
// 在这三处各留一行，每轮 sync 都得调解一次。挂在已经存在的 `Handlers.Supplier`
// 下面，这一整块运营视图在核心文件里的侵入是**零行**（路由那一行除外）——
// 与 §4 台账 #8 用「把方法挂到既有 SettingHandler 上」躲开同样三处的做法同源。
//
// 代价是一个类型上同时挂着用户侧和管理侧的入口，所以两者刻意是**两个类型**：
// 管理端方法一个都不在 SupplierHandler 上，路由写错时是编译期的事，而不是
// 把一个全站流水接口挂到了 /user/supply 下面。
//
// 鉴权全部由路由组的 adminAuth 中间件负责，这里一个字节都不查——这与本仓所有
// admin handler 的做法一致。真正要守住的是**别把这些方法注册到非 admin 组**。
package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// SupplierAdminHandler 处理管理端的供给侧运营视图。
//
// 提现审批（supplier_withdrawal_handler.go）是这个类型上**唯一**的写入口，
// 也是本文件顶部「全部只读」那条自律的唯一例外——一笔提现如果没有人能点"已打款"，
// 供给者的钱就永远停在一张单子上。例外的边界写在 routes/supplier.go 的
// registerSupplyMarketRoutes 上：写路由只有那三条，且都只改提现单的状态。
type SupplierAdminHandler struct {
	adminService      *service.SupplierAdminService
	withdrawalService *service.SupplierWithdrawalService
	// exportService 对账导出（supplier_export_handler.go）。它与上面两个的
	// 差别是**响应体是一份要离开这台机器的文件**：里面有收款账号明文，
	// 且一旦下载完成就不再受这套系统的任何约束。边界写在 §3.9。
	exportService *service.SupplierExportService
	// incidentService 供给号失效事件（supplier_incident_handler.go）。只读，
	// 与看板同类。边界写在 §3.10。
	incidentService *service.SupplierIncidentService
	// healthService 定价与供给健康度（supply_market_health_handler.go）。只读。
	//
	// 可为 nil：这是一块给人看的经营读数，它没装配好不该让同一个 handler 上
	// 其余那些真会动钱的接口（打款、拒绝）跟着起不来。端点自己回 503。
	healthService *service.SupplyMarketHealthService
}

// NewSupplierAdminHandler 构造运营视图 handler。
func NewSupplierAdminHandler(
	adminService *service.SupplierAdminService,
	withdrawalService *service.SupplierWithdrawalService,
	exportService *service.SupplierExportService,
	incidentService *service.SupplierIncidentService,
	healthService *service.SupplyMarketHealthService,
) *SupplierAdminHandler {
	return &SupplierAdminHandler{
		adminService:      adminService,
		withdrawalService: withdrawalService,
		exportService:     exportService,
		incidentService:   incidentService,
		healthService:     healthService,
	}
}

// supplyAdminPaging 解析分页参数。越界值交给 service 侧夹回去（那里有唯一的一份上下限）。
func supplyAdminPaging(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	return page, pageSize
}

// GetOverview 读看板汇总。
// GET /api/v1/admin/supply/overview?window_days=30
func (h *SupplierAdminHandler) GetOverview(c *gin.Context) {
	windowDays, _ := strconv.Atoi(c.DefaultQuery("window_days", "0"))
	overview, err := h.adminService.Overview(c.Request.Context(), windowDays)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, overview)
}

// ListSuppliers 读供给者名册。
// GET /api/v1/admin/supply/suppliers?page=&page_size=&keyword=&sort=
func (h *SupplierAdminHandler) ListSuppliers(c *gin.Context) {
	page, pageSize := supplyAdminPaging(c)
	entries, total, err := h.adminService.ListSuppliers(c.Request.Context(), service.SupplierRosterFilter{
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		Sort:     service.SupplierRosterSort(strings.TrimSpace(c.Query("sort"))),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if entries == nil {
		entries = []service.SupplierRosterEntry{}
	}
	response.Paginated(c, entries, total, page, pageSize)
}

// ListAccounts 读供给账号明细。
// GET /api/v1/admin/supply/accounts?state=&health=&owner_user_id=&page=&page_size=
//
// 「哪些卡在观察期」= state=pending_review；「谁的号在被封」= health=unhealthy。
func (h *SupplierAdminHandler) ListAccounts(c *gin.Context) {
	page, pageSize := supplyAdminPaging(c)
	ownerUserID, _ := strconv.ParseInt(c.DefaultQuery("owner_user_id", "0"), 10, 64)
	views, total, err := h.adminService.ListAccounts(c.Request.Context(), service.SupplyAccountAdminFilter{
		State:       strings.TrimSpace(c.Query("state")),
		Health:      service.SupplyAccountHealth(strings.TrimSpace(c.Query("health"))),
		OwnerUserID: ownerUserID,
		Page:        page,
		PageSize:    pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if views == nil {
		views = []service.SupplyAccountAdminView{}
	}
	response.Paginated(c, views, total, page, pageSize)
}

// ListLedger 读全站钱包流水。
// GET /api/v1/admin/supply/ledger?user_id=&action=&account_id=&request_id=&start_at=&end_at=&page=&page_size=
func (h *SupplierAdminHandler) ListLedger(c *gin.Context) {
	page, pageSize := supplyAdminPaging(c)
	userID, _ := strconv.ParseInt(c.DefaultQuery("user_id", "0"), 10, 64)
	accountID, _ := strconv.ParseInt(c.DefaultQuery("account_id", "0"), 10, 64)

	entries, total, err := h.adminService.ListLedger(c.Request.Context(), service.SupplyAdminLedgerFilter{
		UserID:    userID,
		Action:    strings.TrimSpace(c.Query("action")),
		AccountID: accountID,
		RequestID: strings.TrimSpace(c.Query("request_id")),
		StartAt:   parseSupplyAdminTimeQuery(c.Query("start_at")),
		EndAt:     parseSupplyAdminTimeQuery(c.Query("end_at")),
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if entries == nil {
		entries = []service.SupplyAdminLedgerEntry{}
	}
	response.Paginated(c, entries, total, page, pageSize)
}

// parseSupplyAdminTimeQuery 解析 RFC3339 时间参数，解析不了当没传。
//
// 不报 400 是刻意的：一个畸形的时间参数把整个检索拒掉，运营会以为「这段时间
// 没有流水」。范围放宽（当没筛）会多显示一些行，那是看得见、能自己纠正的。
func parseSupplyAdminTimeQuery(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return &parsed
}
