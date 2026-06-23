-- platform-quota (1/4): 把平台限额的存储身份从 user 过渡到 billing_subject 的数据层准备。
--   1) user_id 改可空：团队 quota 行无单一 user（user_id = NULL, billing_subject_id = 团队主体）。
--   2) 幂等回填 billing_subject_id：每行 -> 该 user 的个人计费主体。
--      迁移 150 已做过同口径回填；此处为 150 之后新建行的安全网，IS NULL 守卫保证可安全重跑。
-- 旧唯一索引 (user_id, platform) 暂留：Postgres 唯一索引允许多 NULL，团队行 user_id=NULL 不冲突；
-- subject 维度唯一索引在 153 _notx 支在线创建。

ALTER TABLE user_platform_quotas ALTER COLUMN user_id DROP NOT NULL;

UPDATE user_platform_quotas q
SET billing_subject_id = bs.id
FROM billing_subjects bs
WHERE bs.type = 'user'
  AND bs.user_id = q.user_id
  AND bs.deleted_at IS NULL
  AND q.billing_subject_id IS NULL
  AND q.deleted_at IS NULL;
