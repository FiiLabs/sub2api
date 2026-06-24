//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMigration154_ResyncsPersonalSubjectBalance(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	// 个人用户余额 = 100，其个人主体被故意置为陈旧值 1。
	// mustCreateUser 不支持 TotalRecharged，故仅设置 Balance；users.total_recharged 默认为 0。
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("resync-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	_, err := client.BillingSubject.Create().
		SetType(domain.BillingSubjectTypeUser).
		SetUserID(user.ID).
		SetStatus(service.StatusActive).
		SetBalance(1).
		SetTotalRecharged(1).
		SetConcurrency(5).
		Save(ctx)
	require.NoError(t, err)

	// 直接执行迁移 SQL（与发布时一致）。
	sqlBytes, err := os.ReadFile(filepath.Join("..", "..", "migrations", "154_resync_personal_subject_balance.sql"))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, string(sqlBytes))
	require.NoError(t, err)

	var balance, recharged float64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT balance, total_recharged FROM billing_subjects WHERE type='user' AND user_id=$1 AND deleted_at IS NULL",
		user.ID).Scan(&balance, &recharged))
	// balance 应从 users.balance=100 同步过来（原陈旧值 1）。
	require.InDelta(t, 100, balance, 0.000001)
	// total_recharged 应从 users.total_recharged=0 同步过来（users 默认值，原陈旧值 1）。
	require.InDelta(t, 0, recharged, 0.000001)
}
