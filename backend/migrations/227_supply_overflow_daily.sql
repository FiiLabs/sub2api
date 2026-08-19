-- APEXONE-EXT: 双边市场——溢出日计数（配额闸门 + 溢出率可见）。
--
-- 每一次「供给池干涸 → 溢出到自营池」，平台都在按自营成本供货、按供给池价收费。
-- 这张表存在的理由有两个，缺一不可：
--
--   1. **配额**。没有上限的话，消费者只要有办法持续把供给池打空（批量小号并发、
--      挑供给薄的时段），就能长期用 0.5× 的价买到 1.0× 成本的服务，而平台侧只有
--      一条 Warn 日志会喊。日配额把这个损失钉死在一个可预算的数上。
--   2. **可见**。溢出率是经营信号（供给规模跟不上需求），此前只能靠翻日志估。
--      这张表让管理端能直接答「今天溢出了多少次、被配额拦下多少次」。
--
-- 为什么是 Postgres 而不是 Redis 计数器：
--   配额判定必须是原子的、跨实例的，且**判定与计数是同一次写**（见下面 repo 里那条
--   带 WHERE 的 ON CONFLICT DO UPDATE）。Redis 也能做，但那样这份数据只活在缓存里，
--   而它同时是要给管理员看的经营数据——重启/驱逐后归零的经营数据不如没有。
--   溢出是失败路径上的稀有事件，多一次 upsert 的代价可以忽略；如果它不稀有，
--   那本身就是要人介入的信号。
--
-- day 用 DATE 而不是时间戳：配额按「平台时区的自然日」结算，跨日重置。写入方传入的
-- 是按 timezone.Now() 算出来的当天日期，不是 UTC——否则中国部署的管理员看到的「今天」
-- 会在早上八点才开始。

CREATE TABLE IF NOT EXISTS supply_overflow_daily (
    day             DATE PRIMARY KEY,

    -- 真正发生了的溢出次数（已经在自营池上成功选到号那一次算，选号失败的不算）。
    overflow_count  BIGINT NOT NULL DEFAULT 0,

    -- 因为配额已满而**没有**溢出的次数。与 overflow_count 分开是必须的：
    -- 混在一起的话，「今天溢出 500 次」既可能是花了 500 次的钱，也可能是省了 500 次，
    -- 而这两件事对运营的含义正好相反。
    denied_count    BIGINT NOT NULL DEFAULT 0,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE supply_overflow_daily IS '供给池溢出的日计数与配额闸门（双边市场）';
COMMENT ON COLUMN supply_overflow_daily.day IS '平台时区的自然日';
COMMENT ON COLUMN supply_overflow_daily.overflow_count IS '当日实际溢出次数（平台按自营成本供了货）';
COMMENT ON COLUMN supply_overflow_daily.denied_count IS '当日因日配额已满而未溢出的次数';
