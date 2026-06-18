package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayServiceRecordUsage_BillsTeamBillingSubject(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	quotaSvc := &openAIRecordUsageAPIKeyQuotaStub{}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, userRepo, subRepo, nil)

	teamID := int64(777)
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "resp_team_subject",
			Usage:     OpenAIUsage{},
			Model:     "gpt-5.1",
			Duration:  time.Second,
		},
		APIKey: &APIKey{
			ID:               1000,
			Quota:            100,
			Group:            &Group{RateMultiplier: 1},
			BillingSubjectID: 900,
			TeamID:           &teamID,
		},
		User:          &User{ID: 2000},
		Account:       &Account{ID: 3000, Type: AccountTypeAPIKey},
		APIKeyService: quotaSvc,
	})

	require.NoError(t, err)

	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, int64(900), usageRepo.lastLog.BillingSubjectID)
	require.NotNil(t, usageRepo.lastLog.ActorUserID)
	require.Equal(t, int64(2000), *usageRepo.lastLog.ActorUserID)
	require.NotNil(t, usageRepo.lastLog.TeamID)
	require.Equal(t, teamID, *usageRepo.lastLog.TeamID)

	require.NotNil(t, billingRepo.lastCmd)
	require.Equal(t, int64(900), billingRepo.lastCmd.BillingSubjectID)
}

func TestOpenAIGatewayServiceRecordUsage_BillsUserWhenNoBillingSubject(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	quotaSvc := &openAIRecordUsageAPIKeyQuotaStub{}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, userRepo, subRepo, nil)

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "resp_user_subject",
			Usage:     OpenAIUsage{},
			Model:     "gpt-5.1",
			Duration:  time.Second,
		},
		APIKey:        &APIKey{ID: 1001, Quota: 100, Group: &Group{RateMultiplier: 1}},
		User:          &User{ID: 2001},
		Account:       &Account{ID: 3001, Type: AccountTypeAPIKey},
		APIKeyService: quotaSvc,
	})

	require.NoError(t, err)

	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, int64(2001), usageRepo.lastLog.BillingSubjectID)
	require.Nil(t, usageRepo.lastLog.TeamID)

	require.NotNil(t, billingRepo.lastCmd)
	require.Equal(t, int64(2001), billingRepo.lastCmd.BillingSubjectID)
}
