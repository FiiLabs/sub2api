-- APEXONE-EXT: 双边市场——供给者提现申请单。
--
-- 钱包（225）到这里才有第二个出口：在此之前 credit 只能抵扣自己发起的请求，
-- 也就是说供给者赚到的钱走不出这个系统。一个只能内部消费的余额不是收入。
--
-- 这张表是**申请单**，不是账。账在 supplier_credit_ledger 里：申请通过时钱就已经
-- 从 available_credit 扣走并记了一条 withdraw 流水，这张表只负责记录「这笔钱要打到
-- 哪里、现在到哪一步、谁审的」。两者由 ledger_id 关联。
--
-- 为什么扣款发生在**申请时**而不是审批时：
--   审批时才扣，意味着从申请到审批之间那笔钱还留在可用区——供给者可以拿它继续付
--   自己的请求，也可以再提一次。运营在后台看到一张 100 的单子点了打款，钱可能早就
--   花掉了，于是要么打出去的是平台垫的钱，要么余额被扣成负数。申请即扣不会有这个
--   窗口，代价只是被拒时要退回去（写一条 withdraw_revert 流水，见下）。
CREATE TABLE IF NOT EXISTS supplier_withdrawals (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- 申请金额，与钱包同口径 DECIMAL(20,8)。申请单一旦建立就不可改金额：
    -- 改金额等于把已经扣掉的那笔钱和这张单子对不上，要改只能拒掉重提。
    amount           DECIMAL(20,8) NOT NULL CHECK (amount > 0),

    -- pending  待处理（钱已从 available 扣走）
    -- paid     已打款（终态）
    -- rejected 被运营拒绝（终态，钱已退回 available）
    -- canceled 供给者自己撤回（终态，钱已退回 available）
    status           VARCHAR(16) NOT NULL DEFAULT 'pending',

    -- 收款方式与账号。平台不解析、不校验格式，原样呈现给运营去打款：
    -- 猜一个收款渠道的格式（银行卡位数、链地址校验和）是一件会随时过期的事，
    -- 猜错了就把一个能收款的人挡在门外。
    payout_channel   VARCHAR(64) NOT NULL,
    payout_account   VARCHAR(256) NOT NULL,
    -- 供给者自己的备注（发票抬头、到账姓名等）。
    user_note        TEXT NULL,

    -- 申请时那条 withdraw 流水。ON DELETE SET NULL 是形式上的：流水表只追加不删。
    ledger_id        BIGINT NULL REFERENCES supplier_credit_ledger(id) ON DELETE SET NULL,

    -- 谁处理的、处理意见、打款凭证（交易号/截图链接/工单号，平台不解析）。
    reviewer_id      BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    review_note      TEXT NULL,
    external_ref     VARCHAR(128) NULL,

    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 进入终态的时刻。NULL 就是「还挂着」，运营的待办队列按它筛。
    resolved_at      TIMESTAMPTZ NULL
);

COMMENT ON TABLE supplier_withdrawals IS '供给者提现申请单（双边市场）';
COMMENT ON COLUMN supplier_withdrawals.status IS 'pending|paid|rejected|canceled';
COMMENT ON COLUMN supplier_withdrawals.ledger_id IS '申请时扣款的那条 withdraw 流水';
COMMENT ON COLUMN supplier_withdrawals.external_ref IS '打款凭证/交易号，平台不解析';
COMMENT ON COLUMN supplier_withdrawals.resolved_at IS '进入终态的时刻；NULL = 仍在 pending';

-- 供给者自己的提现记录页：按人倒序翻页。
CREATE INDEX IF NOT EXISTS idx_supplier_withdrawals_user_created
    ON supplier_withdrawals (user_id, created_at DESC);

-- 运营待办队列：只扫还挂着的单子，最老的先处理。
CREATE INDEX IF NOT EXISTS idx_supplier_withdrawals_pending
    ON supplier_withdrawals (created_at)
    WHERE status = 'pending';

-- 「每人最多几张未决单」这条限制的加速索引。它**不是**唯一索引：
-- 上限是可配置的（supply_withdrawal_settings.max_pending），做成 UNIQUE 就等于
-- 把「1」这个当前默认值焊死在 schema 里，运营调大参数时数据库会先报错。
-- 真正的并发保护来自申请事务里对钱包行的 FOR UPDATE：同一个人的两次申请必然串行。
CREATE INDEX IF NOT EXISTS idx_supplier_withdrawals_user_pending
    ON supplier_withdrawals (user_id)
    WHERE status = 'pending';
