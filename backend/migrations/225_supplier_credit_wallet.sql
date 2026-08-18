-- APEXONE-EXT: 双边市场——供给者「赚取钱包」（earn wallet）。
--
-- 供给者把自己的订阅账号挂进供给池，别人用掉的量按分成比例入账到这里。钱包里的
-- credit 有两种出路：自己消费（抵扣自己发起的请求）或提现。
--
-- 为什么是 raw SQL 而不是 ent：
--   这两张表是资金流水，写入点在 applyUsageBillingEffects 的计费事务内部——那是一段
--   手写 SQL 的热路径，ent 生成的 builder 挤不进去。本仓已有同形态先例
--   （130/131/133 的 user_affiliates + user_affiliate_ledger），刻意照抄那套结构而不是
--   另发明一套，让两个钱包在运维、对账、排障上是同一种东西。
--
-- 金额一律 DECIMAL(20,8)，与 users.balance / user_affiliates.aff_quota 同口径，避免
-- 跨表运算时精度不一致。

-- ============================================================================
-- 1) supplier_credits：每个供给者一行的钱包余额。
-- ============================================================================
CREATE TABLE IF NOT EXISTS supplier_credits (
    user_id           BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    available_credit  DECIMAL(20,8) NOT NULL DEFAULT 0,
    frozen_credit     DECIMAL(20,8) NOT NULL DEFAULT 0,
    history_credit    DECIMAL(20,8) NOT NULL DEFAULT 0,
    spent_credit      DECIMAL(20,8) NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE supplier_credits IS '供给者赚取钱包余额（双边市场）';
COMMENT ON COLUMN supplier_credits.available_credit IS '已解冻、可消费/可提现的 credit';
COMMENT ON COLUMN supplier_credits.frozen_credit IS '冻结中的 credit（等待冻结窗结束）';
COMMENT ON COLUMN supplier_credits.history_credit IS '累计入账（含仍在冻结的部分），只增不减';
COMMENT ON COLUMN supplier_credits.spent_credit IS '累计已消费掉的 credit，只增不减';

-- 供给者列表/排行按可用额排序。
CREATE INDEX IF NOT EXISTS idx_supplier_credits_available
    ON supplier_credits (available_credit);

-- ============================================================================
-- 2) supplier_credit_ledger：追加式流水，每条自带审计快照。
-- ============================================================================
--
-- action 取值：
--   accrue   入账（别人用了我的账号）——正数
--   spend    消费（我用赚取钱包付自己的请求）——正数，表示扣掉的量
--   thaw     解冻（frozen → available 的搬运记录）——正数
--   clawback 追回（冻结窗内发生拒付）——正数，表示扣掉的量
--   withdraw 提现——正数
--
-- 金额一律存正数，方向由 action 决定；这样对账时不需要关心符号约定，SUM 按 action
-- 分组即可。
CREATE TABLE IF NOT EXISTS supplier_credit_ledger (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action           VARCHAR(32) NOT NULL,
    amount           DECIMAL(20,8) NOT NULL,

    -- 幂等键 = usage_log.request_id。accrue/spend 用它保证「一次消耗只对应一次入账」，
    -- 计费事务重放（网络重试、进程崩溃后补偿）不会重复发钱。
    request_id       VARCHAR(64) NULL,

    -- 这笔入账由哪个供给账号产出；账号被删时置 NULL 而不是连坐删流水，
    -- 流水是钱，比账号活得久。
    account_id       BIGINT NULL REFERENCES accounts(id) ON DELETE SET NULL,
    -- 消费方用户（谁用掉的）。同理 SET NULL。
    source_user_id   BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,

    -- 分成审计快照：交接件要求供给者能自行核对计量，因此把「基数 × 比例 = amount」
    -- 的三要素连同当时生效的比例一起冻在流水里。事后调参不会改写历史账。
    basis_amount     DECIMAL(20,8) NULL,
    share_ratio      DECIMAL(10,6) NULL,

    -- 冻结窗：非 NULL = 仍在冻结，到点由 thaw 搬进 available 并置 NULL。
    -- 语义与 user_affiliate_ledger.frozen_until 完全一致。
    frozen_until     TIMESTAMPTZ NULL,

    -- 余额快照（写入后的三个值），供给者账单页无需重算即可展示。
    available_after  DECIMAL(20,8) NULL,
    frozen_after     DECIMAL(20,8) NULL,
    history_after    DECIMAL(20,8) NULL,

    -- 人读备注：clawback/withdraw 的原因、工单号等。
    remark           TEXT NULL,

    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE supplier_credit_ledger IS '供给者赚取钱包流水（追加式，含审计快照）';
COMMENT ON COLUMN supplier_credit_ledger.action IS 'accrue|spend|thaw|clawback|withdraw';
COMMENT ON COLUMN supplier_credit_ledger.request_id IS '幂等键，取自 usage_logs.request_id';
COMMENT ON COLUMN supplier_credit_ledger.basis_amount IS '分成基数（消费者实付金额）';
COMMENT ON COLUMN supplier_credit_ledger.share_ratio IS '入账时生效的分成比例快照';
COMMENT ON COLUMN supplier_credit_ledger.frozen_until IS '冻结到期时间；NULL = 未冻结或已解冻';

-- 「消耗 === 入账」不变量的数据库兜底：同一个 request_id 在同一 action 下只能有一条。
-- 放成部分唯一索引而不是表级 UNIQUE，是因为 thaw/withdraw 这类流水没有 request_id，
-- 多行 NULL 必须允许。
CREATE UNIQUE INDEX IF NOT EXISTS idx_supplier_credit_ledger_request_action_uniq
    ON supplier_credit_ledger (action, request_id)
    WHERE request_id IS NOT NULL;

-- 供给者账单页：按人倒序翻页。
CREATE INDEX IF NOT EXISTS idx_supplier_credit_ledger_user_created
    ON supplier_credit_ledger (user_id, created_at DESC);

-- 解冻扫描：只扫仍在冻结的行。
CREATE INDEX IF NOT EXISTS idx_supplier_credit_ledger_thaw
    ON supplier_credit_ledger (user_id, frozen_until)
    WHERE frozen_until IS NOT NULL;

-- 按账号看收益（供给者仪表盘的「哪个账号在赚钱」）。
CREATE INDEX IF NOT EXISTS idx_supplier_credit_ledger_account
    ON supplier_credit_ledger (account_id)
    WHERE account_id IS NOT NULL;
