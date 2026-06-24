-- 订阅读侧切到 billing_subject 前置兜底：把仍为 NULL 的个人订阅 billing_subject_id 回填到个人主体。
-- migration 150 已做过一次；此处兜底 150 之后经未设 billing_subject_id 路径创建的个人订阅，
-- 确保切换后个人订阅不丢。幂等、可重复执行。团队订阅创建时已带 billing_subject_id。
UPDATE user_subscriptions us
SET billing_subject_id = bs.id
FROM billing_subjects bs
WHERE bs.type = 'user'
  AND bs.user_id = us.user_id
  AND bs.deleted_at IS NULL
  AND us.billing_subject_id IS NULL;
