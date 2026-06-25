//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestTeamUsageByMember 验证 teamRepository.UsageByMember：
//   - 按 actor_user_id 聚合请求数、actual_cost、total_cost；
//   - 时间窗外的记录被排除；
//   - 其他团队的记录被排除；
//   - actor_user_id 为空的记录（非人工行为）被排除。
func TestTeamUsageByMember(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	ts := time.Now().UnixNano()

	// 建两个用户作为团队成员。
	actor1 := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("actor1-%d@member-usage.test", ts),
		PasswordHash: "h",
	})
	actor2 := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("actor2-%d@member-usage.test", ts),
		PasswordHash: "h",
	})

	// 目标团队（带 billing_subject）。
	teamID, _ := mustCreateTeamWithBalance(t, client, ctx, actor1.ID, 0)

	// 另一个团队（actor2 的用量不得渗入 teamID）。
	otherTeamID, _ := mustCreateTeamWithBalance(t, client, ctx, actor2.ID, 0)

	// 给目标团队建一个 API key（用量日志需要 api_key_id）。
	keyID := mustCreateTeamAPIKey(t, client, teamID, actor1.ID, fmt.Sprintf("k1-%d", ts))
	otherKeyID := mustCreateTeamAPIKey(t, client, otherTeamID, actor2.ID, fmt.Sprintf("k2-%d", ts))

	accID := mustCreateMinimalAccount(t, client)

	// 时间窗：今天内。
	windowStart := time.Now().Add(-24 * time.Hour)
	windowEnd := time.Now().Add(time.Hour)

	// Actor1 在目标团队内的记录（窗口内 2 条）。
	mustCreateTeamUsageWithTotal(t, client, teamID, actor1.ID, keyID, accID,
		1.0, 1.2, windowStart.Add(time.Minute), fmt.Sprintf("r1a-%d", ts))
	mustCreateTeamUsageWithTotal(t, client, teamID, actor1.ID, keyID, accID,
		0.5, 0.6, windowStart.Add(2*time.Minute), fmt.Sprintf("r1b-%d", ts))

	// Actor2 在目标团队内的记录（窗口内 1 条）。
	mustCreateTeamUsageWithTotal(t, client, teamID, actor2.ID, keyID, accID,
		2.0, 2.5, windowStart.Add(3*time.Minute), fmt.Sprintf("r2a-%d", ts))

	// 窗口外的记录（应排除）。
	mustCreateTeamUsageWithTotal(t, client, teamID, actor1.ID, keyID, accID,
		99.0, 99.0, windowStart.Add(-48*time.Hour), fmt.Sprintf("rout-%d", ts))

	// 另一团队的 actor2 记录（应排除）。
	mustCreateTeamUsageWithTotal(t, client, otherTeamID, actor2.ID, otherKeyID, accID,
		50.0, 50.0, windowStart.Add(time.Minute), fmt.Sprintf("rother-%d", ts))

	// actor_user_id = NULL 的记录（SetActorUserID 不设；需手动 SQL，
	// 因为 ent builder 会把 0 也写进去，改用原始 SQL 插入）。
	mustCreateTeamUsageNilActor(t, client, teamID, keyID, accID, 77.0, windowStart.Add(time.Minute), fmt.Sprintf("rnil-%d", ts))

	repo := NewTeamRepository(client)
	rows, err := repo.UsageByMember(ctx, teamID, windowStart, windowEnd)
	require.NoError(t, err)

	// 建立 per-actor 映射。
	byActor := make(map[int64]service.TeamMemberUsage, len(rows))
	for _, row := range rows {
		byActor[row.UserID] = row
	}

	// Actor1：2 条记录，actual_cost = 1.0+0.5 = 1.5，total_cost = 1.2+0.6 = 1.8，requests = 2。
	a1, ok := byActor[actor1.ID]
	require.True(t, ok, "actor1 row should be present")
	require.Equal(t, int64(2), a1.Requests, "actor1 requests")
	require.InDelta(t, 1.5, a1.ActualCost, 1e-9, "actor1 actual_cost")
	require.InDelta(t, 1.8, a1.TotalCost, 1e-9, "actor1 total_cost")

	// Actor2：1 条记录（另一团队记录不计），actual_cost = 2.0，total_cost = 2.5。
	a2, ok := byActor[actor2.ID]
	require.True(t, ok, "actor2 row should be present")
	require.Equal(t, int64(1), a2.Requests, "actor2 requests")
	require.InDelta(t, 2.0, a2.ActualCost, 1e-9, "actor2 actual_cost")
	require.InDelta(t, 2.5, a2.TotalCost, 1e-9, "actor2 total_cost")

	// 窗口外的 99.0 和另一团队的 50.0 均未出现。
	require.Equal(t, 2, len(rows), "only 2 actor rows should be returned")
}

// mustCreateTeamUsageWithTotal inserts a usage_log row with both actual_cost and total_cost.
func mustCreateTeamUsageWithTotal(t *testing.T, client *dbent.Client, teamID, actorUserID, apiKeyID, accountID int64, actualCost, totalCost float64, createdAt time.Time, reqID string) {
	t.Helper()
	_, err := client.UsageLog.Create().
		SetUserID(actorUserID).
		SetAPIKeyID(apiKeyID).
		SetAccountID(accountID).
		SetRequestID(reqID).
		SetModel("claude-3").
		SetTeamID(teamID).
		SetActorUserID(actorUserID).
		SetActualCost(actualCost).
		SetTotalCost(totalCost).
		SetCreatedAt(createdAt).
		Save(context.Background())
	require.NoError(t, err)
}

// mustCreateTeamUsageNilActor inserts a usage_log row with actor_user_id IS NULL.
// These rows must be excluded from UsageByMember results.
func mustCreateTeamUsageNilActor(t *testing.T, client *dbent.Client, teamID, apiKeyID, accountID int64, actualCost float64, createdAt time.Time, reqID string) {
	t.Helper()
	// 目标团队的 owner（actor1）用作 user_id 占位；actor_user_id 故意不设（NULL）。
	_, err := client.UsageLog.Create().
		SetUserID(1). // placeholder, not the actor
		SetAPIKeyID(apiKeyID).
		SetAccountID(accountID).
		SetRequestID(reqID).
		SetModel("claude-3").
		SetTeamID(teamID).
		SetActualCost(actualCost).
		SetCreatedAt(createdAt).
		Save(context.Background())
	require.NoError(t, err)
}

// mustCreateTeamWithBalance is defined in fixtures_integration_test.go.
// mustCreateUser, mustCreateTeamAPIKey, mustCreateMinimalAccount are in team_repository_unit_test.go.
// These are accessible because all files share the `repository` test package.
var _ = domain.TeamRoleOwner // ensure domain import is used
