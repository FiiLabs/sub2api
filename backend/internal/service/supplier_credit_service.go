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

// ShareRatio 当前分成比例，给供给者界面**如实显示**用。
//
// 存在的理由是一条已经踩过的坑：这个数早先只写死在前端文案和文档里，而它其实是
// 运营随时可改的设置。运营改了、文案没改，界面就会对着供给者报一个错的比例——
// 而这恰恰是他决定要不要挂号时唯一在意的数字。
//
// 读不到时回 0，调用方据此**不显示**（而不是显示一个 0%）：说不出比例
// 比说一个错的比例好，后者会被当成承诺。
func (s *SupplierCreditService) ShareRatio(ctx context.Context) float64 {
	if s == nil || s.settingService == nil {
		return 0
	}
	settings := s.settingService.GetSupplierSettlementSettings(ctx)
	if settings == nil {
		return 0
	}
	return settings.ShareRatio
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
	entries, total, err := s.repo.ListLedger(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	return stripConsumerIdentity(entries), total, nil
}

// stripConsumerIdentity 抹掉流水里指向消费者的字段。
//
// 供给者的应得信息是「谁付的钱不关我事，我这一笔赚了多少」。SourceUserID 是入账
// 幂等与追回用的内部关联键，翻页拉一遍就能得到一份「谁在用我的号」的 user_id 序列，
// 那是消费者的隐私，不是供给者的对账依据——对账要的 request_id / account_id / 金额
// 都还在。
//
// 抹在这一层而不是 handler 层：本服务就是「供给者视角」的边界，日后再挂一个
// 读流水的 handler 也不会重新把它漏出去。运营侧走的是 SupplierAdminService，
// 那里保留该字段（追一笔拒付必须能定位到消费者），两条路径的类型是同一个，
// 差别只在这个函数。
func stripConsumerIdentity(entries []SupplierCreditLedgerEntry) []SupplierCreditLedgerEntry {
	for i := range entries {
		entries[i].SourceUserID = nil
	}
	return entries
}
