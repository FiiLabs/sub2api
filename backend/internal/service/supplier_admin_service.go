// APEXONE-EXT: 双边市场——管理端运营视图服务。
//
// 只做参数夹取与白名单校验，聚合全在 SQL 里。刻意不缓存：这几个数字是运营用来
// 决定「要不要打钱、要不要停一个号」的依据，宁可每次多一次查询，也不要给出一个
// 不知道过期多久的答案。真到了扛不住的量，该做的是物化视图，而不是在这里放一个
// 谁也说不清 TTL 的缓存。
package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	supplyAdminDefaultPageSize = 20
	supplyAdminMaxPageSize     = 100
	// 流水窗口的默认与上限。上限压在一年：再长的区间该走导出，不该走一个同步接口。
	supplyAdminDefaultWindowDays = 30
	supplyAdminMaxWindowDays     = 365
)

// ErrSupplyAdminInvalidSort 排序键不在白名单里。
//
// 刻意报错而不是静默回落到默认排序：前端传了个后端不认识的键，多半是两边的
// 常量已经漂移了，静默回落会让「排序按钮点了没反应」这种问题一直没人发现。
var ErrSupplyAdminInvalidSort = infraerrors.BadRequest(
	"SUPPLY_ADMIN_INVALID_SORT", "unsupported sort key")

// SupplierAdminService 给管理端看板提供只读聚合。
type SupplierAdminService struct {
	repo SupplierAdminRepository
}

// NewSupplierAdminService 构造运营视图服务。
func NewSupplierAdminService(repo SupplierAdminRepository) *SupplierAdminService {
	return &SupplierAdminService{repo: repo}
}

// unavailable 是「服务没装配起来」的统一回答。
//
// 这几个接口全部只读，所以缺依赖时回 503 而不是回一个空结果：一个空看板和
// 一个「没有任何供给者」的看板在界面上长得一模一样，而两者要做的事完全相反。
func (s *SupplierAdminService) unavailable() error {
	return infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "supply market admin service unavailable")
}

func (s *SupplierAdminService) ready() bool {
	return s != nil && s.repo != nil
}

// clampSupplyAdminPage 把分页参数夹回合法区间。上下限只有这一份。
func clampSupplyAdminPage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = supplyAdminDefaultPageSize
	}
	if pageSize > supplyAdminMaxPageSize {
		pageSize = supplyAdminMaxPageSize
	}
	return page, pageSize
}

// Overview 读看板汇总。windowDays <= 0 用默认值。
func (s *SupplierAdminService) Overview(ctx context.Context, windowDays int) (*SupplyMarketOverview, error) {
	if !s.ready() {
		return nil, s.unavailable()
	}
	if windowDays <= 0 {
		windowDays = supplyAdminDefaultWindowDays
	}
	if windowDays > supplyAdminMaxWindowDays {
		windowDays = supplyAdminMaxWindowDays
	}
	return s.repo.Overview(ctx, windowDays)
}

// ListSuppliers 分页读名册。
func (s *SupplierAdminService) ListSuppliers(ctx context.Context, filter SupplierRosterFilter) ([]SupplierRosterEntry, int64, error) {
	if !s.ready() {
		return nil, 0, s.unavailable()
	}
	if filter.Sort == "" {
		filter.Sort = SupplierRosterSortOwed
	}
	if !isKnownRosterSort(filter.Sort) {
		return nil, 0, ErrSupplyAdminInvalidSort
	}
	filter.Page, filter.PageSize = clampSupplyAdminPage(filter.Page, filter.PageSize)
	return s.repo.ListSuppliers(ctx, filter)
}

// isKnownRosterSort 白名单校验。遍历那一个数组，而不是再写一遍 switch——
// 多一份取值清单就多一处会漏改的地方。
func isKnownRosterSort(sort SupplierRosterSort) bool {
	for _, known := range SupplierRosterSorts {
		if sort == known {
			return true
		}
	}
	return false
}

// ListAccounts 分页读供给账号明细。
func (s *SupplierAdminService) ListAccounts(ctx context.Context, filter SupplyAccountAdminFilter) ([]SupplyAccountAdminView, int64, error) {
	if !s.ready() {
		return nil, 0, s.unavailable()
	}
	// Health 是个封闭集合，但传错时不报错——它和 State 一样只是个筛子，
	// 一个不认识的值筛出空集是合理的回答。非白名单值在仓储层被当成「不筛」，
	// 所以这里显式归一，免得靠仓储的默认分支来兜。
	if filter.Health != SupplyAccountHealthHealthy && filter.Health != SupplyAccountHealthUnhealthy {
		filter.Health = SupplyAccountHealthAny
	}
	if filter.OwnerUserID < 0 {
		filter.OwnerUserID = 0
	}
	filter.Page, filter.PageSize = clampSupplyAdminPage(filter.Page, filter.PageSize)
	return s.repo.ListAccounts(ctx, filter)
}

// ListLedger 分页读全站流水。
//
// 与供给者侧那个同名方法的唯一差别是 UserID 可以为 0（看全站）——这个接口
// 只从管理路由组进来，鉴权由中间件保证。
func (s *SupplierAdminService) ListLedger(ctx context.Context, filter SupplyAdminLedgerFilter) ([]SupplyAdminLedgerEntry, int64, error) {
	if !s.ready() {
		return nil, 0, s.unavailable()
	}
	if filter.UserID < 0 {
		filter.UserID = 0
	}
	if filter.AccountID < 0 {
		filter.AccountID = 0
	}
	filter.Page, filter.PageSize = clampSupplyAdminPage(filter.Page, filter.PageSize)
	return s.repo.ListLedger(ctx, filter)
}
