//go:build integration

// Package repository — integration round-trip test for the usage_log write path.
//
// Verifies that billing_subject_id, team_id, and actor_user_id are persisted by
// the REAL Create (createSingle) path and can be read back from the database.
//
// RED: fails today because the INSERT omits these three columns (they are always
// written NULL even when the service.UsageLog has them set).
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

// TestCreateSingle_PersistsBillingSubjectFields is an integration round-trip test:
//  1. Create a UsageLog via the real repo.Create path (createSingle).
//  2. SELECT the row back from the DB.
//  3. Assert billing_subject_id / team_id / actor_user_id are NOT NULL and equal
//     to the values that were passed in.
func TestCreateSingle_PersistsBillingSubjectFields(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)

	// Create the required FK rows.
	user := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("billing-write-%s@test.com", uuid.NewString()),
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-billing-write-" + uuid.NewString(),
		Name:   "k",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "acc-billing-write-" + uuid.NewString(),
	})

	// Create a team whose billing_subject row we can use as billing_subject_id.
	_, teamSubjID := mustCreateTeamWithBalance(t, client, ctx, user.ID, 0)

	// Build the repo pointing at the shared integrationDB (same as testEntClient).
	repo := newUsageLogRepositoryWithSQL(client, integrationDB)

	teamID := int64(teamSubjID) // reuse team's billing_subject as both team_id and billing_subject_id
	actorID := user.ID

	log := &service.UsageLog{
		UserID:           user.ID,
		APIKeyID:         apiKey.ID,
		AccountID:        account.ID,
		RequestID:        uuid.NewString(),
		Model:            "claude-opus-4-5",
		BillingSubjectID: teamSubjID,
		TeamID:           &teamID,
		ActorUserID:      &actorID,
		InputTokens:      10,
		OutputTokens:     20,
		TotalCost:        0.5,
		ActualCost:       0.4,
		CreatedAt:        time.Now().UTC(),
	}

	inserted, err := repo.Create(ctx, log)
	require.NoError(t, err, "Create must not error")
	require.True(t, inserted, "row must be newly inserted")
	require.NotZero(t, log.ID, "log.ID must be set after Create")

	// SELECT the raw columns back from the DB.
	var gotBillingSubjectID, gotTeamID, gotActorUserID *int64
	err = integrationDB.QueryRowContext(ctx,
		`SELECT billing_subject_id, team_id, actor_user_id
		   FROM usage_logs
		  WHERE id = $1`,
		log.ID,
	).Scan(&gotBillingSubjectID, &gotTeamID, &gotActorUserID)
	require.NoError(t, err, "SELECT after Create must succeed")

	require.NotNil(t, gotBillingSubjectID, "billing_subject_id must be NOT NULL after Create")
	require.Equal(t, teamSubjID, *gotBillingSubjectID, "billing_subject_id must equal input value")

	require.NotNil(t, gotTeamID, "team_id must be NOT NULL after Create")
	require.Equal(t, teamID, *gotTeamID, "team_id must equal input value")

	require.NotNil(t, gotActorUserID, "actor_user_id must be NOT NULL after Create")
	require.Equal(t, actorID, *gotActorUserID, "actor_user_id must equal input value")
}
