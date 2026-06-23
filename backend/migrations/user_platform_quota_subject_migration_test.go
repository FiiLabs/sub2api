package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUserPlatformQuotaSubjectMigrationContainsRequiredDDL 校验 platform-quota (1/4)
// 的两支迁移文件内容：152 事务支（user_id 可空 + 幂等回填），153 _notx 支（subject 维度唯一索引）。
// 与 team_workspaces_migration_test.go 同风格：只断言 SQL 文本，不依赖真实 DB。
func TestUserPlatformQuotaSubjectMigrationContainsRequiredDDL(t *testing.T) {
	raw152, err := FS.ReadFile("152_user_platform_quota_subject.sql")
	require.NoError(t, err)
	sql152 := string(raw152)

	raw153, err := FS.ReadFile("153_user_platform_quota_subject_unique_notx.sql")
	require.NoError(t, err)
	sql153 := string(raw153)

	// --- 152 事务支：user_id 可空 + 幂等回填个人主体 ---
	required152 := []string{
		"ALTER TABLE user_platform_quotas ALTER COLUMN user_id DROP NOT NULL",
		"UPDATE user_platform_quotas",
		"FROM billing_subjects",
		"bs.type = 'user'",
		"bs.user_id = ",
		// 幂等守卫：仅回填尚未有 subject 的活跃行（与迁移 150 的回填保持同口径，可安全重跑）
		"billing_subject_id IS NULL",
		"deleted_at IS NULL",
	}
	for _, fragment := range required152 {
		require.Contains(t, sql152, fragment, "152 缺少片段: %s", fragment)
	}

	// --- 153 _notx 支：subject × platform 部分唯一索引（CONCURRENTLY）---
	required153 := []string{
		"CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_user_platform_quotas_subject_platform_unique",
		"ON user_platform_quotas(billing_subject_id, platform)",
		"WHERE billing_subject_id IS NOT NULL AND deleted_at IS NULL",
	}
	for _, fragment := range required153 {
		require.Contains(t, sql153, fragment, "153 缺少片段: %s", fragment)
	}

	// --- 执行模式约束（与 migrations_runner.validateMigrationExecutionMode 对齐）---
	// 事务支 152 不得含 CONCURRENTLY（否则会被 runner 拒绝）。
	require.NotContains(t, strings.ToUpper(sql152), "CONCURRENTLY",
		"152 事务支不得含 CONCURRENTLY，应放入 _notx 支")
	// _notx 支 153 不得含事务控制语句，也不得混入 DDL/DML。
	upper153 := strings.ToUpper(sql153)
	for _, banned := range []string{"BEGIN", "COMMIT", "ROLLBACK", "ALTER TABLE", "CREATE TABLE", "INSERT", "UPDATE "} {
		require.NotContains(t, upper153, banned, "153 _notx 支不得含: %s", banned)
	}

	// --- 安全：两支均不得破坏性 DDL ---
	normalized := strings.ToUpper(sql152 + "\n" + sql153)
	require.NotContains(t, normalized, "DROP TABLE")
	require.NotContains(t, normalized, "DROP COLUMN")
	require.NotContains(t, normalized, "DROP CONSTRAINT")
}
