// APEXONE-EXT: 双边市场——供给者赚取钱包的读侧服务。
//
// 只做三件事：读余额、读流水、在读之前顺手解冻。写侧（accrue/spend）不在这里——
// 那两个动作只发生在计费事务内部（internal/repository/usage_billing_supplier.go），
// 走的是 tx，不经过本服务。这样「钱的写入口」全仓只有一个，审计时不必满仓找。
package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	supplierLedgerDefaultPageSize = 20
	supplierLedgerMaxPageSize     = 100
)

// SupplierCreditService 给供给者仪表盘提供只读视图。
type SupplierCreditService struct {
	repo           SupplierCreditRepository
	settingService *SettingService
}

func NewSupplierCreditService(repo SupplierCreditRepository, settingService *SettingService) *SupplierCreditService {
	return &SupplierCreditService{repo: repo, settingService: settingService}
}

// IsEnabled 供给结算是否开启。前端据此决定要不要显示「收益」入口。
func (s *SupplierCreditService) IsEnabled(ctx context.Context) bool {
	if s == nil || s.settingService == nil {
		return false
	}
	return s.settingService.GetSupplierSettlementSettings(ctx).Enabled
}

// GetWallet 读钱包快照。
//
// 读之前先解冻一次（照抄 affiliate 的懒解冻）：供给者点开页面时看到的
// 「可用/冻结」就是此刻的真实划分，不必等后台扫描那一轮。解冻失败是
// best-effort——它只影响两个数字之间的划分，不影响总额，没有理由让读失败。
//
// 真正兜底的是 SupplierThawService 的周期扫描：只用 API、从不开页面的供给者
// 走不到这里。两者都幂等，同时命中也不会多搬一分钱。
func (s *SupplierCreditService) GetWallet(ctx context.Context, userID int64) (*SupplierCreditSummary, error) {
	if s == nil || s.repo == nil {
		return nil, infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "supplier credit service unavailable")
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER", "invalid user")
	}
	_, _ = s.repo.ThawMatured(ctx, userID)
	// EnsureWallet 而非 GetWallet：从未入过账的供给者也该看到一个全零的钱包，
	// 而不是一个 404。
	return s.repo.EnsureWallet(ctx, userID)
}

// ListLedger 分页读流水。userID 由调用方从会话里取，绝不接受请求体里的用户 id。
func (s *SupplierCreditService) ListLedger(ctx context.Context, filter SupplierCreditLedgerFilter) ([]SupplierCreditLedgerEntry, int64, error) {
	if s == nil || s.repo == nil {
		return nil, 0, infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "supplier credit service unavailable")
	}
	if filter.UserID <= 0 {
		return nil, 0, infraerrors.BadRequest("INVALID_USER", "invalid user")
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = supplierLedgerDefaultPageSize
	}
	if filter.PageSize > supplierLedgerMaxPageSize {
		filter.PageSize = supplierLedgerMaxPageSize
	}
	return s.repo.ListLedger(ctx, filter)
}
