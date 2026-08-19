// APEXONE-EXT: 双边市场——结算参数与供给池路由的管理端读写。
//
// 挂在**已有的** `*SettingHandler` 上而不是新起一个 handler 类型：那样要动
// `AdminHandlers` 结构体、`ProvideAdminHandlers` 的形参表、admin ProviderSet 和
// wire_gen——四处上游合并热区，换来的只是一个新名字。方法挂在既有类型上，
// 本文件是纯新增，wire 层改动为零，路由层只多一行。
//
// 两组配置分成两对端点，与 setting_supply_pool.go 里「刻意分成两个 key」同一个理由：
// 改分成比例和改兜底池是两件不同的事，共用一个端点会让审计日志分不清谁改了什么。
package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// SupplierSettlementSettingsResponse 是结算参数的对外形态。
//
// 单独定义而不是直接回 service 结构体：service 那个类型在计费热路径上被读，
// 给它加 json tag 之外的展示字段（比如下面的 *_max）会让热路径带上只有面板需要的东西。
type SupplierSettlementSettingsResponse struct {
	Enabled              bool    `json:"enabled"`
	ShareRatio           float64 `json:"share_ratio"`
	FreezeHours          int     `json:"freeze_hours"`
	SpendFromWalletFirst bool    `json:"spend_from_wallet_first"`

	// 边界值随配置一起下发，前端不必把这两个数抄一遍。抄一遍的下场是后端改了上限、
	// 前端还在按旧值拦，用户看到的是一个前端说不行、后端其实允许的值。
	ShareRatioMax  float64 `json:"share_ratio_max"`
	FreezeHoursMax int     `json:"freeze_hours_max"`
}

func newSupplierSettlementSettingsResponse(s *service.SupplierSettlementSettings) SupplierSettlementSettingsResponse {
	resp := SupplierSettlementSettingsResponse{
		ShareRatioMax:  service.SupplierShareRatioMax,
		FreezeHoursMax: service.SupplierFreezeHoursMax,
	}
	if s == nil {
		return resp
	}
	resp.Enabled = s.Enabled
	resp.ShareRatio = s.ShareRatio
	resp.FreezeHours = s.FreezeHours
	resp.SpendFromWalletFirst = s.SpendFromWalletFirst
	return resp
}

// GetSupplierSettlementSettings 读结算参数
// GET /api/v1/admin/settings/supplier-settlement
//
// service 侧这个 getter 永不返回错误（fail-closed，读不到就是「关闭」），
// 所以这里没有错误分支。面板上看到的「关闭」因此有两种成因——真的没配过，
// 或者读配置出错了——后者在服务端有 warn 日志。
func (h *SettingHandler) GetSupplierSettlementSettings(c *gin.Context) {
	settings := h.settingService.GetSupplierSettlementSettings(c.Request.Context())
	response.Success(c, newSupplierSettlementSettingsResponse(settings))
}

// UpdateSupplierSettlementSettingsRequest 更新结算参数请求。
//
// 全字段必传（不是 PATCH 语义）：这三个数必须一起看——比例调高而冻结窗不动
// 等于放大拒付敞口。部分更新会让运营在只想改一个数时，另两个数悄悄沿用了
// 他没看见的旧值。
type UpdateSupplierSettlementSettingsRequest struct {
	Enabled              bool    `json:"enabled"`
	ShareRatio           float64 `json:"share_ratio"`
	FreezeHours          int     `json:"freeze_hours"`
	SpendFromWalletFirst bool    `json:"spend_from_wallet_first"`
}

// UpdateSupplierSettlementSettings 写结算参数
// PUT /api/v1/admin/settings/supplier-settlement
//
// 不在这里重复 service 侧的区间校验：那边写路径已经对「开着开关却配了不可能的值」
// 直接报错（而不是像读路径那样 clamp）。在 handler 再抄一份区间，等于给同一条规则
// 立两个源头，改上限时必然漏掉一个。
func (h *SettingHandler) UpdateSupplierSettlementSettings(c *gin.Context) {
	var req UpdateSupplierSettlementSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	settings := &service.SupplierSettlementSettings{
		Enabled:              req.Enabled,
		ShareRatio:           req.ShareRatio,
		FreezeHours:          req.FreezeHours,
		SpendFromWalletFirst: req.SpendFromWalletFirst,
	}
	if err := h.settingService.SetSupplierSettlementSettings(c.Request.Context(), settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// 回读而不是回显请求体：写路径会 normalize（夹回区间），回显请求体会让面板显示
	// 一个数据库里并不存在的值。
	response.Success(c, newSupplierSettlementSettingsResponse(
		h.settingService.GetSupplierSettlementSettings(c.Request.Context())))
}

// SupplyPoolSettingsResponse 是供给池路由配置的对外形态。
type SupplyPoolSettingsResponse struct {
	Enabled         bool  `json:"enabled"`
	SupplyGroupID   int64 `json:"supply_group_id"`
	OverflowGroupID int64 `json:"overflow_group_id"`
}

func newSupplyPoolSettingsResponse(s *service.SupplyPoolSettings) SupplyPoolSettingsResponse {
	if s == nil {
		return SupplyPoolSettingsResponse{}
	}
	return SupplyPoolSettingsResponse{
		Enabled:         s.Enabled,
		SupplyGroupID:   s.SupplyGroupID,
		OverflowGroupID: s.OverflowGroupID,
	}
}

// GetSupplyPoolSettings 读供给池路由配置
// GET /api/v1/admin/settings/supply-pool
func (h *SettingHandler) GetSupplyPoolSettings(c *gin.Context) {
	settings := h.settingService.GetSupplyPoolSettings(c.Request.Context())
	response.Success(c, newSupplyPoolSettingsResponse(settings))
}

// UpdateSupplyPoolSettingsRequest 更新供给池路由配置请求。
type UpdateSupplyPoolSettingsRequest struct {
	Enabled         bool  `json:"enabled"`
	SupplyGroupID   int64 `json:"supply_group_id"`
	OverflowGroupID int64 `json:"overflow_group_id"`
}

// UpdateSupplyPoolSettings 写供给池路由配置
// PUT /api/v1/admin/settings/supply-pool
//
// 这里同样不校验分组是否存在，理由见 setting_supply_pool.go：分组可能在配置之后
// 被删掉，配置侧的存在性校验给不出「以后也有效」的保证，真正的兜底在调度侧。
func (h *SettingHandler) UpdateSupplyPoolSettings(c *gin.Context) {
	var req UpdateSupplyPoolSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	settings := &service.SupplyPoolSettings{
		Enabled:         req.Enabled,
		SupplyGroupID:   req.SupplyGroupID,
		OverflowGroupID: req.OverflowGroupID,
	}
	if err := h.settingService.SetSupplyPoolSettings(c.Request.Context(), settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	response.Success(c, newSupplyPoolSettingsResponse(
		h.settingService.GetSupplyPoolSettings(c.Request.Context())))
}
