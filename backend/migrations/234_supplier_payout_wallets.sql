-- APEXONE-EXT: 双边市场——链上收款地址绑定 + 提现单的链上字段。
--
-- 229 建的提现单是「工单」：运营看着 payout_account 手工打款，external_ref 手填凭证。
-- 这次让其中一部分渠道自动化——供给者绑一个 BSC 地址，worker 直接把 USDT 打过去。
--
-- # 为什么地址要单独一张表，而不是继续塞在 payout_account 里
--
-- payout_account 是**每张单子一份的快照**，这没错也不该改：单子建好之后再改绑定，
-- 不能把一笔在途的打款改道。但快照解决不了两件事：
--
--   1. 地址得在**绑定时**就校验一次。链上转账不可逆，等到运营点打款时才发现地址
--      少一位，钱已经出去了。校验必须发生在一个「还没有任何钱牵扯进来」的时刻。
--   2. 一个地址只能属于一个账号。这是反女巫：否则一个人注册 50 个号、贡献 50 个
--      账号、全提到同一个地址，平台在补贴一个人却以为在补贴一个市场。这条约束
--      需要一个 UNIQUE 索引，而 UNIQUE 索引没法建在「每张单子一份的快照」上。
--
-- 所以：绑定表是**校验过的、唯一的**地址来源，提现单在建单那一刻从它取一份快照。
--
-- # 地址为什么加密存、又为什么还能建唯一索引
--
-- 迁移 232 把 payout_account 加密了，理由是「当事人换不掉」。链上地址其实换得掉
-- （再生成一个就是），但 **user_id ↔ address 这条连线换不掉**：库泄漏一次，
-- 「平台上的这个人就是链上的那个地址」这件事就永久成立了，而链上是公开可查的。
-- 所以地址同样走 AES-256-GCM（复用 payoutAccountCipher）。
--
-- 但 GCM 每次加密带随机 nonce，同一个地址两次入库是两串不同的密文——唯一索引就废了。
-- 因此额外存一列 address_hash = SHA-256(小写地址)，唯一索引建在它上面。
--
-- 拿哈希当盲索引在这里是安全的，因为被哈希的东西不是低熵秘密：EVM 地址是 160 位，
-- 穷举不可行。攻击者能做的只有「我猜是这个地址，验证一下」——而他要能猜，
-- 就已经知道那个地址了。这与「哈希密码」是两类问题，别照搬那边的直觉。
CREATE TABLE IF NOT EXISTS supplier_payout_wallets (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- 链标识。当前只有 'bsc'；做成列而不是焊死，是因为加第二条链时这张表
    -- 不该重建——但也**不**建成枚举类型：Postgres 的 enum 加值要 ALTER TYPE，
    -- 而这个值集的权威定义在 Go 那边（SupplierPayoutNetworks）。
    network      VARCHAR(16) NOT NULL,

    -- 密文地址（enc.v1: 前缀 + AES-256-GCM）。展示与打款时解密。
    address      VARCHAR(256) NOT NULL,
    -- SHA-256(小写地址) 的十六进制，64 字符。唯一索引建在它上面（见文件头）。
    address_hash CHAR(64) NOT NULL,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE supplier_payout_wallets IS '供给者链上收款地址绑定（双边市场）';
COMMENT ON COLUMN supplier_payout_wallets.address IS 'AES-256-GCM 密文，enc.v1: 前缀';
COMMENT ON COLUMN supplier_payout_wallets.address_hash IS 'SHA-256(小写地址)，唯一索引用的盲索引';

-- 每人每链一个地址。绑新的就是覆盖旧的（upsert），不保留历史：
-- 留着历史绑定只会让「这笔钱该打到哪」多出一个需要选择的分支，而答案永远是最新那个。
CREATE UNIQUE INDEX IF NOT EXISTS uniq_supplier_payout_wallets_user_network
    ON supplier_payout_wallets (user_id, network);

-- 反女巫：一个地址在一条链上只能属于一个账号（见文件头第 2 点）。
--
-- 它同时是一道**误绑保护**：把地址填成别人的（复制粘贴串行、抄了个示例地址），
-- 在绑定这一刻就被拒，而不是等钱打到陌生人手里之后。
CREATE UNIQUE INDEX IF NOT EXISTS uniq_supplier_payout_wallets_network_hash
    ON supplier_payout_wallets (network, address_hash);

-- ============================================================================
-- 提现单的链上字段
-- ============================================================================

-- network 非空即「这张单子由 worker 上链结算」，为空即沿用 229 的人工打款。
-- 用一个可空列来分辨两条路径，而不是加一个 is_onchain 布尔：布尔为真时还得再问
-- 一次「那是哪条链」，两个字段就能不一致。
ALTER TABLE supplier_withdrawals ADD COLUMN IF NOT EXISTS network VARCHAR(16) NULL;

-- 币种符号与合约地址。**落地的是地址**，符号只作展示。
-- 符号→地址的映射来自配置，配置一改，历史单子上那个「USDT」指的是哪个合约就被
-- 悄悄改写了；把地址钉在行上，这张单子发的是什么，十年后还答得出来。
ALTER TABLE supplier_withdrawals ADD COLUMN IF NOT EXISTS token_symbol  VARCHAR(16) NULL;
ALTER TABLE supplier_withdrawals ADD COLUMN IF NOT EXISTS token_address VARCHAR(64) NULL;

-- gas 手续费，从供给者收益里扣：链上实发 = amount - fee_amount。
--
-- amount 仍然是**从可用区扣走的总额**（gross），这一条不能改：ledger 的 withdraw
-- 流水、退款、导出全部按 amount 走，改了它等于改写这三条路径的含义。
-- 于是 fee 只是 amount 内部的一次切分，退款时按 amount 全额退（gas 还没花出去）。
--
-- DEFAULT 0 让 229 时代的历史单子自动是「无手续费」，与它们的实际情况一致。
ALTER TABLE supplier_withdrawals ADD COLUMN IF NOT EXISTS fee_amount DECIMAL(20,8) NOT NULL DEFAULT 0;

-- 链上交易哈希。到位后 external_ref 也会被写成同一个值——
-- external_ref 是「给人看的凭证」，tx_hash 是「给程序对账的键」，两者恰好同源，
-- 但用途不同，不合并成一列：将来换链或换渠道，external_ref 还得装别的东西。
ALTER TABLE supplier_withdrawals ADD COLUMN IF NOT EXISTS tx_hash VARCHAR(80) NULL;

-- 广播用的链上 nonce，**广播之前**就落库。
--
-- 这是「广播响应丢了」唯一可靠的解药：同一个 nonce 在链上最多只有一笔交易能成功，
-- 所以重试时原样复用它，最坏情况是重复广播、链上择一，绝不会变成两次转账。
-- 不落库而每次向节点要 nonce，重试就会拿到下一个号，于是同一张单子打两次款。
ALTER TABLE supplier_withdrawals ADD COLUMN IF NOT EXISTS chain_nonce BIGINT NULL;

-- worker 租约。非空且在未来 = 有人正在处理这张单子。
-- 它让定时 worker 与管理端的「手动推进」按钮互斥：没有这道锁，运营点一下就可能
-- 与正在跑的 worker 同时广播同一笔转账，而那两笔会拿到不同的 nonce。
ALTER TABLE supplier_withdrawals ADD COLUMN IF NOT EXISTS leased_until TIMESTAMPTZ NULL;

-- 上一次处理失败的原因。留给运营排查，也写进单子详情给供给者看个大概。
-- 成功进入终态时清空——一条陈旧的报错留在 confirmed 的单子上只会误导人。
ALTER TABLE supplier_withdrawals ADD COLUMN IF NOT EXISTS last_error TEXT NULL;

COMMENT ON COLUMN supplier_withdrawals.network IS '非空=由 worker 上链结算；空=人工打款';
COMMENT ON COLUMN supplier_withdrawals.token_address IS '稳定币合约地址，落地即快照，不随配置漂移';
COMMENT ON COLUMN supplier_withdrawals.fee_amount IS 'gas 手续费，从 amount 内部切分；链上实发 = amount - fee_amount';
COMMENT ON COLUMN supplier_withdrawals.chain_nonce IS '广播前落库，重试必须复用——防双付的唯一手段';
COMMENT ON COLUMN supplier_withdrawals.leased_until IS 'worker 租约；与管理端手动推进互斥';

-- 229 的 status 是 VARCHAR(16) 且没有 CHECK 约束，所以扩状态不需要 ALTER 约束。
-- 新增两个中间态（processing / failed），权威定义仍在 Go 那边的白名单里。
COMMENT ON COLUMN supplier_withdrawals.status IS 'pending|processing|paid|failed|rejected|canceled';

-- worker 的取件查询：待处理 + 租约过期的，最老的先做。
--
-- 条件里同时有 pending 和 processing：processing 是「已广播、等确认」，
-- 它的租约同样会过期（进程重启、等待超时），过期后必须能被重新取件继续等确认，
-- 否则一张已经广播出去的单子会永远停在 processing。
CREATE INDEX IF NOT EXISTS idx_supplier_withdrawals_worker_queue
    ON supplier_withdrawals (created_at)
    WHERE status IN ('pending', 'processing');
