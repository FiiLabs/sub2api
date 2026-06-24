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
	// 团队主体余额 250；建团队 + 关联主体 + 把 user 设为 active 成员（owner）。
	teamSubj, err := client.BillingSubject.Create().
		SetType(domain.BillingSubjectTypeTeam).
		SetStatus(domain.StatusActive).
		SetBalance(250).
		Save(ctx)
	require.NoError(t, err)
	team, err := client.Team.Create().
		SetName("T-" + fmt.Sprint(time.Now().UnixNano())).
		SetSlug("t-" + fmt.Sprint(time.Now().UnixNano())).
		SetOwnerUserID(user.ID).
		SetCreatedByUserID(user.ID).
		SetStatus(domain.TeamStatusActive).
		SetBillingSubjectID(teamSubj.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.TeamMember.Create().
		SetTeamID(team.ID).SetUserID(user.ID).
		SetRole(domain.TeamRoleOwner).
		SetStatus(domain.TeamMemberStatusActive).
		SetJoinedAt(time.Now()).
		Save(ctx)
	require.NoError(t, err)

	items, err := repo.ListWorkspaces(ctx, user.ID)
	require.NoError(t, err)

	var teamItem *service.WorkspaceSubject
	for i := range items {
		if items[i].Type == domain.BillingSubjectTypeTeam && items[i].TeamID == team.ID {
			teamItem = &items[i]
		}
	}
	require.NotNil(t, teamItem, "team workspace must be present")
	require.InDelta(t, 250, teamItem.Balance, 0.000001)
}
