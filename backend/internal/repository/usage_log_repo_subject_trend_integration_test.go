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

// TestGetSubjectUsageTrend 验证按计费主体（+可选 actor）的使用趋势聚合：
// 1. actorUserID=0 → 主体全量（涵盖所有 actor）
// 2. actorUserID>0 → 仅该 actor 的切片
// 3. 不同主体之间互相隔离
func TestGetSubjectUsageTrend(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	now := time.Now().UTC()
	start := now.Add(-time.Minute)
	end := now.Add(time.Minute)

	actor1 := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("trend-actor1-%s@test.com", uuid.NewString()),
	})
	actor2 := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("trend-actor2-%s@test.com", uuid.NewString()),
	})

	_, teamSubjID := mustCreateTeamWithBalance(t, client, ctx, actor1.ID, 0)

	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: actor1.ID, Key: "sk-trend-" + uuid.NewString(), Name: "k",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "trend-acct-" + uuid.NewString(),
	})

	repo := newUsageLogRepositoryWithSQL(client, integrationDB)

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

	// --- 断言 1：actorUserID=0 → 主体全量（3 条请求，总 input=50）---
	allTrend, err := repo.GetSubjectUsageTrend(ctx, teamSubjID, 0, start, end, "day")
	require.NoError(t, err)
	require.NotEmpty(t, allTrend, "全量趋势不应为空")
	var totalRequests int64
	var totalInputTokens int64
	for _, p := range allTrend {
		totalRequests += p.Requests
		totalInputTokens += p.InputTokens
	}
	require.Equal(t, int64(3), totalRequests, "全量应包含两个 actor 的 3 条日志")
	require.Equal(t, int64(50), totalInputTokens, "actor1×2×10 + actor2×30 = 50")

	// --- 断言 2：actorUserID=actor1 → 仅 actor1 的切片（2 条, input=20）---
	actor1Trend, err := repo.GetSubjectUsageTrend(ctx, teamSubjID, actor1.ID, start, end, "day")
	require.NoError(t, err)
	var actor1Requests int64
	var actor1InputTokens int64
	for _, p := range actor1Trend {
		actor1Requests += p.Requests
		actor1InputTokens += p.InputTokens
	}
	require.Equal(t, int64(2), actor1Requests, "actor1 过滤后应为 2 条")
	require.Equal(t, int64(20), actor1InputTokens, "actor1 input_tokens: 2×10=20")

	// --- 断言 3：不同主体互相隔离 ---
	_, otherSubjID := mustCreateTeamWithBalance(t, client, ctx, actor1.ID, 0)
	otherTrend, err := repo.GetSubjectUsageTrend(ctx, otherSubjID, 0, start, end, "day")
	require.NoError(t, err)
	var otherRequests int64
	for _, p := range otherTrend {
		otherRequests += p.Requests
	}
	require.Equal(t, int64(0), otherRequests, "不同主体的日志不应被计入")
}

// TestGetSubjectModelStats 验证按计费主体（+可选 actor）的模型统计聚合：
// 1. actorUserID=0 → 主体全量（涵盖所有 actor）
// 2. actorUserID>0 → 仅该 actor 的切片
// 3. 不同主体之间互相隔离
func TestGetSubjectModelStats(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	now := time.Now().UTC()
	start := now.Add(-time.Minute)
	end := now.Add(time.Minute)

	actor1 := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("model-actor1-%s@test.com", uuid.NewString()),
	})
	actor2 := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("model-actor2-%s@test.com", uuid.NewString()),
	})

	_, teamSubjID := mustCreateTeamWithBalance(t, client, ctx, actor1.ID, 0)

	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: actor1.ID, Key: "sk-model-" + uuid.NewString(), Name: "k",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "model-acct-" + uuid.NewString(),
	})

	repo := newUsageLogRepositoryWithSQL(client, integrationDB)

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

	// --- 断言 1：actorUserID=0 → 主体全量（3 条请求，总 input=50）---
	allStats, err := repo.GetSubjectModelStats(ctx, teamSubjID, 0, start, end)
	require.NoError(t, err)
	require.NotEmpty(t, allStats, "全量模型统计不应为空")
	var totalRequests int64
	var totalInputTokens int64
	for _, s := range allStats {
		totalRequests += s.Requests
		totalInputTokens += s.InputTokens
	}
	require.Equal(t, int64(3), totalRequests, "全量应包含两个 actor 的 3 条日志")
	require.Equal(t, int64(50), totalInputTokens, "actor1×2×10 + actor2×30 = 50")

	// --- 断言 2：actorUserID=actor1 → 仅 actor1 的切片（2 条, input=20）---
	actor1Stats, err := repo.GetSubjectModelStats(ctx, teamSubjID, actor1.ID, start, end)
	require.NoError(t, err)
	var actor1Requests int64
	var actor1InputTokens int64
	for _, s := range actor1Stats {
		actor1Requests += s.Requests
		actor1InputTokens += s.InputTokens
	}
	require.Equal(t, int64(2), actor1Requests, "actor1 过滤后应为 2 条")
	require.Equal(t, int64(20), actor1InputTokens, "actor1 input_tokens: 2×10=20")

	// --- 断言 3：不同主体互相隔离 ---
	_, otherSubjID := mustCreateTeamWithBalance(t, client, ctx, actor1.ID, 0)
	otherStats, err := repo.GetSubjectModelStats(ctx, otherSubjID, 0, start, end)
	require.NoError(t, err)
	var otherRequests int64
	for _, s := range otherStats {
		otherRequests += s.Requests
	}
	require.Equal(t, int64(0), otherRequests, "不同主体的日志不应被计入")
}
