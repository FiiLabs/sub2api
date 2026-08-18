-- APEXONE-EXT: 双边市场——供给账号归属。
--
-- NULL = 管理员自建/自营账号（上游原有语义，全部存量行保持 NULL，向后兼容）；
-- 非 NULL = 用户自助提交的供给账号，该 user 即结算分成对象。
--
-- ON DELETE SET NULL：供给者销号时账号退回「自营」语义而不是被级联删除。
-- 账号里握着的是上游 OAuth 凭证，误删会让在途请求全挂；退回自营由运营接手处置
-- 更安全，也符合「关掉供给池即可一键退回纯自营网关」的退出设计。
ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS owner_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL;

COMMENT ON COLUMN accounts.owner_user_id IS
    'Supplier who owns this account (NULL = admin-created / first-party account)';
