-- APEXONE-EXT: 双边市场——供给者自助授权的待定会话。
--
-- 为什么不能直接复用上游那个内存 SessionStore（oauth.NewSessionStore）：
--
--   1. **没有归属人**。内存会话只有 state/code_verifier，任何人拿到 session_id 都能
--      来兑换。管理端只有管理员能调，问题不大；供给者接入是面向普通用户的公开入口，
--      「这个会话属于谁」必须是服务端记着的事实，不能靠调用方自称。
--   2. **多实例必挂**。授权链接在实例 A 生成、回调打到实例 B，内存里查不到就是
--      「session not found」。管理员碰上可以重试，供给者接入走负载均衡，是必然故障。
--   3. **重启即丢**。发版正好卡在用户授权的十分钟窗口里，这批人全部失败。
--
-- 形态参照上游已有的 pending_auth_sessions（登录侧的持久化待定会话），不另发明。
--
-- 关于 code_verifier 明文存储：PKCE verifier 单独没有用，必须配上一次性的授权码才能
-- 换 token，且本表行十分钟过期、一次性消费。它与内存方案的暴露面差别是「进程内存」
-- 换成「数据库」，在本仓凭证本来就明文存 jsonb 的现状下不是新增的短板。真要收紧应当
-- 连同 accounts.credentials 一起做应用层加密，那是独立的一刀。

CREATE TABLE IF NOT EXISTS supplier_oauth_sessions (
    id            BIGSERIAL PRIMARY KEY,

    -- 交给前端的不透明句柄。回调时凭它 + 登录态找回本行。
    session_id    VARCHAR(128) NOT NULL,

    -- 归属人。会话属于谁是服务端记下的，兑换时用登录态比对，防止「拿别人的
    -- session_id 把账号挂到自己名下」。
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    platform      VARCHAR(32) NOT NULL,

    -- PKCE 三件套 + scope。scope 决定兑换时按不按 setup-token 走。
    state         VARCHAR(255) NOT NULL,
    code_verifier VARCHAR(255) NOT NULL,
    scope         VARCHAR(255) NOT NULL,

    -- 一次性消费标记。兑换用一条 UPDATE ... WHERE consumed_at IS NULL 原子领取，
    -- 并发重放里只有一个能拿到，避免同一个授权码被换两次、建出两个账号。
    consumed_at   TIMESTAMPTZ NULL,

    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE supplier_oauth_sessions IS '供给者自助 OAuth 待定会话（双边市场）';
COMMENT ON COLUMN supplier_oauth_sessions.session_id IS '交给前端的不透明句柄';
COMMENT ON COLUMN supplier_oauth_sessions.user_id IS '会话归属人，兑换时必须与登录态一致';
COMMENT ON COLUMN supplier_oauth_sessions.consumed_at IS '非 NULL = 已兑换，一次性';

CREATE UNIQUE INDEX IF NOT EXISTS idx_supplier_oauth_sessions_session_id
    ON supplier_oauth_sessions (session_id);

-- 「这个人最近发起了几次授权」——限流与清理都按这个维度查。
CREATE INDEX IF NOT EXISTS idx_supplier_oauth_sessions_user_created
    ON supplier_oauth_sessions (user_id, created_at DESC);

-- 过期清理只扫还没被消费的行。
CREATE INDEX IF NOT EXISTS idx_supplier_oauth_sessions_expires
    ON supplier_oauth_sessions (expires_at)
    WHERE consumed_at IS NULL;
