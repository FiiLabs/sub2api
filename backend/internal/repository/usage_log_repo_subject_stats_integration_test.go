//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// insertUsageLogForSubjectTest 直接写 SQL，包含 billing_subject_id 和 actor_user_id，
// 绕过 createSingle（其 INSERT 不含这两列）。
func insertUsageLogForSubjectTest(t *testing.T, ctx context.Context, db *sql.DB,
	userID, billingSubjectID, actorUserID, apiKeyID, accountID int64,
	inputTokens, outputTokens int, cost float64, createdAt time.Time) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		INSERT INTO usage_logs
			(user_id, api_key_id, account_id, request_id, model,
			 billing_subject_id, actor_user_id,
			 input_tokens, output_tokens,
			 total_cost, actual_cost, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		userID, apiKeyID, accountID, uuid.NewString(), "claude-3",
		billingSubjectID, actorUserID,
		inputTokens, outputTokens,
		cost, cost, createdAt,
	)
	require.NoError(t, err, "insert usage_log with billing_subject_id")
}

// TestGetSubjectStatsAggregatedFiltersBySubjectAndActor validates that:
// 1. calling with actorUserID=0 returns totals for all actors under the subject.
// 2. calling with actorUserID>0 returns only that actor's slice.
// 3. rows belonging to a different billing_subject are not included.
func TestGetSubjectStatsAggregatedFiltersBySubjectAndActor(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	now := time.Now().UTC()

	actor1 := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("actor1-%s@test.com", uuid.NewString()),
	})
	actor2 := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("actor2-%s@test.com", uuid.NewString()),
	})

	_, teamSubjID := mustCreateTeamWithBalance(t, client, ctx, actor1.ID, 0)

	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: actor1.ID, Key: "sk-subj-stats-" + uuid.NewString(), Name: "k",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "subj-stats-acct-" + uuid.NewString(),
	})

	// 用 integrationDB 读写：写入带 billing_subject_id/actor_user_id 的行（raw SQL），
	// 再用 repo（也指向 integrationDB）聚合。用唯一的 teamSubjID 保证测试隔离。
	repo := newUsageLogRepositoryWithSQL(client, integrationDB)

	start := now.Add(-time.Minute)
	end := now.Add(time.Minute)

	// actor1 写入 2 条日志 (input=10 each)
	for i := 0; i < 2; i++ {
		insertUsageLogForSubjectTest(t, ctx, integrationDB,
			actor1.ID, teamSubjID, actor1.ID,
			apiKey.ID, account.ID,
			10, 5, 0.1, now)
	}

	// actor2 写入 1 条日志 (input=30)
	insertUsageLogForSubjectTest(t, ctx, integrationDB,
		actor2.ID, teamSubjID, actor2.ID,
		apiKey.ID, account.ID,
		30, 15, 0.3, now)

	// --- 断言 1：actorUserID=0 → 主体全量（3 条请求）---
	allStats, err := repo.GetSubjectStatsAggregated(ctx, teamSubjID, 0, start, end)
	require.NoError(t, err)
	require.Equal(t, int64(3), allStats.TotalRequests, "全量应包含两个 actor 的 3 条日志")
	require.Equal(t, int64(50), allStats.TotalInputTokens, "actor1×2×10 + actor2×30 = 50")

	// --- 断言 2：actorUserID=actor1 → 只含 actor1 的日志（2 条）---
	actor1Stats, err := repo.GetSubjectStatsAggregated(ctx, teamSubjID, actor1.ID, start, end)
	require.NoError(t, err)
	require.Equal(t, int64(2), actor1Stats.TotalRequests, "actor1 过滤后应为 2 条")
	require.Equal(t, int64(20), actor1Stats.TotalInputTokens, "actor1 input_tokens: 2×10=20")

	// --- 断言 3：actorUserID=actor2 → 只含 actor2 的日志（1 条）---
	actor2Stats, err := repo.GetSubjectStatsAggregated(ctx, teamSubjID, actor2.ID, start, end)
	require.NoError(t, err)
	require.Equal(t, int64(1), actor2Stats.TotalRequests, "actor2 过滤后应为 1 条")
	require.Equal(t, int64(30), actor2Stats.TotalInputTokens, "actor2 input_tokens: 30")

	// --- 断言 4：不同计费主体不应互相干扰 ---
	_, otherSubjID := mustCreateTeamWithBalance(t, client, ctx, actor1.ID, 0)
	otherStats, err := repo.GetSubjectStatsAggregated(ctx, otherSubjID, 0, start, end)
	require.NoError(t, err)
	require.Equal(t, int64(0), otherStats.TotalRequests, "不同主体的日志不应被计入")
}
