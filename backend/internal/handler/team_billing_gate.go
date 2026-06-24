package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/domain"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// RequireTeamBillingManage 是所有「团队计费管理」入口（兑换 / 在线支付 / 订阅购买）共用的权限闸。
//   - 个人主体：放行（用户对自己的钱包有完全权限）。
//   - 团队主体：必须持有 team.billing.manage（owner/admin/billing），否则拒绝。
//
// 共用同一 helper，避免逐接口口径漂移（CPO 备注 2）。
func RequireTeamBillingManage(subject middleware2.AuthSubject) error {
	if subject.SubjectType == domain.BillingSubjectTypeTeam && !subject.Permissions[domain.TeamPermissionManageBilling] {
		return service.ErrTeamPermissionDenied
	}
	return nil
}
