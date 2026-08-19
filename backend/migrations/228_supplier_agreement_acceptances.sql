-- APEXONE-EXT: 双边市场——供给者协议的同意记录。
--
-- 平台在收下一个陌生人的上游订阅凭证之前，要求他先同意一份写明了权利义务的协议。
-- 这张表就是那件事的**证据**：谁、在什么时候、同意了哪一个版本、从哪个 IP。
--
-- 为什么单起一张表，而不是在 users 上加一个布尔或者往 extra 里塞一个时间戳：
--
--   1. **版本是一等公民**。协议改一次，之前的同意就不再覆盖新条款；一个布尔答不了
--      「他同意的是哪一版」。而这恰恰是出纠纷时唯一要问的问题。
--   2. **同意记录只增不改**。一行代表一次真实发生过的点击，改写它等于伪造证据。
--      追加式的表天然守住这一点，users 上的一个字段守不住。
--   3. **它比账号活得久**。供给者解绑了所有号、甚至注销了账户，"他当时同意过"
--      这件事仍然要留着——那段时间里产生的每一笔分成都是按这份协议算的。
--
-- 因此这张表**不跟着用户级联删除**。users 的销号本来就是软删（外键上的
-- ON DELETE CASCADE 一次也不会触发），这里索性连外键都不加：一条指向已注销用户的
-- 同意记录不是脏数据，它就是那份证据本身。

CREATE TABLE IF NOT EXISTS supplier_agreement_acceptances (
    id           BIGSERIAL PRIMARY KEY,

    -- 同意人。刻意不加外键：见表头第 3 点，这行记录要比 users 行的可用性活得久。
    user_id      BIGINT NOT NULL,

    -- 他同意的那一版协议号，取自 settings 的 supply_agreement_settings.version。
    version      VARCHAR(64) NOT NULL,

    -- 同意时刻。这是这张表存在的理由，NOT NULL。
    accepted_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- 取证字段。两个都可空——反向代理没配好时拿不到真实 IP，那不该让同意本身失败。
    -- 存下来是为了在「这不是我点的」这类争议里有话可说，不做任何程序判断。
    ip           VARCHAR(64) NULL,
    user_agent   VARCHAR(512) NULL,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE supplier_agreement_acceptances IS '供给者协议同意记录（双边市场，追加式证据）';
COMMENT ON COLUMN supplier_agreement_acceptances.version IS '同意的协议版本，来自 supply_agreement_settings.version';
COMMENT ON COLUMN supplier_agreement_acceptances.ip IS '同意时的客户端 IP，仅作证据，不参与判断';

-- 同一个人对同一个版本只留一行。
--
-- 幂等靠它兜底而不是靠应用层先查后插：接入页上重复点一次「我已阅读并同意」是很
-- 正常的操作，先查后插在并发下会插出两行，而两行同意记录在争议里是个需要解释的
-- 噪音。写入走 ON CONFLICT DO NOTHING——**保留最早的那一次**，因为那才是他真正
-- 做出决定的时刻。
CREATE UNIQUE INDEX IF NOT EXISTS idx_supplier_agreement_user_version_uniq
    ON supplier_agreement_acceptances (user_id, version);

-- 「这个人最近同意过什么」——供给者页面上要显示他同意的版本与时间。
CREATE INDEX IF NOT EXISTS idx_supplier_agreement_user_accepted
    ON supplier_agreement_acceptances (user_id, accepted_at DESC);
