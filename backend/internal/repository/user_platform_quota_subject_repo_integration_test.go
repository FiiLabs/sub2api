//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// mustCreatePersonalSubject 为指定 user 创建一个 type='user' 的个人计费主体，返回其 ID。
// 满足 user_platform_quotas.billing_subject_id 的 FK；个人主体唯一索引保证每 user 仅一个。
func mustCreatePersonalSubject(t *testing.T, client *dbent.Client, ctx context.Context, userID int64) int64 {
	t.Helper()
	bs, err := client.BillingSubject.Create().
		SetType(domain.BillingSubjectTypeUser).
		SetUserID(userID).
		SetStatus(service.StatusActive).
		SetBalance(0).
		SetConcurrency(5).
		Save(ctx)
	require.NoError(t, err, "create personal billing subject")
	return bs.ID
}

func TestUserPlatformQuotaRepository_GetBySubjectPlatform(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateUserForQuota(t, client)
	subjectID := mustCreatePersonalSubject(t, client, txCtx, userID)

	repo := NewUserPlatformQuotaRepository(client)

	// 未插入时应返回 nil
	rec, err := repo.GetBySubjectPlatform(txCtx, subjectID, "anthropic")
	require.NoError(t, err, "get before insert should not error")
	require.Nil(t, rec, "get before insert should return nil")

	// 通过 subject 维度限额写入后查询
	daily := 12.0
	require.NoError(t, repo.UpsertForSubject(txCtx, subjectID, []UserPlatformQuotaRecord{
		{BillingSubjectID: subjectID, Platform: "anthropic", DailyLimitUSD: &daily},
	}))

	rec, err = repo.GetBySubjectPlatform(txCtx, subjectID, "anthropic")
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, subjectID, rec.BillingSubjectID)
	require.Equal(t, "anthropic", rec.Platform)
	require.NotNil(t, rec.DailyLimitUSD)
	require.InDelta(t, 12.0, *rec.DailyLimitUSD, 1e-9)
}

func TestUserPlatformQuotaRepository_IncrementUsageWithResetBySubject_SameWindow(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	userID := mustCreateUserForQuota(t, client)
	subjectID := mustCreatePersonalSubject(t, client, ctx, userID)

	repo := NewUserPlatformQuotaRepository(client)
	now := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC) // 周五

	// 首次调用：应新建 subject 维度行（user_id 留空）
	require.NoError(t, repo.IncrementUsageWithResetBySubject(ctx, subjectID, "anthropic", 1.5, now))
	rec, err := repo.GetBySubjectPlatform(ctx, subjectID, "anthropic")
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.InDelta(t, 1.5, rec.DailyUsageUSD, 1e-9)
	require.InDelta(t, 1.5, rec.WeeklyUsageUSD, 1e-9)
	require.InDelta(t, 1.5, rec.MonthlyUsageUSD, 1e-9)

	// 同窗口再次累加
	require.NoError(t, repo.IncrementUsageWithResetBySubject(ctx, subjectID, "anthropic", 0.5, now))
	rec, err = repo.GetBySubjectPlatform(ctx, subjectID, "anthropic")
	require.NoError(t, err)
	require.InDelta(t, 2.0, rec.DailyUsageUSD, 1e-9)
	require.InDelta(t, 2.0, rec.WeeklyUsageUSD, 1e-9)
	require.InDelta(t, 2.0, rec.MonthlyUsageUSD, 1e-9)
}

// TestUserPlatformQuotaRepository_IncrementUsageWithResetBySubject_TeamShared 验证
// 团队语义：多次（模拟多成员）对同一 billing_subject 的消耗累加到唯一一行，共享同一限额。
func TestUserPlatformQuotaRepository_IncrementUsageWithResetBySubject_TeamShared(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	// 用一个 user-type 主体承载（repo 仅按 subjectID 键，不依赖 team 机制）。
	owner := mustCreateUserForQuota(t, client)
	subjectID := mustCreatePersonalSubject(t, client, ctx, owner)

	repo := NewUserPlatformQuotaRepository(client)
	now := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)

	// 成员 A 与成员 B 的消耗都计入同一 subject。
	require.NoError(t, repo.IncrementUsageWithResetBySubject(ctx, subjectID, "openai", 3.0, now))
	require.NoError(t, repo.IncrementUsageWithResetBySubject(ctx, subjectID, "openai", 4.0, now))

	list, err := repo.ListBySubject(ctx, subjectID)
	require.NoError(t, err)
	// 唯一约束：同 subject 同 platform 仅一行。
	count := 0
	var openai *UserPlatformQuotaRecord
	for i := range list {
		if list[i].Platform == "openai" {
			count++
			openai = &list[i]
		}
	}
	require.Equal(t, 1, count, "team subject + platform should have exactly one row")
	require.NotNil(t, openai)
	require.InDelta(t, 7.0, openai.DailyUsageUSD, 1e-9, "two members' usage shares one subject row")
}

