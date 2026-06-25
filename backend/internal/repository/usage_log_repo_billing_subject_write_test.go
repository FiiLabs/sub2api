// Tests that the write path persists billing_subject_id, team_id, actor_user_id.
// These are unit tests (no build tag = always run) that mirror the sqlmock pattern
// in usage_log_repo_request_type_test.go.
package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestPrepareUsageLogInsert_IncludesBillingSubjectFields asserts that
// prepareUsageLogInsert appends billing_subject_id/team_id/actor_user_id at
// the end of the args slice (before created_at), and that usageLogInsertArgTypes
// has matching entries.
//
// RED: fails today because prepareUsageLogInsert omits these three fields.
func TestPrepareUsageLogInsert_IncludesBillingSubjectFields(t *testing.T) {
	teamID := int64(42)
	actorID := int64(99)

	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:           1,
		APIKeyID:         2,
		AccountID:        3,
		RequestID:        "req-billing-subject",
		Model:            "gpt-5",
		BillingSubjectID: 7,
		TeamID:           &teamID,
		ActorUserID:      &actorID,
		CreatedAt:        time.Date(2025, 6, 25, 0, 0, 0, 0, time.UTC),
	})

	// args count must match types count
	require.Len(t, prepared.args, len(usageLogInsertArgTypes),
		"arg count must equal usageLogInsertArgTypes length")

	// The 3 new columns are appended after account_stats_cost and before created_at.
	// Layout (from end): [..., account_stats_cost, billing_subject_id, team_id, actor_user_id, created_at]
	n := len(prepared.args)

	// created_at is last
	_, isTime := prepared.args[n-1].(time.Time)
	require.True(t, isTime, "last arg must be created_at (time.Time)")

	// billing_subject_id: sql.NullInt64{Int64:7, Valid:true}
	billingSubjectArg, ok := prepared.args[n-4].(sql.NullInt64)
	require.True(t, ok, "args[n-4] must be sql.NullInt64 (billing_subject_id)")
	require.True(t, billingSubjectArg.Valid, "billing_subject_id must be valid (non-zero)")
	require.Equal(t, int64(7), billingSubjectArg.Int64)

	// team_id: sql.NullInt64{Int64:42, Valid:true}
	teamIDArg, ok := prepared.args[n-3].(sql.NullInt64)
	require.True(t, ok, "args[n-3] must be sql.NullInt64 (team_id)")
	require.True(t, teamIDArg.Valid, "team_id must be valid")
	require.Equal(t, int64(42), teamIDArg.Int64)

	// actor_user_id: sql.NullInt64{Int64:99, Valid:true}
	actorIDArg, ok := prepared.args[n-2].(sql.NullInt64)
	require.True(t, ok, "args[n-2] must be sql.NullInt64 (actor_user_id)")
	require.True(t, actorIDArg.Valid, "actor_user_id must be valid")
	require.Equal(t, int64(99), actorIDArg.Int64)
}

// TestPrepareUsageLogInsert_ZeroBillingSubjectIsNull asserts that a zero
// BillingSubjectID produces an invalid (NULL) NullInt64, and nil pointers
// also produce invalid NullInt64 values.
func TestPrepareUsageLogInsert_ZeroBillingSubjectIsNull(t *testing.T) {
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:           1,
		APIKeyID:         2,
		AccountID:        3,
		RequestID:        "req-zero-billing-subject",
		Model:            "gpt-5",
		BillingSubjectID: 0, // zero → NULL
		TeamID:           nil,
		ActorUserID:      nil,
		CreatedAt:        time.Date(2025, 6, 25, 0, 0, 0, 0, time.UTC),
	})

	n := len(prepared.args)

	billingSubjectArg, ok := prepared.args[n-4].(sql.NullInt64)
	require.True(t, ok, "args[n-4] must be sql.NullInt64 (billing_subject_id)")
	require.False(t, billingSubjectArg.Valid, "zero BillingSubjectID must be NULL")

	teamIDArg, ok := prepared.args[n-3].(sql.NullInt64)
	require.True(t, ok, "args[n-3] must be sql.NullInt64 (team_id)")
	require.False(t, teamIDArg.Valid, "nil TeamID must be NULL")

	actorIDArg, ok := prepared.args[n-2].(sql.NullInt64)
	require.True(t, ok, "args[n-2] must be sql.NullInt64 (actor_user_id)")
	require.False(t, actorIDArg.Valid, "nil ActorUserID must be NULL")
}

