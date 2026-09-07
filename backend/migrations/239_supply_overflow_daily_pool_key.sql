-- APEXONE-EXT: 双边市场——溢出日计数按平台/池拆分。
--
-- 原表一天一行（day 作主键）。此前只有一个供给池（Claude），够用。现在要同时跑
-- Claude 供给池 + OpenAI 供给池，两池若共用一行：
--   1. 每日溢出预算会被两池共享——OpenAI 的溢出吃掉 Claude 的额度，反之亦然；
--   2. overflow / denied / exhausted 三个计数把两池混在一起，
--      「今天谁的兜底不够用（该加谁的自营号）」这个唯一有用的信号就分不出平台了。
--
-- 加一个 pool_key 列进主键，各池独立记账。pool_key = 账号平台（与 supply_pool_settings
-- 的 pools 键一致：anthropic / openai / …）。存量行属于当年唯一的 anthropic 池，
-- 由列默认值补 'anthropic'，历史计数原样保留在 anthropic 名下。
--
-- 普通事务迁移（非 _notx）：只有列增加与主键重建，无并发索引操作。

ALTER TABLE supply_overflow_daily
    ADD COLUMN IF NOT EXISTS pool_key TEXT NOT NULL DEFAULT 'anthropic';

ALTER TABLE supply_overflow_daily
    DROP CONSTRAINT IF EXISTS supply_overflow_daily_pkey;

ALTER TABLE supply_overflow_daily
    ADD PRIMARY KEY (day, pool_key);

COMMENT ON COLUMN supply_overflow_daily.pool_key IS
    '供给池的平台键（anthropic / openai / …），与 supply_pool_settings.pools 键一致；各池独立记账';
