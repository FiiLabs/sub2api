//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// TestGetDashboardStatsBySubject 验证按计费主体+可选 actor 的仪表盘统计聚合：
// 1. actorUserID=0 → 主体全量（涵盖所有 actor）
// 2. actorUserID=actor1 → 仅 actor1 的切片
// 3. api_key 统计：主体全量 vs actor 过滤（created_by_user_id）
// 4. 不同主体之间互相隔离
func TestGetDashboardStatsBySubject(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	now := time.Now().UTC()

	// 创建两个 actor 用户
	actor1 := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("dash-actor1-%s@test.com", uuid.NewString()),
	})
	actor2 := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("dash-actor2-%s@test.com", uuid.NewString()),
	})

	// 创建团队及对应 billing_subject
	_, teamSubjID := mustCreateTeamWithBalance(t, client, ctx, actor1.ID, 0)

	// 创建关联账号（日志所需）
	account := mustCreateAccount(t, client, &service.Account{
		Name: "dash-subj-acct-" + uuid.NewString(),
	})

	// 创建 api_key：actor1 名下的 key（billing_subject_id + created_by_user_id）
	apiKeyActor1, err := client.APIKey.Create().
		SetUserID(actor1.ID).
		SetKey("sk-dash-actor1-" + uuid.NewString()).
		SetName("actor1-key").
		SetStatus(service.StatusActive).
		SetBillingSubjectID(teamSubjID).
		SetCreatedByUserID(actor1.ID).
		Save(ctx)
	require.NoError(t, err, "create apiKey for actor1")

	// 创建 api_key：actor2 名下的 key（billing_subject_id + created_by_user_id）
	_, err = client.APIKey.Create().
		SetUserID(actor2.ID).
		SetKey("sk-dash-actor2-" + uuid.NewString()).
		SetName("actor2-key").
		SetStatus(service.StatusActive).
		SetBillingSubjectID(teamSubjID).
		SetCreatedByUserID(actor2.ID).
		Save(ctx)
	require.NoError(t, err, "create apiKey for actor2")

	// repo 实例（写入后用同一 DB 聚合）
	repo := newUsageLogRepositoryWithSQL(client, integrationDB)

	// actor1 写入 2 条日志（input_tokens=10 each, cost=0.1）
	for i := 0; i < 2; i++ {
		insertUsageLogForSubjectTest(t, ctx, integrationDB,
			actor1.ID, teamSubjID, actor1.ID,
			apiKeyActor1.ID, account.ID,
			10, 5, 0.1, now)
	}

	// actor2 写入 1 条日志（input_tokens=30, cost=0.3）
	insertUsageLogForSubjectTest(t, ctx, integrationDB,
		actor2.ID, teamSubjID, actor2.ID,
		apiKeyActor1.ID, account.ID,
		30, 15, 0.3, now)

	// --- 断言 1：actorUserID=0 → 主体全量（3 条请求）---
	allStats, err := repo.GetDashboardStatsBySubject(ctx, teamSubjID, 0)
	require.NoError(t, err)
	require.Equal(t, int64(3), allStats.TotalRequests, "全量应包含两个 actor 的 3 条日志")
	require.Equal(t, int64(50), allStats.TotalInputTokens, "actor1×2×10 + actor2×30 = 50")

	// --- 断言 2：actorUserID=actor1 → 仅 actor1 的切片（2 条）---
	actor1Stats, err := repo.GetDashboardStatsBySubject(ctx, teamSubjID, actor1.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), actor1Stats.TotalRequests, "actor1 过滤后应为 2 条")
	require.Equal(t, int64(20), actor1Stats.TotalInputTokens, "actor1 input_tokens: 2×10=20")

	// --- 断言 3：API Key 统计 — 主体全量应有 2 个 key，actor1 过滤后仅 1 个 ---
	require.Equal(t, int64(2), allStats.TotalAPIKeys, "主体下共 2 个 key")
	require.Equal(t, int64(2), allStats.ActiveAPIKeys, "主体下共 2 个活跃 key")
	require.Equal(t, int64(1), actor1Stats.TotalAPIKeys, "actor1 名下仅 1 个 key")
	require.Equal(t, int64(1), actor1Stats.ActiveAPIKeys, "actor1 名下仅 1 个活跃 key")

	// --- 断言 4：不同主体互相隔离 ---
	_, otherSubjID := mustCreateTeamWithBalance(t, client, ctx, actor1.ID, 0)
	otherStats, err := repo.GetDashboardStatsBySubject(ctx, otherSubjID, 0)
	require.NoError(t, err)
	require.Equal(t, int64(0), otherStats.TotalRequests, "不同主体的日志不应被计入")
	require.Equal(t, int64(0), otherStats.TotalAPIKeys, "不同主体的 key 不应被计入")
}
