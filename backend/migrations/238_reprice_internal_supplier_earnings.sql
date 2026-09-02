-- APEXONE-EXT: 一次性数据修正——把一个内部供给账号的历史收益按现行计费参数重算。
--
-- 计费参数（分组倍率、分成比例）调整后，该账号在旧参数下产生的入账需要按新参数
-- 重新计算。只影响这一个内部账号。
--
-- 做成迁移而不是管理端接口：这是一次性动作，不是要长期存在的能力。迁移跑一次即
-- 记入 schema_migrations，此后不再执行，也不存在可被调用的入口。
--
-- 必须改 accrue 行本身而不是补一条冲正流水：解冻任务按「到期 accrue 行的 amount」
-- 汇总搬运，只补冲正会让下一次解冻按旧金额搬，余额逐步回涨。
--
-- 重算后三条不变式仍然成立：
--   每行  basis_amount × share_ratio = amount
--   钱包  available = 未冻结行之和 / frozen = 冻结中行之和 / history = 全部之和

DO $$
DECLARE
    v_user_id       BIGINT  := 16;
    v_multiplier    NUMERIC := 0.14;
    v_share_ratio   NUMERIC := 0.50;

    v_foreign_rows  BIGINT;
    v_before_avail  NUMERIC(20,8);
    v_before_frozen NUMERIC(20,8);
    v_before_hist   NUMERIC(20,8);
    v_after_avail   NUMERIC(20,8);
    v_after_frozen  NUMERIC(20,8);
    v_after_hist    NUMERIC(20,8);
    v_repriced      BIGINT := 0;
    v_unmatched     BIGINT := 0;
BEGIN
    -- 锁住钱包行：迁移在容器启动时跑，此刻解冻任务与计费路径都可能已在运行。
    SELECT available_credit, frozen_credit, history_credit
      INTO v_before_avail, v_before_frozen, v_before_hist
      FROM supplier_credits
     WHERE user_id = v_user_id
     FOR UPDATE;

    IF v_before_hist IS NULL THEN
        RETURN;
    END IF;

    -- 只在该账号流水里除 accrue / thaw 之外什么都没有时才动：一旦有过 spend /
    -- withdraw / clawback，余额就不再能从 accrue 行重算出来。
    --
    -- 这时跳过而不是报错：迁移执行器 fail-closed，抛异常会让容器起不来。
    SELECT COUNT(*) INTO v_foreign_rows
      FROM supplier_credit_ledger
     WHERE user_id = v_user_id
       AND action NOT IN ('accrue', 'thaw');

    IF v_foreign_rows > 0 THEN
        RETURN;
    END IF;

    -- 新基数取 usage_logs.total_cost × 倍率，只依赖两个当下可核对的事实：
    -- 这笔请求的官方牌价，和现在的参数。usage_logs.request_id 有唯一索引（027），
    -- 所以 join 是确定的；匹配不到的行原样保留。
    --
    -- amount 从**已落到 8 位的 basis** 推出，而不是从 total_cost 一路算到底：
    -- 两条路会因舍入方向不同而分叉，导致「基数 × 比例 ≠ 金额」。这里与线上
    -- accrue 的算法保持一致。
    WITH repriced AS (
        UPDATE supplier_credit_ledger AS l
           SET basis_amount = ROUND(u.total_cost * v_multiplier, 8),
               share_ratio  = v_share_ratio,
               amount       = ROUND(ROUND(u.total_cost * v_multiplier, 8) * v_share_ratio, 8),
               updated_at   = NOW()
          FROM usage_logs AS u
         WHERE l.user_id    = v_user_id
           AND l.action     = 'accrue'
           AND l.request_id IS NOT NULL
           AND u.request_id = l.request_id
        RETURNING 1
    )
    SELECT COUNT(*) INTO v_repriced FROM repriced;

    SELECT COUNT(*) INTO v_unmatched
      FROM supplier_credit_ledger AS l
     WHERE l.user_id = v_user_id
       AND l.action  = 'accrue'
       AND NOT EXISTS (SELECT 1 FROM usage_logs AS u WHERE u.request_id = l.request_id);

    -- 从 accrue 行重算钱包。frozen_until IS NULL 表示该行已被解冻搬进可用区，
    -- 所以「未冻结行之和 = available」是这套账的定义。
    UPDATE supplier_credits AS c
       SET available_credit = s.avail,
           frozen_credit    = s.froz,
           history_credit   = s.hist,
           updated_at       = NOW()
      FROM (
        SELECT COALESCE(SUM(amount) FILTER (WHERE frozen_until IS NULL), 0)     AS avail,
               COALESCE(SUM(amount) FILTER (WHERE frozen_until IS NOT NULL), 0) AS froz,
               COALESCE(SUM(amount), 0)                                          AS hist
          FROM supplier_credit_ledger
         WHERE user_id = v_user_id AND action = 'accrue'
      ) AS s
     WHERE c.user_id = v_user_id
    RETURNING c.available_credit, c.frozen_credit, c.history_credit
         INTO v_after_avail, v_after_frozen, v_after_hist;

    -- 留痕。金额存正数（与其余动作同约定），方向与依据写在 remark 里。
    INSERT INTO supplier_credit_ledger (
        user_id, action, amount, share_ratio,
        available_after, frozen_after, history_after, remark
    ) VALUES (
        v_user_id,
        'reprice',
        ABS(v_after_hist - v_before_hist),
        v_share_ratio,
        v_after_avail, v_after_frozen, v_after_hist,
        format(
            'migration 238: repriced to current billing parameters '
            '(multiplier=%s share=%s), entries=%s unmatched=%s, history %s -> %s',
            v_multiplier, v_share_ratio, v_repriced, v_unmatched,
            v_before_hist, v_after_hist
        )
    );
END $$;