// TestCreateSingle_QueryContainsBillingSubjectColumns asserts that the INSERT
// query sent by createSingle includes billing_subject_id, team_id, actor_user_id
// both in the column list and as bound args.
//
// RED: fails today because the INSERT column list omits these three columns.
func TestCreateSingle_QueryContainsBillingSubjectColumns(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	teamID := int64(55)
	actorID := int64(77)
	createdAt := time.Date(2025, 6, 25, 12, 0, 0, 0, time.UTC)

	log := &service.UsageLog{
		UserID:           10,
		APIKeyID:         20,
		AccountID:        30,
		RequestID:        "req-billing-subject-insert",
		Model:            "claude-opus-4-5",
		BillingSubjectID: 5,
		TeamID:           &teamID,
		ActorUserID:      &actorID,
		CreatedAt:        createdAt,
	}

	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(
			log.UserID,
			log.APIKeyID,
			log.AccountID,
			log.RequestID,
			log.Model,
			sqlmock.AnyArg(), // requested_model
			sqlmock.AnyArg(), // upstream_model
			sqlmock.AnyArg(), // group_id
			sqlmock.AnyArg(), // subscription_id
			log.InputTokens,
			log.OutputTokens,
			log.CacheCreationTokens,
			log.CacheReadTokens,
			log.CacheCreation5mTokens,
			log.CacheCreation1hTokens,
			log.ImageOutputTokens,
			log.ImageOutputCost,
			log.InputCost,
			log.OutputCost,
			log.CacheCreationCost,
			log.CacheReadCost,
			log.TotalCost,
			log.ActualCost,
			log.RateMultiplier,
			log.AccountRateMultiplier,
			log.BillingType,
			sqlmock.AnyArg(), // request_type (normalized)
			log.Stream,
			log.OpenAIWSMode,
			sqlmock.AnyArg(), // duration_ms
			sqlmock.AnyArg(), // first_token_ms
			sqlmock.AnyArg(), // user_agent
			sqlmock.AnyArg(), // ip_address
			log.ImageCount,
			sqlmock.AnyArg(), // image_size
			sqlmock.AnyArg(), // image_input_size
			sqlmock.AnyArg(), // image_output_size
			sqlmock.AnyArg(), // image_size_source
			sqlmock.AnyArg(), // image_size_breakdown
			sqlmock.AnyArg(), // service_tier
			sqlmock.AnyArg(), // reasoning_effort
			sqlmock.AnyArg(), // inbound_endpoint
			sqlmock.AnyArg(), // upstream_endpoint
			log.CacheTTLOverridden,
			sqlmock.AnyArg(), // channel_id
			sqlmock.AnyArg(), // model_mapping_chain
			sqlmock.AnyArg(), // billing_tier
			sqlmock.AnyArg(), // billing_mode
			sqlmock.AnyArg(), // account_stats_cost
			// The 3 new columns appended at end (before created_at):
			sql.NullInt64{Int64: 5, Valid: true},  // billing_subject_id
			sql.NullInt64{Int64: 55, Valid: true}, // team_id
			sql.NullInt64{Int64: 77, Valid: true}, // actor_user_id
			createdAt,
		).
		WillReturnRows(newBillingSubjectMockRows(createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestBuildUsageLogBestEffortInsertQuery_ContainsBillingSubjectColumns asserts
// that the best-effort INSERT query string contains the three column names in
// both the CTE column-alias list and the INSERT column list.
func TestBuildUsageLogBestEffortInsertQuery_ContainsBillingSubjectColumns(t *testing.T) {
	teamID := int64(10)
	actorID := int64(20)
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:           1,
		APIKeyID:         2,
		AccountID:        3,
		RequestID:        "req-best-effort-billing",
		Model:            "gpt-5",
		BillingSubjectID: 9,
		TeamID:           &teamID,
		ActorUserID:      &actorID,
		CreatedAt:        time.Date(2025, 6, 25, 12, 0, 0, 0, time.UTC),
	})

	query, args := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})

	require.Contains(t, query, "billing_subject_id", "best-effort CTE must mention billing_subject_id")
	require.Contains(t, query, "team_id", "best-effort CTE must mention team_id")
	require.Contains(t, query, "actor_user_id", "best-effort CTE must mention actor_user_id")
	require.Contains(t, query, "INSERT INTO usage_logs (")

	// arg count must still equal len(prepared.args)
	require.Len(t, args, len(prepared.args))
}

// TestBuildUsageLogBatchInsertQuery_ContainsBillingSubjectColumns asserts that
// the batched INSERT query also carries the three columns.
func TestBuildUsageLogBatchInsertQuery_ContainsBillingSubjectColumns(t *testing.T) {
	teamID := int64(11)
	actorID := int64(22)
	log := &service.UsageLog{
		UserID:           1,
		APIKeyID:         2,
		AccountID:        3,
		RequestID:        "req-batch-billing",
		Model:            "gpt-5",
		BillingSubjectID: 8,
		TeamID:           &teamID,
		ActorUserID:      &actorID,
		CreatedAt:        time.Now().UTC(),
	}
	prepared := prepareUsageLogInsert(log)

	query, args := buildUsageLogBatchInsertQuery(
		[]string{usageLogBatchKey(log.RequestID, log.APIKeyID)},
		map[string]usageLogInsertPrepared{usageLogBatchKey(log.RequestID, log.APIKeyID): prepared},
	)

	require.Contains(t, query, "billing_subject_id", "batch CTE must mention billing_subject_id")
	require.Contains(t, query, "team_id", "batch CTE must mention team_id")
	require.Contains(t, query, "actor_user_id", "batch CTE must mention actor_user_id")
	// +1 for input_idx which is injected by the batch builder
	require.Equal(t, len(prepared.args)+1, len(args))
}

// TestExecUsageLogInsertNoResult_QueryContainsBillingSubjectColumns asserts
// that execUsageLogInsertNoResult sends the 3 columns via the prepared args.
func TestExecUsageLogInsertNoResult_QueryContainsBillingSubjectColumns(t *testing.T) {
	db, mock := newSQLMock(t)

	teamID := int64(33)
	actorID := int64(44)
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:           1,
		APIKeyID:         2,
		AccountID:        3,
		RequestID:        "req-no-result-billing",
		Model:            "gpt-5",
		BillingSubjectID: 6,
		TeamID:           &teamID,
		ActorUserID:      &actorID,
		CreatedAt:        time.Date(2025, 6, 25, 12, 0, 0, 0, time.UTC),
	})

	mock.ExpectExec("INSERT INTO usage_logs").
		WithArgs(anySliceToDriverValues(prepared.args)...).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := execUsageLogInsertNoResult(context.Background(), db, prepared)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// newBillingSubjectMockRows is a helper used only in this file.
func newBillingSubjectMockRows(createdAt time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(1), createdAt)
}

// Ensure driver import is used (anyArgMatcher implements driver.Valuer check).
var _ driver.Value = nil