func TestUserPlatformQuotaRepository_UpsertForSubject_ReplaceAndSoftDelete(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateUserForQuota(t, client)
	subjectID := mustCreatePersonalSubject(t, client, txCtx, userID)

	repo := NewUserPlatformQuotaRepository(client)

	d1, d2 := 5.0, 10.0
	require.NoError(t, repo.UpsertForSubject(txCtx, subjectID, []UserPlatformQuotaRecord{
		{BillingSubjectID: subjectID, Platform: "anthropic", DailyLimitUSD: &d1},
		{BillingSubjectID: subjectID, Platform: "openai", DailyLimitUSD: &d2},
	}))
	list, err := repo.ListBySubject(txCtx, subjectID)
	require.NoError(t, err)
	require.Len(t, list, 2)

	// 第二次仅保留 anthropic → openai 应被软删除
	d3 := 7.0
	require.NoError(t, repo.UpsertForSubject(txCtx, subjectID, []UserPlatformQuotaRecord{
		{BillingSubjectID: subjectID, Platform: "anthropic", DailyLimitUSD: &d3},
	}))
	list, err = repo.ListBySubject(txCtx, subjectID)
	require.NoError(t, err)
	require.Len(t, list, 1, "openai should be soft-deleted")
	require.Equal(t, "anthropic", list[0].Platform)
	require.NotNil(t, list[0].DailyLimitUSD)
	require.InDelta(t, 7.0, *list[0].DailyLimitUSD, 1e-9, "anthropic limit updated to 7.0")
}

func TestUserPlatformQuotaRepository_ResetExpiredWindowBySubject(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateUserForQuota(t, client)
	subjectID := mustCreatePersonalSubject(t, client, txCtx, userID)

	repo := NewUserPlatformQuotaRepository(client)

	// 通过 ent 直接建一条 subject 维度记录（user_id 留空）。
	_, err := client.UserPlatformQuota.Create().
		SetBillingSubjectID(subjectID).
		SetPlatform("gemini").
		SetDailyUsageUsd(10.0).
		SetWeeklyUsageUsd(20.0).
		SetMonthlyUsageUsd(50.0).
		SetDailyWindowStart(time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)).
		SetWeeklyWindowStart(time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)).
		SetMonthlyWindowStart(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)).
		Save(txCtx)
	require.NoError(t, err)

	newStart := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.ResetExpiredWindowBySubject(txCtx, subjectID, "gemini", "daily", newStart))

	rec, err := repo.GetBySubjectPlatform(txCtx, subjectID, "gemini")
	require.NoError(t, err)
	require.InDelta(t, 0.0, rec.DailyUsageUSD, 1e-9)
	require.InDelta(t, 20.0, rec.WeeklyUsageUSD, 1e-9, "weekly unchanged")
	require.InDelta(t, 50.0, rec.MonthlyUsageUSD, 1e-9, "monthly unchanged")
}

func TestUserPlatformQuotaRepository_ResetExpiredWindowBySubject_NotFoundSentinel(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUserPlatformQuotaRepository(client)

	err := repo.ResetExpiredWindowBySubject(ctx, 9_999_999, "anthropic", "daily", time.Now())
	require.True(t, errors.Is(err, ErrUserPlatformQuotaNotFound),
		"expected ErrUserPlatformQuotaNotFound, got %v", err)
}

// TestBatchSnapshotUsageBySubject_InsertOverwrite 验证 subject 维度绝对值覆盖语义。
func TestBatchSnapshotUsageBySubject_InsertOverwrite(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	u1 := mustCreateUserForQuota(t, client)
	u2 := mustCreateUserForQuota(t, client)
	s1 := mustCreatePersonalSubject(t, client, ctx, u1)
	s2 := mustCreatePersonalSubject(t, client, ctx, u2)

	repo := NewUserPlatformQuotaRepository(client)

	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	dailyStart := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	weeklyStart := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)
	monthlyStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	first := []UserPlatformQuotaSnapshot{
		{BillingSubjectID: s1, Platform: "anthropic", DailyUsageUSD: 1.0, WeeklyUsageUSD: 3.0, MonthlyUsageUSD: 5.0, DailyWindowStart: dailyStart, WeeklyWindowStart: weeklyStart, MonthlyWindowStart: monthlyStart},
		{BillingSubjectID: s2, Platform: "openai", DailyUsageUSD: 2.0, WeeklyUsageUSD: 4.0, MonthlyUsageUSD: 6.0, DailyWindowStart: dailyStart, WeeklyWindowStart: weeklyStart, MonthlyWindowStart: monthlyStart},
	}
	require.NoError(t, repo.BatchSnapshotUsageBySubject(ctx, first, now))

	rec1, err := repo.GetBySubjectPlatform(ctx, s1, "anthropic")
	require.NoError(t, err)
	require.NotNil(t, rec1)
	require.InDelta(t, 1.0, rec1.DailyUsageUSD, 1e-9)

	// 第二批对同一 key 传不同值 → 绝对覆盖，非累加。
	now2 := now.Add(5 * time.Minute)
	second := []UserPlatformQuotaSnapshot{
		{BillingSubjectID: s1, Platform: "anthropic", DailyUsageUSD: 9.9, WeeklyUsageUSD: 19.9, MonthlyUsageUSD: 29.9, DailyWindowStart: dailyStart, WeeklyWindowStart: weeklyStart, MonthlyWindowStart: monthlyStart},
	}
	require.NoError(t, repo.BatchSnapshotUsageBySubject(ctx, second, now2))

	rec1After, err := repo.GetBySubjectPlatform(ctx, s1, "anthropic")
	require.NoError(t, err)
	require.InDelta(t, 9.9, rec1After.DailyUsageUSD, 1e-9, "must overwrite, not accumulate")
	require.InDelta(t, 19.9, rec1After.WeeklyUsageUSD, 1e-9)
	require.InDelta(t, 29.9, rec1After.MonthlyUsageUSD, 1e-9)
}
