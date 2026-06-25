package domain

const (
	BillingSubjectTypeUser = "user"
	BillingSubjectTypeTeam = "team"

	TeamStatusActive   = "active"
	TeamStatusDisabled = "disabled"

	TeamMemberStatusActive    = "active"
	TeamMemberStatusSuspended = "suspended"
	TeamMemberStatusLeft      = "left"

	TeamInvitationStatusPending  = "pending"
	TeamInvitationStatusAccepted = "accepted"
	TeamInvitationStatusExpired  = "expired"
	TeamInvitationStatusRevoked  = "revoked"

	TeamRoleOwner     = "owner"
	TeamRoleAdmin     = "admin"
	TeamRoleBilling   = "billing"
	TeamRoleDeveloper = "developer"
	TeamRoleViewer    = "viewer"

	TeamPermissionViewUsage         = "team.usage.view"
	TeamPermissionManageKeys        = "team.keys.manage"
	TeamPermissionManageMembers     = "team.members.manage"
	TeamPermissionManageBilling     = "team.billing.manage"
	TeamPermissionManageSettings    = "team.settings.manage"
	TeamPermissionDissolveTeam      = "team.dissolve"
	TeamPermissionTransferOwnership = "team.ownership.transfer"

	// Self/all 分级：owner/admin 才有 *.all，可跨成员查看/管理团队全量资源。
	TeamPermissionManageKeysAll = "team.keys.manage.all"
	TeamPermissionViewUsageAll  = "team.usage.view.all"
)

func TeamRolePermissions(role string) map[string]bool {
	switch role {
	case TeamRoleOwner:
		return map[string]bool{
			TeamPermissionViewUsage:         true,
			TeamPermissionManageKeys:        true,
			TeamPermissionManageMembers:     true,
			TeamPermissionManageBilling:     true,
			TeamPermissionManageSettings:    true,
			TeamPermissionDissolveTeam:      true,
			TeamPermissionTransferOwnership: true,
			TeamPermissionManageKeysAll:     true,
			TeamPermissionViewUsageAll:      true,
		}
	case TeamRoleAdmin:
		return map[string]bool{
			TeamPermissionViewUsage:      true,
			TeamPermissionManageKeys:     true,
			TeamPermissionManageMembers:  true,
			TeamPermissionManageBilling:  true,
			TeamPermissionManageSettings: true,
			TeamPermissionManageKeysAll:  true,
			TeamPermissionViewUsageAll:   true,
		}
	case TeamRoleBilling:
		return map[string]bool{TeamPermissionViewUsage: true, TeamPermissionManageBilling: true}
	case TeamRoleDeveloper:
		return map[string]bool{TeamPermissionViewUsage: true, TeamPermissionManageKeys: true}
	case TeamRoleViewer:
		return map[string]bool{TeamPermissionViewUsage: true}
	default:
		return map[string]bool{}
	}
}
