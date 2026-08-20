-- APEXONE-EXT: 双边市场——供给账号的接入来源。
--
-- 一个供给账号建出来时，记一行「它是从哪个网络挂上来的」。
--
-- # 为什么需要这张表
--
-- 平台对「一个人能挂多少个号」有上限（supply_onboarding_settings），但那道闸只按
-- user_id 数。注册一个新用户是免费的：想绕开每人上限，注册第二个账号即可。真正
-- 稀缺的、攻击者绕不开的维度是**出口网络**，而 accounts 表上没有任何地方记着
-- 「这个号是从哪挂上来的」——owner_user_id 只回答「归谁」，不回答「从哪来」。
--
-- supplier_oauth_sessions（226）也答不了：那张表按 15 分钟过期，未消费的行会被清掉，
-- 已消费的行留着但它记的是**会话**不是**账号**，而且当时也没记 IP。
--
-- # 为什么是独立一张表而不是 accounts.extra 里的一个键
--
--   1. **它要能被 COUNT**。按 IP 数号是一次带 WHERE 的聚合，jsonb 键上做这件事要建
--      表达式索引，而这张表一个普通 B-tree 索引就够。
--   2. **它是取证材料，不是账号属性**。extra 里的东西会被接入流程、观察期任务、
--      管理端反复读写；来源 IP 写一次之后再不该被任何流程改动。分开放，让「谁能改它」
--      变成一个一眼能看完的清单（只有 CompleteOAuth 的那条 INSERT）。
--   3. **它比账号活得久**。号被解绑（软删）之后这一行留着——出纠纷时要能回答
--      「这批号当初是不是同一个人挂的」，那时 accounts 行已经被抹掉凭证了。
--
-- # 刻意没有外键
--
-- 与 228（协议同意记录）同样的理由：这行记录要比它指向的行的**可用性**活得久。
-- 加 ON DELETE CASCADE 也没用——本仓的账号删除是软删，级联一次也不会触发；
-- 加了只会让人以为清理是自动的。数号的那条查询自己 JOIN accounts 并过滤
-- deleted_at，「已解绑的号不再占额度」这条规则写在查询里，不写在外键上。

CREATE TABLE IF NOT EXISTS supplier_account_origins (
    -- 账号 id 直接做主键：一个账号只可能被接入一次，重复插入是幂等的
    -- （ON CONFLICT DO NOTHING），保留第一次那行。
    account_id  BIGINT PRIMARY KEY,

    -- 接入时的归属人。数每人上限不读这一列（那条查 accounts.owner_user_id，
    -- 那才是当下的归属），这里存的是**当时**是谁——归属若被运营改动过，
    -- 两者会不一致，而不一致本身正是要留证的事。
    user_id     BIGINT NOT NULL,

    -- 接入时的客户端 IP。反向代理没配好时可能拿不到真实值，那种情况下这一行
    -- 根本不会被写入（见 service 侧：空 IP 跳过记录，也跳过按 IP 的限额判断），
    -- 所以这一列是 NOT NULL——库里不存在「来源未知」的行。
    client_ip   VARCHAR(64) NOT NULL,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE supplier_account_origins IS '供给账号的接入来源（双边市场，每 IP 限额与取证）';
COMMENT ON COLUMN supplier_account_origins.client_ip IS '接入时客户端 IP，用于每 IP 账号数上限';
COMMENT ON COLUMN supplier_account_origins.user_id IS '接入时的归属人；当下归属以 accounts.owner_user_id 为准';

-- 「这个网络挂了几个号」——每 IP 限额那条 COUNT 走它。
CREATE INDEX IF NOT EXISTS idx_supplier_account_origins_ip
    ON supplier_account_origins (client_ip);

-- 「这个人是从哪些网络挂的号」——只给人看（排查刷号时按人回溯），不参与判断。
CREATE INDEX IF NOT EXISTS idx_supplier_account_origins_user
    ON supplier_account_origins (user_id, created_at DESC);
