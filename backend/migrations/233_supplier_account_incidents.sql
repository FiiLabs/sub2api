-- APEXONE-EXT: 双边市场——供给账号失效事件台账。
--
-- # 这张表要回答的问题
--
-- 一个供给者的号被上游封了、或者凭证过期了，在这张表出现之前，系统的全部反应是：
-- `accounts.status` 变成 `error`、`schedulable` 变成 false。然后就没有然后了。
--
--   - **供给者不知道**。他的号安静地停止接单，收益曲线掉到零。他下次登录仪表盘
--     才会看见，而那可能是两周之后。这是这套系统里对供给者最不友好的一件事：
--     他把自己的订阅交出来，出了问题却要靠自己去发现。
--   - **运营看不出趋势**。管理端只有一个 `Unhealthy` 计数——一个此刻的数字。
--     「这周坏了多少个」「谁的号在反复坏」「昨天那一批是不是同时坏的」，
--     一个即时计数一个也答不了，因为**状态没有历史**：号修好了，那个数字就变回去，
--     曾经坏过这件事在库里一个字节都不剩。
--   - **平台不做反应**。一个反复往平台塞坏号的人，与一个号偶尔掉线的正常供给者，
--     在准入这一侧完全无法区分。
--
-- 这张表把「坏过」这件事从一个瞬时状态变成一条有始有终的记录。三个时刻各一列：
-- 发现（detected_at）、告知（notified_at）、恢复（resolved_at）。
--
-- # 事件的判据：accounts.status <> 'active'
--
-- 这个判据能成立，靠的是供给侧一个刻意的设计：**供给者自己暂停/下线一个号，
-- 走的是 `extra` 里的 supply_state 加 `schedulable`，从不碰 `accounts.status`**
-- （见 supplier_onboarding_service.go 的 PauseAccount）。所以 status 这一列的
-- 非 active 值只会由两种力量写入：上游把号封了/凭证失效（accountRepository.SetError），
-- 或者管理员在账号页手工停用。两者都属于「供给者需要被告知的坏消息」，
-- 而供给者自己按的暂停不是——他知道自己按了。
--
-- 如果哪天有人让 PauseAccount 去写 status，这张表会立刻开始给每一个正常下线的
-- 供给者发"你的号出问题了"的邮件。这是那次改动会撞到的第一堵墙，写在这里。
--
-- # 为什么是「一个号同时只有一条未结事件」
--
-- 一个坏掉的号会被周期扫描每 5 分钟看见一次。没有约束的话，一个坏了一周的号
-- 会在这张表里留下两千行——报表里那个人的封禁数变成一个与现实无关的数字，
-- 而通知会每五分钟发一封。
--
-- 部分唯一索引 `(account_id) WHERE resolved_at IS NULL` 把这件事交给数据库：
-- 插入走 `ON CONFLICT (account_id) WHERE resolved_at IS NULL DO NOTHING`，
-- 于是「已经有一条开着的就什么都不做」是一次原子的判断，多实例同时扫描也安全。
-- 号恢复后事件被结掉，同一个号再坏时可以开一条**新的**——那才是第二次事件。
--
-- # 为什么把归属人和账号名快照进来
--
-- account_id 之外还存 user_id / account_name，是因为这张表的读者是**报表**，
-- 而报表要能解释一件已经发生的事。号被解绑（凭证抹掉、归属置空）或被删掉之后，
-- 「上个月谁坏了几个号」这个问题不该因为账号行没了就答不出来。
-- 这与 230 supplier_account_origins 存快照的理由是同一条。
--
-- # 刻意没有外键
--
-- 与 228 / 230 / 231 同理：这是一张追加式的证据表，它的价值恰恰在于比它记录的
-- 对象活得更久。一条 `ON DELETE CASCADE` 会让「删掉那个号」顺手抹掉他坏过的证据。

CREATE TABLE IF NOT EXISTS supplier_account_incidents (
    id             BIGSERIAL PRIMARY KEY,

    -- 出事的供给账号。没有外键，见文件头末段。
    account_id     BIGINT       NOT NULL,

    -- 归属人快照。写入时从 accounts.owner_user_id 取，此后不再跟着变——
    -- 一个号换了主人不该让旧事件记到新主人头上。
    user_id        BIGINT       NOT NULL,

    -- 账号名快照，只为报表可读（运营手上有的是名字，不是 id）。
    account_name   VARCHAR(255) NOT NULL DEFAULT '',
    platform       VARCHAR(32)  NOT NULL DEFAULT '',

    -- 出事那一刻的 accounts.status（error / rate_limited / disabled ...）。
    -- 存下来是因为它此后会变：号修好了 status 回到 active，而事件要记住是什么坏的。
    status         VARCHAR(32)  NOT NULL,

    -- 上游给的失败原因。供给者邮件里**不**放它的原文（可能含 token 片段与内部
    -- 地址），只在管理端报表里显示。截断由写入侧负责。
    error_message  TEXT         NOT NULL DEFAULT '',

    -- 扫描第一次看见它坏掉的时刻。不是「它真的坏掉的时刻」——后者无从得知，
    -- 两者最多差一个扫描周期（默认 5 分钟）。
    detected_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    -- 通知供给者的时刻。NULL = 还没发。这一列是「一次事件只发一封信」的闸：
    -- 发信成功才写它，所以 SMTP 挂掉时下一轮会重试，而不是把信永久丢掉。
    notified_at    TIMESTAMPTZ  NULL,

    -- 号恢复（status 回到 active）、或号已不存在（被解绑/删除）的时刻。
    -- NULL = 仍然坏着。它同时是上面那条部分唯一索引的判据。
    resolved_at    TIMESTAMPTZ  NULL,

    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE supplier_account_incidents IS '供给账号失效事件（双边市场：通知供给者、封禁率报表、接入熔断）';
COMMENT ON COLUMN supplier_account_incidents.resolved_at IS 'NULL 即仍然坏着；同时是「一个号只有一条未结事件」那条部分唯一索引的判据';
COMMENT ON COLUMN supplier_account_incidents.notified_at IS '通知供给者成功的时刻；NULL 时下一轮扫描会重试发信';

-- 一个号同时只有一条未结事件。本表最要紧的约束，见文件头。
CREATE UNIQUE INDEX IF NOT EXISTS uk_supplier_account_incidents_open
    ON supplier_account_incidents (account_id)
    WHERE resolved_at IS NULL;

-- 「这个人的号坏过几次」——封禁率报表与接入熔断都走它。
CREATE INDEX IF NOT EXISTS idx_supplier_account_incidents_user
    ON supplier_account_incidents (user_id, detected_at DESC);

-- 「现在有哪些号坏着」——运营巡检与待发通知的扫描走它。
-- 带上 notified_at 是为了让「未结且未通知」这个查询能只走索引：
-- 那是每 5 分钟跑一次的扫描，而绝大多数轮次它应该一行都读不到。
CREATE INDEX IF NOT EXISTS idx_supplier_account_incidents_open
    ON supplier_account_incidents (detected_at DESC, notified_at)
    WHERE resolved_at IS NULL;

-- 「最近这段时间坏了多少」——看板窗口聚合走它。
CREATE INDEX IF NOT EXISTS idx_supplier_account_incidents_detected
    ON supplier_account_incidents (detected_at DESC);
