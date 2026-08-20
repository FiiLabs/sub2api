-- APEXONE-EXT: 双边市场——支付争议（拒付 / chargeback）台账。
--
-- # 为什么需要一张表
--
-- 拒付是这套系统里**唯一一件平台会真的赔钱**的事，而在这张表出现之前它在库里
-- 没有任何痕迹：Stripe 推来的 charge.dispute.* 走到 provider 那个 switch 里
-- 命中 `return nil, nil`，webhook 回 200，然后什么都不发生。
--
-- 三件事必须落在同一行上，否则事后回答不了「这笔亏损是怎么来的」：
--
--   1. **幂等**。同一个争议 Stripe 会推很多次（created / updated / closed /
--      funds_withdrawn / funds_reinstated），而这条路径上的副作用是**扣钱**——
--      追回供给者的分成、扣回消费者的余额。没有幂等键就等于每收到一次推送
--      扣一遍。dispute_id 上的 UNIQUE 是这件事的唯一保证。
--   2. **对账**。争议金额是通道币种的，追回和扣回是 users.balance 口径的，
--      两者之间隔着一次按订单比例的换算。把换算前后的四个数都存下来，
--      运营核对时不需要重跑一遍那段算术。
--   3. **未覆盖的那部分**。uncovered_basis > 0 意味着这笔拒付发生时，
--      对应的分成已经解冻甚至已经提现——那部分平台自吃。它是「冻结窗配短了」
--      的直接证据，也是 §6 里 freeze_hours 该调到多少的唯一一手数据。
--
-- # 为什么不复用 payment_orders 的状态
--
-- payment_orders.status 是 core 的枚举，被前端筛选、统计、退款守卫等十几处消费。
-- 往里加一个 DISPUTED 值，等于让每一个 `switch status` 的地方多一条必须正确处理
-- 的分支，而它们全在上游合并的热区。何况「被拒付」与「已退款」在业务上确实不同：
-- 退款是我们主动发起的，refund_amount / refund_at / force_refund 那几列有明确含义；
-- 拒付是钱被别人拿走，我们从头到尾没发起过任何东西。
-- 订单那边只留一条 payment_audit_logs（action = DISPUTE_*），这张表才是事实来源。
--
-- # 刻意没有外键
--
-- 与 228 / 230 同样的理由，外加一条本表独有的：**争议完全可能指向一个我们没有的订单**。
-- 同一个 Stripe 账户被多套环境（预发 / 另一个部署）共用时，webhook 会推来别人的
-- 争议；那种情况下 order_id 为 NULL，这一行仍然要落库——它是「我们收到过这件事、
-- 但没能对上订单」的唯一记录，恰恰是最需要人去看的一类。

CREATE TABLE IF NOT EXISTS payment_disputes (
    id               BIGSERIAL PRIMARY KEY,

    -- 通道侧的争议 id（Stripe 的 dp_...）。幂等键，见文件头第 1 点。
    dispute_id       VARCHAR(191) NOT NULL,

    -- 哪条通道推来的。目前只有 stripe 实现了争议事件，存下来是为了将来
    -- 多一条通道时不必猜这一行是谁写的。
    provider_key     VARCHAR(32)  NOT NULL,

    -- 被争议的支付意图 id，对应 payment_orders.payment_trade_no。
    -- 它是查订单的主键——Stripe 的 dispute 对象不带我们的商户订单号。
    trade_no         VARCHAR(191) NOT NULL DEFAULT '',

    -- 对上的订单。对不上时为 NULL（见文件头末段），不是错误。
    order_id         BIGINT       NULL,
    out_trade_no     VARCHAR(191) NOT NULL DEFAULT '',

    -- 被拒付的消费者。同样可能为 NULL（订单没对上时）。
    user_id          BIGINT       NULL,

    -- open / won / lost，语义见 internal/payment/dispute.go。
    -- 刻意不存 Stripe 那八个原始状态：那几个的区别决定的是「运营该不该上传证据」，
    -- 而那件事发生在 Stripe 后台，不发生在这里。
    status           VARCHAR(16)  NOT NULL,

    -- 通道给的争议原因（fraudulent / product_not_received / ...）。只给人看。
    reason           VARCHAR(64)  NOT NULL DEFAULT '',

    -- 争议金额与其币种，通道口径（主单位，不是分）。
    dispute_amount   DECIMAL(20,8) NOT NULL DEFAULT 0,
    currency         VARCHAR(8)    NOT NULL DEFAULT '',

    -- 换算到 users.balance 口径之后的基数。追回与扣回都按它算。
    basis_amount     DECIMAL(20,8) NOT NULL DEFAULT 0,

    -- 实际从消费者余额扣回了多少。可能小于 basis_amount——他已经花掉了，
    -- 那正是拒付欺诈得手的形态，差额同样是平台的亏损。
    balance_deducted DECIMAL(20,8) NOT NULL DEFAULT 0,

    -- 从供给者冻结区追回的 credit，及其对应的消费基数。
    clawed_credit    DECIMAL(20,8) NOT NULL DEFAULT 0,
    clawed_basis     DECIMAL(20,8) NOT NULL DEFAULT 0,
    -- max(0, basis_amount - clawed_basis)。见文件头第 3 点。
    uncovered_basis  DECIMAL(20,8) NOT NULL DEFAULT 0,

    -- 副作用只跑一次的闸。非空 = 追回与扣回已经执行过，后续任何一次推送
    -- （包括 closed）都不再重复执行。刻意用时间戳而不是布尔：
    -- 「什么时候扣的」在对账时和「扣没扣」一样重要。
    settled_at       TIMESTAMPTZ  NULL,

    -- 争议关闭的时刻（won / lost 到达时写）。
    resolved_at      TIMESTAMPTZ  NULL,

    -- 原始报文，留证。
    raw_data         TEXT         NOT NULL DEFAULT '',

    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE payment_disputes IS '支付争议/拒付台账（双边市场，分成追回与亏损归因）';
COMMENT ON COLUMN payment_disputes.settled_at IS '追回与扣回执行完成的时刻；非空即不再重复执行';
COMMENT ON COLUMN payment_disputes.uncovered_basis IS '拒付发生时已解冻、追不回的那部分基数——冻结窗是否配短了的直接证据';

-- 幂等键。UPSERT 走它，是本表最要紧的一个约束。
CREATE UNIQUE INDEX IF NOT EXISTS uk_payment_disputes_dispute_id
    ON payment_disputes (dispute_id);

-- 「这个订单被拒付过吗」——订单详情页与人工排查走它。
CREATE INDEX IF NOT EXISTS idx_payment_disputes_order
    ON payment_disputes (order_id)
    WHERE order_id IS NOT NULL;

-- 「这个人拒付过几次」——拒付率报表与风控走它。同一个人反复拒付是最强的欺诈信号。
CREATE INDEX IF NOT EXISTS idx_payment_disputes_user
    ON payment_disputes (user_id, created_at DESC)
    WHERE user_id IS NOT NULL;

-- 「最近有哪些没结的争议」——运营每日巡检走它。
CREATE INDEX IF NOT EXISTS idx_payment_disputes_status
    ON payment_disputes (status, created_at DESC);
