-- 消费侧迁移 cutover 前置：把个人用户的钱包余额从 users.balance 重新同步到其个人 billing_subject。
-- migration 150 在建主体时按当时 users.balance 做过 seed；此后消费/充值仍走 users.balance，
-- 故 seed 值可能已陈旧。本迁移在「消费侧切到 subject」同版本发布时重新对齐，确保切换后个人主体
-- 余额 == 用户当前余额。幂等、可重复执行。团队主体不在此列（无对应 user，余额由充值进账填充）。
-- 数据前提：生产无在用的 subject 余额数据，故此处直接覆盖写是安全的（不会冲掉真实主体余额）。
UPDATE billing_subjects bs
SET balance = u.balance,
    total_recharged = u.total_recharged,
    updated_at = NOW()
FROM users u
WHERE bs.type = 'user'
  AND bs.user_id = u.id
  AND bs.deleted_at IS NULL
  AND u.deleted_at IS NULL;
