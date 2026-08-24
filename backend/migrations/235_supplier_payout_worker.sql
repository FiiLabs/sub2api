-- APEXONE-EXT: 双边市场——链上打款 worker（M4）需要的两样东西。
--
-- 状态机、租约、nonce、tx_hash 这些列 234 都已经备好，这里只补运行期缺的两件：

-- broadcasted_at：第一次广播的时刻，只写一次。
--
-- 它是「放弃等确认」这个决定唯一可靠的时钟。worker 对一笔交易的确认查询可能
-- 永远等不到结果（广播传输失败后重试会重签，gasPrice 变了哈希就变了，而真正
-- 上链的可能是上一次签的那笔）——没有一个截止点，这张单子会在 processing 里
-- 无限转圈，而管理端对 processing 的单子刻意无权处置。到点后 worker 把单子标成
-- failed 并在 last_error 里写明「结果不明，退款前先查链」，交还给人。
--
-- 不用 created_at 当这个时钟：单子可能建成几天后 worker 才被打开；
-- 不用 updated_at：每次续租都会碰它，永远到不了点。
ALTER TABLE supplier_withdrawals ADD COLUMN IF NOT EXISTS broadcasted_at TIMESTAMPTZ NULL;

COMMENT ON COLUMN supplier_withdrawals.broadcasted_at IS '第一次广播的时刻，只写一次；worker 放弃等确认的时钟';

-- 捞单谓词的部分索引。
--
-- worker 每 30 秒扫一次「链上、未决、租约空闲」，绝大多数时刻这个集合是空的——
-- 部分索引让空扫描的代价是一次索引探测，而不是全表。谓词里刻意不带租约条件：
-- leased_until 每次续租都变，放进谓词会让每次续租都要维护索引。
CREATE INDEX IF NOT EXISTS idx_supplier_withdrawals_payout_due
    ON supplier_withdrawals (id)
    WHERE network IS NOT NULL AND status IN ('pending', 'processing');
