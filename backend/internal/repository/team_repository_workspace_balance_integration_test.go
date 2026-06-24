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

func TestListWorkspaces_FillsTeamBalance(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewTeamRepository(client)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("ws-bal-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      5,
	})

	// 用 helper 建合法 team + team billing_subject（满足 owner_check 约束）
	teamID, _ := mustCreateTeamWithBalance(t, client, ctx, user.ID, 250)

	// 把 user 设为该团队的 active 成员（owner）
	_, err := client.TeamMember.Create().
		SetTeamID(teamID).SetUserID(user.ID).
		SetRole(domain.TeamRoleOwner).
		SetStatus(domain.TeamMemberStatusActive).
		SetJoinedAt(time.Now()).
		Save(ctx)
	require.NoError(t, err)

	items, err := repo.ListWorkspaces(ctx, user.ID)
	require.NoError(t, err)

	var teamItem *service.WorkspaceSubject
	for i := range items {
		if items[i].Type == domain.BillingSubjectTypeTeam && items[i].TeamID == teamID {
			teamItem = &items[i]
		}
	}
	require.NotNil(t, teamItem, "team workspace must be present")
	require.InDelta(t, 250, teamItem.Balance, 0.000001)
}
