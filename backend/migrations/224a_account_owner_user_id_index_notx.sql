-- APEXONE-EXT: 供给者仪表盘按 owner 列自己的账号。
-- 部分索引：绝大多数行是自营账号（owner_user_id IS NULL），只索引供给账号可以把索引
-- 压到供给规模而不是账号总量。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owner_user_id
    ON accounts (owner_user_id)
    WHERE owner_user_id IS NOT NULL;
