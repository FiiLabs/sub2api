-- platform-quota (1/4): subject x platform 部分唯一索引（在线建，不锁表）。
-- 个人主体回填后 (billing_subject_id, platform) 无冲突；团队主体未来多成员共享同一行。
-- 历史 151 已建同列非唯一索引（不同索引名），本支补唯一约束，两者可共存。
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_user_platform_quotas_subject_platform_unique
  ON user_platform_quotas(billing_subject_id, platform)
  WHERE billing_subject_id IS NOT NULL AND deleted_at IS NULL;
