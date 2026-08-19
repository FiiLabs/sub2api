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

// SupplierAdminHandler 处理管理端的供给侧只读查询。
type SupplierAdminHandler struct {
	adminService *service.SupplierAdminService
}

// NewSupplierAdminHandler 构造运营视图 handler。
func NewSupplierAdminHandler(adminService *service.SupplierAdminService) *SupplierAdminHandler {
	return &SupplierAdminHandler{adminService: adminService}
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
