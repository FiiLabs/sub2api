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

func TestUsageBillingApply_DeductsSubjectBalanceWhenScoped(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("subj-bill-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})

	// 用 helper 建合法 team billing_subject（满足 owner_check 约束），余额 50
	// 该测试只需 subjectID，不需要成员关系
	_, teamSubjID := mustCreateTeamWithBalance(t, client, ctx, user.ID, 50)

	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID, Key: "sk-subj-" + uuid.NewString(), Name: "k", Quota: 0,
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "subj-acct-" + uuid.NewString(), Type: service.AccountTypeAPIKey,
	})

	cmd := &service.UsageBillingCommand{
		RequestID:        uuid.NewString(),
		APIKeyID:         apiKey.ID,
		UserID:           user.ID,
		BalanceSubjectID: teamSubjID,
		AccountID:        account.ID,
		AccountType:      service.AccountTypeAPIKey,
		BalanceCost:      12.5,
	}
	result, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.NotNil(t, result.NewBalance)
	require.InDelta(t, 37.5, *result.NewBalance, 0.000001)

	// 团队主体被扣，user 余额不动。
	var subjBal, userBal float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM billing_subjects WHERE id=$1", teamSubjID).Scan(&subjBal))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&userBal))
	require.InDelta(t, 37.5, subjBal, 0.000001)
	require.InDelta(t, 100, userBal, 0.000001)
}
