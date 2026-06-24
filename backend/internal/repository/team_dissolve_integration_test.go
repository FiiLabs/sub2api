//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// TestDissolveTeam_SoftDeletesTeamMembersAndSubject 验证解散团队在单事务内按
// 「成员 → 团队 → 计费主体」顺序软删，且不触发 migration 150 的
// billing_subjects_referenced_invariant 触发器（即团队必须先于其计费主体软删）。
func TestDissolveTeam_SoftDeletesTeamMembersAndSubject(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewTeamRepository(client)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("dissolve-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})

	// 用 helper 建合法 team + team billing_subject（余额 0，满足 owner_check 约束、
	// 且 teams_billing_subject_invariant 通过：先建 team→建主体 SetTeamID→回填）。
	teamID, subjID := mustCreateTeamWithBalance(t, client, ctx, user.ID, 0)

	// 把 user 设为该团队的 active 成员（owner）。
	_, err := client.TeamMember.Create().
		SetTeamID(teamID).SetUserID(user.ID).
		SetRole(domain.TeamRoleOwner).
		SetStatus(domain.TeamMemberStatusActive).
		SetJoinedAt(time.Now()).
		Save(ctx)
	require.NoError(t, err)

	// 关键断言：软删顺序正确则不触发 billing_subjects_referenced_invariant。
	require.NoError(t, repo.DissolveTeam(ctx, teamID))

	// 团队、成员、计费主体均被软删（deleted_at 非空）。
	var teamDeleted, subjDeleted, memberDeleted int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM teams WHERE id=$1 AND deleted_at IS NOT NULL", teamID).Scan(&teamDeleted))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM billing_subjects WHERE id=$1 AND deleted_at IS NOT NULL", subjID).Scan(&subjDeleted))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM team_members WHERE team_id=$1 AND deleted_at IS NOT NULL", teamID).Scan(&memberDeleted))
	require.Equal(t, 1, teamDeleted)
	require.Equal(t, 1, subjDeleted)
	require.Equal(t, 1, memberDeleted)
}
