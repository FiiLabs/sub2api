package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTeamWorkspaceMigrationContainsRequiredDDL(t *testing.T) {
	raw150, err := FS.ReadFile("150_team_workspaces_billing_subjects.sql")
	require.NoError(t, err)
	sql150 := string(raw150)

	raw151, err := FS.ReadFile("151_team_workspaces_subject_indexes_notx.sql")
	require.NoError(t, err)
	sql151 := string(raw151)

	required150 := []string{
		"CREATE TABLE IF NOT EXISTS billing_subjects",
		"CREATE TABLE IF NOT EXISTS teams",
		"CREATE TABLE IF NOT EXISTS team_members",
		"CREATE TABLE IF NOT EXISTS team_invitations",
		"status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled'))",
		"status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'left'))",
		"status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'expired', 'revoked'))",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_billing_subjects_user_unique",
		"WHERE type = 'user' AND deleted_at IS NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_billing_subjects_team_unique",
		"WHERE type = 'team' AND deleted_at IS NULL",
		"DO $$",
		"FROM pg_constraint",
		"WHERE conname = 'billing_subjects_team_fk'",
		"AND conrelid = 'billing_subjects'::regclass",
		"ALTER TABLE billing_subjects",
		"ADD CONSTRAINT billing_subjects_team_fk",
		"FOREIGN KEY (team_id) REFERENCES teams(id) DEFERRABLE INITIALLY DEFERRED",
		"CREATE OR REPLACE FUNCTION enforce_team_billing_subject_invariant()",
		"bs.type = 'team'",
		"bs.team_id = NEW.id",
		"DROP TRIGGER IF EXISTS teams_billing_subject_invariant ON teams",
		"CREATE TRIGGER teams_billing_subject_invariant",
		"BEFORE INSERT OR UPDATE OF billing_subject_id ON teams",
		"CREATE OR REPLACE FUNCTION prevent_referenced_billing_subject_invalid_state()",
		"TG_OP = 'DELETE'",
		"teams.billing_subject_id = OLD.id",
		"NEW.type <> 'team'",
		"NEW.team_id <> teams.id",
		"NEW.deleted_at IS NOT NULL",
		"DROP TRIGGER IF EXISTS billing_subjects_referenced_invariant ON billing_subjects",
		"CREATE TRIGGER billing_subjects_referenced_invariant",
		"BEFORE UPDATE OF type, team_id, deleted_at OR DELETE ON billing_subjects",
		"ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS billing_subject_id BIGINT",
		"ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS team_id BIGINT",
		"ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS created_by_user_id BIGINT",
		"ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS updated_by_user_id BIGINT",
		"ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS billing_subject_id BIGINT",
		"ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS team_id BIGINT",
		"ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS actor_user_id BIGINT",
		"ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS billing_subject_id BIGINT",
		"ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS team_id BIGINT",
		"ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS created_by_user_id BIGINT",
		"ALTER TABLE user_subscriptions ADD COLUMN IF NOT EXISTS billing_subject_id BIGINT",
		"ALTER TABLE user_platform_quotas ADD COLUMN IF NOT EXISTS billing_subject_id BIGINT",
		"INSERT INTO billing_subjects",
		"UPDATE api_keys ak",
		"UPDATE usage_logs ul",
		"UPDATE payment_orders po",
		"UPDATE user_subscriptions us",
		"UPDATE user_platform_quotas upq",
		"CREATE INDEX IF NOT EXISTS idx_teams_owner_user_id ON teams(owner_user_id)",
		"CREATE INDEX IF NOT EXISTS idx_teams_billing_subject_id ON teams(billing_subject_id)",
	}

	for _, fragment := range required150 {
		require.Contains(t, sql150, fragment)
	}

	require.GreaterOrEqual(t, strings.Count(sql150, "AND bs.deleted_at IS NULL"), 5)

	required151 := []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_api_keys_billing_subject_id ON api_keys(billing_subject_id)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_api_keys_team_id ON api_keys(team_id)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_api_keys_created_by_user_id ON api_keys(created_by_user_id)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_billing_subject_created_at ON usage_logs(billing_subject_id, created_at)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_team_created_at ON usage_logs(team_id, created_at)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_actor_created_at ON usage_logs(actor_user_id, created_at)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payment_orders_billing_subject_id ON payment_orders(billing_subject_id)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payment_orders_team_id ON payment_orders(team_id)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payment_orders_created_by_user_id ON payment_orders(created_by_user_id)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_subscriptions_billing_subject_group ON user_subscriptions(billing_subject_id, group_id)",
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_platform_quotas_billing_subject_platform ON user_platform_quotas(billing_subject_id, platform)",
	}

	for _, fragment := range required151 {
		require.Contains(t, sql151, fragment)
	}

	require.NotContains(t, sql150, "CREATE INDEX IF NOT EXISTS idx_api_keys_billing_subject_id")
	require.NotContains(t, sql150, "CREATE INDEX IF NOT EXISTS idx_usage_logs_billing_subject_created_at")
	require.NotContains(t, sql150, "CREATE INDEX IF NOT EXISTS idx_payment_orders_billing_subject_id")
	require.NotContains(t, sql151, "INSERT INTO")
	require.NotContains(t, sql151, "UPDATE ")
	require.NotContains(t, sql151, "ALTER TABLE")
	require.NotContains(t, sql151, "CREATE TABLE")

	normalizedSQL := strings.ToUpper(sql150 + "\n" + sql151)
	require.False(t, strings.Contains(normalizedSQL, "DROP TABLE"))
	require.False(t, strings.Contains(normalizedSQL, "DROP COLUMN"))
	require.False(t, strings.Contains(normalizedSQL, "DROP CONSTRAINT"))
}
