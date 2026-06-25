package domain

// TeamKeyCreatorFilter 返回 key 读/管的 created_by_user_id 限制。
// 返回 nil = 不限制（个人主体，或团队 owner/admin 持 team.keys.manage.all）。
// 返回非 nil = 收敛到 actorUserID 自己创建的 key。
func TeamKeyCreatorFilter(subjectType string, permissions map[string]bool, actorUserID int64) *int64 {
	if subjectType != BillingSubjectTypeTeam || permissions[TeamPermissionManageKeysAll] {
		return nil
	}
	v := actorUserID
	return &v
}

// TeamUsageActorFilter 解析 usage 读的 actor_user_id 限制。
// requested = 请求传入的 actor（0 表示未传）。
// ok=false 表示普通成员尝试查看他人 actor，handler 应返回 403。
func TeamUsageActorFilter(subjectType string, permissions map[string]bool, actorUserID, requested int64) (filter int64, ok bool) {
	if subjectType != BillingSubjectTypeTeam || permissions[TeamPermissionViewUsageAll] {
		return requested, true
	}
	if requested != 0 && requested != actorUserID {
		return 0, false
	}
	return actorUserID, true
}
