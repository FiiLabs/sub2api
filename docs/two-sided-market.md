# 双边 Token 平台（ApexOne）实施方案

> 分支：`feat/two-sided-market`（基线 `sync/upstream-v0.1.177` ← `proof-solo`）
> 设计出处：wayfinder 交接件（`HANDOFF.md` / `architecture.md` / `business-risk.md` / 14 张 ticket，未纳入本仓 git）
> 起始日期：2026-08-18

## 0. 本轮已定的五条决策

设计交接件内部有一处冲突（`architecture.md §9` 主张方案②开源+从 proof-solo 起步；更晚的 `HANDOFF §F` 翻案为方案③闭源+从 0.1.177 干净私有仓重建）。2026-08-18 由决策人当面裁定，本实施取以下组合：

| # | 决策 | 取值 |
|---|---|---|
| 1 | 开发基线 | 本仓 `proof-solo` → 新分支，**保留 TEE**（proof/attestation/e2ee/dstack/phala 全部原样留下，不执行 `architecture.md §8` 的拆除清单） |
| 2 | 开源姿态 | **开源**（2026-08-18 决策人裁定）。留在公开仓 `FiiLabs/sub2api`，不新建干净私有仓 |
| 3 | 上游同步点 | `v0.1.177` tag（非 `upstream/main` tip，遵循「别追 tip」纪律） |
| 4 | 首版范围 | **地基垂直切片**，非 `architecture.md` 全量 |
| 5 | 供给者接入深度 | 含**自助 OAuth 全流程**（非管理员代建的最小形态） |

**净效果 = 回到 `architecture.md §9` 的方案②**（开源 + 分支卫生 + 从 proof-solo 拉 feature 分支），`HANDOFF §F` / ticket 10 更正段的方案③（闭源私有 SaaS + 彻底弃 TEE + 从 0.1.177 干净重建）**整条不采纳**。

连带影响：

- **LGPL**：闭源路线那条「永不公开分发修改后二进制/镜像，否则触发源码义务」的红线随之消失——开源分发本就满足 LGPL，合规面反而简单。仍建议律师复核，但不再是存亡级前提。
- **TEE 重新有意义**：ticket 09 判 TEE 归零的前提是「闭源使 attestation 证明无意义」。开源后该前提不成立，故保留 TEE 是自洽的，`architecture.md §8` 的拆除清单作废。
- **暴露面（认）**：检测规避层代码（`shouldMimicClaudeCode` 伪装 Claude Code、TLS-JA3 指纹、metadata 改写）与本文档描述的双边模型均公开可见。这正是方案③想规避的 willful-circumvention 证据面，决策人已知悉并选择承担。

未采纳的设计分支保留在交接件里，随时可重新翻案；本文档只记录当前实施取向。

## 1. 首版垂直切片范围

打通「一个供给账号进池 → 被消费 → 供给者赚到冻结积分 → 到期释放 → 可当消费花」这条端到端链路。

**在范围**：`account.owner_user_id`、赚取钱包 ledger、内联结算 accrue、thaw 释放、供给池/自营池配置与溢出、供给者自助 OAuth 接入、供给者仪表盘、下线双通道与观察期。

**出范围（后续刀次）**：新供给者保底 floor（ticket 14 A/B）、积分提现（ticket 14 C，**显式未决**）、封禁率报表与平台熔断、自动代理池、拟人化时段节流、订阅档位识别、User 信誉分、连接级 draining、API key 型供给。

## 2. 与设计交接件的实测偏差

`architecture.md` / ticket 10 写于 0.1.161–0.1.170 期间，以下细节在 0.1.177 上已对不上，实施以本节为准：

| 交接件记载 | 0.1.177 实测 |
|---|---|
| `docs/impl/README.md` 花名册 + `docs/impl/two-sided-market.md` | **`docs/impl/` 不存在**，本仓 `docs/` 是扁平结构 → 本文档落在 `docs/two-sided-market.md` |
| 迁移编号 194+ | 现有迁移已到 `migrations/223_*`，新迁移从 **224** 起 |
| `router.go:116` registerRoutes | 定义在 `router.go:99`，`routes.Register*Routes` 调用块在 **`router.go:117-132`** |
| `repository/wire.go:67`、`service/wire.go:676`、`handler/wire.go:217`、`handler/handler.go:47`&`wire.go:168` | `repository/wire.go:67`（ProviderSet 起始，`NewAffiliateRepository` 在 :104）、`service/wire.go:861`、`handler/wire.go:274`、`handler/handler.go:41` & `handler/wire.go:45/85` |
| affiliate 模板含 `handler/affiliate*.go`、`routes/affiliate.go` | affiliate **没有**独立 handler/routes 文件：service/repo 是 `internal/{service,repository}/affiliate_*.go`，路由挂在 `routes/admin.go:768` 的 `admin.Group("/affiliates")` 下。供给侧仍按扁平文件模式新建，但 `routes/supplier.go` 是纯新增，无同名先例可抄 |
| `usage_billing_repo.go:174` `applyUsageBillingEffects` | **仍在 :174**，签名未变，fork 未改动 → 冲突风险确实低 |
| ticket 07：「供给池干涸溢出到自营池，用现成的 `fallback_group_id`，零 core 改动」 | **不成立**。`fallback_group_id` 的唯一读者是 `resolveGatewayGroup`（`gateway_scheduling.go:872`），触发条件是「分组开了 `claude_code_only` 而请求不是 Claude Code 客户端」——按**客户端类型**做的静态降级，发生在选号**之前**，与有没有可用账号无关。供给池被抽干时请求照样在供给池里走完整轮调度然后拿 `ErrNoAvailableAccounts` 出来。溢出必须另起一条规则，代价是一处 core 侵入（见 core touch #7） |
| ticket 06：「健康检查复用现成的 `TestAccountConnection`」 | **不成立**。`AccountTestService.TestAccountConnection`（`account_test_service.go:263`）第一个形参是 `*gin.Context`，函数体直接往响应流里写 SSE——它是一个 HTTP 处理器，不是可复用的探针。观察期要判「这个号还活着吗」得另写一个无 gin 依赖的探测函数（随 #9 落），或退而用既有的 token 刷新结果当健康信号 |
| ticket 06：「持久化会话照抄 `pending_auth_sessions`」 | 形态可抄，**语义不可抄**。登录侧的待定会话没有「归属人」这一维（那时用户还没登录），而供给侧的会话必须有：它决定账号最终挂到谁名下。所以 `supplier_oauth_sessions` 多了 `user_id` + `consumed_at`（见 §3.3） |

**关于分成基数的一处更正**（本文档早先版本写错，以此处为准）：曾判定「`service.UsageBillingCommand` 没有 account_cost 字段，accrue 拿不到基数，需新增成本口径字段」。实测后作废——分成基数是**消费者实付**，不是账号成本口径，而实付已经完整地在命令里：`BalanceCost + SubscriptionCost`（见 `gateway_usage_billing.go:277-330`，两者都由 `p.Cost.ActualCost` 赋值，已乘过分组倍率，构造处二选一）。

顺带钉死一处易错点：`AccountQuotaCost = TotalCost × AccountRateMultiplier` 是**账号维度记账口径（近似官方价）**，拿它当分成基数会让供给者按官方价拿 70%，而消费者只按 0.5× 官方价付费——每笔都亏。已有单测 `TestSupplierSettlementBasisUsesConsumerActualPayment` 守住。

因此 `UsageBillingCommand` 上新增的不是成本字段，而是结算**参数**（比例/冻结窗/是否走钱包），见 core touch #3。

## 3. 扩展层落法

沿用 affiliate 扁平文件模式，新文件优先：

- `internal/repository/supplier_*_repo.go` — raw-SQL，用 `dbent.Client.ExecContext/QueryContext` + `withTx`，**不进 ent schema**
- `internal/service/supplier_*.go`
- `internal/handler/supplier_handler.go`
- `internal/server/routes/supplier.go`
- `frontend/src/views/user/SupplierView.vue`、`api/supplier.ts`、`i18n/{en,zh}/supplier.ts`

迁移丢 `.sql` 进 `backend/migrations/` 即被 `//go:embed` 自动加载；含 `CREATE INDEX CONCURRENTLY` 的用 `_notx.sql` 后缀。

wire 注册后跑 `make -C backend generate`（本轮已实测：该命令在本基线上正常工作，wire 与 ent 均标准生成，**不需要手维护 `wire_gen.go`**）。

前端坑（实测）：build 产物 `frontend/dist` 需同步到 `backend/internal/web/dist` 才被 Go embed。

### 3.1 已落地的扩展层模块

| 模块 | 文件 | 迁移 | 说明 |
|---|---|---|---|
| 供给账号归属 | `ent/schema/account.go`（唯一一处 ent 改动） | `224` / `224a_notx` | 见 core touch #1 |
| 赚取钱包 | `internal/service/supplier_credit.go`（类型+接口）、`internal/repository/supplier_credit_repo.go`（SQL） | `225` | `supplier_credits` 余额 + `supplier_credit_ledger` 追加式流水，结构照抄 `user_affiliates` 那套 |
| 计费内结算 | `internal/repository/usage_billing_supplier.go` | — | 基数取值、归属查询、accrue、钱包优先扣减；core 侧只留两处调用（见 core touch #2/#3） |
| 结算参数配置 | `internal/service/setting_supplier.go` | — | 一个 JSON settings key `supplier_settlement_settings`（总开关 / 比例 / 冻结窗 / 是否走钱包）；atomic + singleflight 缓存 60s |
| 参数→计费的接缝 | `internal/service/gateway_supplier_settlement.go` | — | `applySupplierSettlementParams`，core 侧只留一行（见 core touch #3a） |
| 冻结额释放 | `internal/service/supplier_thaw_service.go` | — | 周期扫描（10min / leader lock / 单轮 500 人），照抄 `PaymentOrderExpiryService` 形态 |
| 钱包读侧 | `internal/service/supplier_credit_service.go` | — | 仪表盘用的余额/流水读取，读前顺手懒解冻 |
| 供给池路由配置 | `internal/service/setting_supply_pool.go` | — | 第二个 JSON settings key `supply_pool_settings`（开关 / 供给池分组 id / 溢出目标分组 id / **日溢出配额**），缓存形态同上 |
| 供给池溢出 | `internal/service/gateway_supply_overflow.go` | — | 供给池硬耗尽时在自营池上重跑一轮调度；core 侧只留一个函数改名（见 core touch #7） |
| 溢出日配额与计数 | `internal/service/supply_overflow_budget.go`（闸门+接口）、`internal/repository/supply_overflow_repo.go`（SQL） | `227` | 溢出前先过日配额闸门：判定与计数是**同一条** `ON CONFLICT DO UPDATE ... WHERE` 语句，并发下不会超发；被拦下的次数单独计入 `denied_count`。计数不可用时 fail-closed（不溢出）。见 §3.2 |
| 供给者自助接入 | `internal/service/supplier_onboarding.go`（类型+接口）、`supplier_onboarding_service.go`（编排）、`internal/repository/supplier_onboarding_repo.go`（SQL） | `226` | 持久化 OAuth 会话 + 建号 + 写归属 + 入池；见 §3.3 |
| OAuth 协议层复用 | `internal/service/oauth_service_supplier.go` | — | 把 PKCE 生成与兑换从上游进程内的 `sessionStore` 解耦出来。同包新文件，**core 侵入为零**（`exchangeCodeForToken` 未导出，同包才调得到） |
| 供给侧 HTTP | `internal/handler/supplier_handler.go`、`internal/server/routes/supplier.go` | — | `/api/v1/user/supply/*`：status / oauth 两步 / accounts 增删挂起 / wallet / ledger。中间件与用户面一字不差（JWT + BackendModeUserGuard + 面板限流 + 审计）；所有端点的用户 id **只**取自 JWT，没有一个接受 `user_id` 入参 |
| 观察期参数配置 | `internal/service/setting_supply_probation.go` | — | 第三个 JSON settings key `supply_probation_settings`（自动入池开关 / 最短观察时长 / 连续成功次数 / 探测间隔 / 探测模型 / 排空窗），缓存形态同上。与另外两组**刻意不同**：越界值 clamp 后照存而不是报错，见 §3.5 |
| 观察期与排空 | `internal/service/supplier_lifecycle_service.go` | — | 周期任务（5min / leader lock / 单轮 200 个 / 单轮至多 20 次探测），形态照抄 `SupplierThawService`；两件事：`draining` 到期转 `retired`，`pending_review` 探测达标后入池 |
| 管理端配置 HTTP | `internal/handler/admin/setting_handler_supplier.go` | — | `GET/PUT /api/v1/admin/settings/{supplier-settlement,supply-pool,supply-probation}`，三组配置各自一对端点。方法挂在**既有**的 `*SettingHandler` 上（它已经持有 `settingService`），因此 wire 零改动；路由挂进既有的 `registerSettingsRoutes`（见 core touch #8） |
| 供给侧前端 | `frontend/src/api/supply.ts`、`stores/supply.ts`、`views/user/SupplyView.vue`、`i18n/locales/{zh,en}/supply.ts` | — | 接入页与仪表盘合一页：钱包四格 + 两步授权 + 我的订阅表 + 收益流水分页；下线两条通道（两套确认文案）、`draining` 徽章与排空截止时间、观察期进度（探测次数 / `eligible_at` 用「不早于」语气 / 探测失败原因）。见 §3.4、§3.5 |
| 管理端前端 | `frontend/src/api/admin/supplyMarket.ts`、`views/admin/SupplyMarketView.vue` | — | 单起一页而不是往 `SettingsView.vue`（近九千行、上游最痛的合并热区）里加三个 section；三组配置各自一个保存按钮，审计日志才分得清谁改了什么。池卡片里配额输入框与「今日已溢出 N 次 / 被拦 M 次」挨着显示——单看配额说明不了任何事 |

**结算参数现在有人填了，但默认是关的**。`GetSupplierSettlementSettings` 读那个 JSON key，`ToBillingParams()` 在总开关关闭时返回全零值——也就是计费里「什么都不做」的那一支。配置行不存在（当前生产状态）、JSON 损坏、数据库读失败，一律回退到关闭：这是**刻意的 fail-closed**，与本包其他网关行为设置的 fail-open 相反。网关设置读不到时放行请求最多少一层加工；结算参数读不到时按猜测值给钱，错的是账。打开开关要管理员在「双边市场」页显式保存一次（`PUT /api/v1/admin/settings/supplier-settlement`）。

**为什么冻结额释放必须有后台任务**，而不能照抄 affiliate 的「读仪表盘时顺手解冻」：affiliate 返利额的唯一出口是在面板上看见然后花掉，用户不开面板就用不到它，懒解冻够用。供给者 credit 的主出口是**抵扣自己发起的 API 请求**，那条路径上没有人打开过任何页面——只做懒解冻的话，一个从不登面板、只用 API 的供给者的钱会永远躺在冻结区，每次请求照扣 `users.balance`，功能在他身上等于不存在。所以两条都做：`SupplierThawService` 周期扫描兜底，`SupplierCreditService.GetWallet` 读前即时解冻（低延迟体感）。两者都幂等，同时命中不会多搬一分钱（到期流水被 `UPDATE ... WHERE frozen_until <= NOW()` 一次性摘掉，第二次扫不到）。

赚取钱包的三条设计约束（实现里已钉死，改动前先读）：

1. **幂等闸门先于余额**。`accrue`/`spend` 都是「先插流水后动钱」：流水表上有部分唯一索引 `(action, request_id) WHERE request_id IS NOT NULL`，插不进去就说明这笔已记过账，余额一分不动。Go 侧 `ON CONFLICT` 的推断子句必须与该索引谓词逐字一致，否则 Postgres 直接报错而非降级——已用测试钉住两边一致性。
2. **入账金额服务端现算**（`BasisAmount × ShareRatio`），调用方传不进金额。流水里「基数 × 比例 = 金额」三要素自洽，供给者不必信任服务端算术即可核对。
3. **`spend` 余额不足返回 `false` 而非报错**，计费侧据此回退扣 `users.balance`；重放已扣过的请求返回 `true`（不是 `false`），否则计费侧会转头去扣 balance，同一请求就扣了两处。

写操作都拆成「接受 executor 的包级 `*Tx` 函数」+「自己开事务的方法」两层，core 侧（#2）因此只需要一行调用。

**`clawback`（冻结窗内拒付追回）已落地**（`supplier_credit_repo.go` + `supplier_clawback.go`），调用点在退款成功之后。它把不变量 2 从「数据结构上成立」推到「运营上成立」。五条设计约束，改动前先读：

1. **只动冻结区**。候选集限定 `action='accrue' AND source_user_id=$1 AND frozen_until IS NOT NULL`——已释放的钱按不变量 2 就是拒付安全的，追回它等于毁约。
2. **按笔整撤，不按金额切**。逐条撤销该消费者名下最新的入账直到覆盖退款基数，跨过界的那一条整条撤掉（超撤 ≤ 一个请求的分成）。换来的是一条 accrue 恰好配一条 clawback 的干净配对，也就是幂等的抓手。多撤的部分记在 `ReversedBasis`、撤不满的缺口记在 `UncoveredBasis`，两个数都随日志出来——沉默地少追回才是账不平。
3. **幂等复用已有索引**，不新加迁移：clawback 流水沿用被撤入账的 `request_id`，`(action, request_id)` 上的部分唯一索引天然保证「一条入账只能被撤一次」。候选 SQL 里的 `NOT EXISTS` 与 `frozen_until IS NOT NULL` 是另外两道各自独立的闸。
4. **必须把被撤的入账摘出解冻队列**（`SET frozen_until = NULL`）。只扣 `frozen_credit` 不摘队列的话，解冻任务过后会把同一笔钱再往可用区搬一次——`GREATEST` 只护住了冻结区不为负，可用区会凭空变多。这条 UPDATE 是止损，不是记账，已被 SQL 形状测 + 行为测 + 真库集成测三处钉住。
5. **`history_credit` 保持单调**，追回不减它。它是对账锚点（`history = SUM(accrue.amount)`）；净收益 = `history − SUM(clawback.amount)`，在读侧算。

调用点两处（`markRefundOk` 与 `finalizePendingRefundSuccess`），都是 **best-effort**：走到那一行时钱已经在支付通道退出去了，追回失败只记 `slog.Error`，绝不让一笔成功的退款回滚成「钱退了、系统说没退」。`finalizePendingRefundSuccess` 那处刻意放在 `tx.Commit()` **之后**——事务内报错会把整个退款事务置为 aborted，吞掉错误也救不回来。追回也**刻意不看结算总开关**：关掉开关后冻结区里仍躺着开着时产生的入账，按 `enabled` 短路等于给「先关开关再退款」开一条套利路径。

### 3.2 供给池溢出的四条边界（改动前先读）

**溢出不换价签，这是白捡的**。消费者价来自 `apiKey.Group.RateMultiplier`（`gateway_usage_billing.go:806-813`），而调度用的 group 是调度函数内部的局部变量，从不回传给计费。所以溢出只换供货来源：消费者按自己买的 0.5× 档付钱，平台自吃「按自营成本供货却按供给池价收费」的差额。这既是对的（供给干涸是平台的问题，不该让消费者按 2 倍结账），也不是新造的语义——已有的 claude-code 降级早就在跑同一条路：账号来自 fallback 分组、计价来自 apiKey 分组。

**代价必须是被盯着的指标**。每一次溢出平台都在亏钱供货，所以溢出走 `slog.Warn`（`[SupplyPool] supply pool exhausted`），这条日志的频率就是溢出率，涨起来是要人介入的经营信号。

**配额是这条代价的硬上限**（原先记在这里的「只有日志、没有熔断」遗留风险已关闭）。`supply_pool_settings.daily_overflow_limit` 给溢出加了一个按平台时区自然日结算的次数上限：配额用完后，供给池耗尽的请求拿回它**原本就会拿到**的 `ErrNoAvailableAccounts`，也就是「溢出没开」时的行为，不是新增的故障面。三条实现约束改动前必须读（都在 `supply_overflow_budget.go` 顶部）：

1. **判定与计数是同一次写**。「先读计数再判断再加一」在并发下会超发——配额设 100 时几十个并发的耗尽请求会各自读到 99 然后一起放行。所以配额判定下沉成一条 `INSERT ... ON CONFLICT (day) DO UPDATE SET overflow_count = overflow_count + 1 WHERE overflow_count < $2 RETURNING overflow_count`：有返回行 = 允许，无返回行 = 已满且**没有**写入。判定和加一发生在同一个行锁里。
2. **fail-closed**。计数写失败（数据库抖动、表还没迁移）时不溢出，理由与 `GetSupplyPoolSettings` 读不到时不溢出一致：溢出是要花平台的钱的动作，花钱的决定不能建立在「不知道现在花了多少」之上。**但「计数器没装」不算失败**——那时放行，否则某次装配漏了 provider 会静默地把整个溢出功能关掉，比不装更难查。
3. **被拦下的次数单独计**（`denied_count`）。混进 `overflow_count` 的话，「今天 500 次」既可能是花了 500 次的钱，也可能是省了 500 次，对运营的含义正好相反。

配额消耗发生在**溢出目标解析成功之后**：否则任何一个空分组的耗尽都会去吃供给池的预算。计数落在 Postgres（`supply_overflow_daily`）而不是 Redis，因为同一批数字既是判定依据又是管理端要看的经营读数，重启/驱逐后归零的经营数据不如没有。

**门开得很窄，是防误配不是防滥用**。只有 `supply_pool_settings.supply_group_id` 显式指定的那**一个**分组会溢出，判据用的是 `resolveGatewayGroup` **解析后**的分组（在失败路径上才解析，热路径零成本）而不是 API key 上的原始 id——claude-code 降级会在选号前换掉分组，那种情况下耗尽的是降级分组不是供给池。另外：只在硬耗尽（`ErrNoAvailableAccounts`）时溢出，「有号但都忙」返回的等待计划不触发（那是拥挤不是缺货）；只重试一次不成链；溢出池也空时**返回原始错误**，因为请求打的是消费者自己的分组，报一个指向自营池的错误会把排查的人引到错误的池子上。

### 3.3 自助接入的五条边界（改动前先读）

**新号一定不可调度，而且靠两条独立理由**。`CompleteOAuth` 建号时显式写 `Schedulable: false`（不靠「`AccountService.Create` 恰好不设这个字段」的零值——`adminServiceImpl.CreateAccount` 那条路径就显式设了 `true`，靠零值等于把安全性押在上游不改），并且**先不绑分组**。建号到写归属之间有一个窗口，窗口里这个号没有主人；若此时它能被调度，产生的用量会按自营账号计——供给者干了活拿不到钱，且事后无从追认（`usage_log` 不回溯归属）。顺序 `Create → SetAccountOwner → BindGroups` 由单测钉死。

**`pending_review → active` 只能由后台任务发生，而且默认关着**（原文写的是「本切片没有任何东西做这件事」，#9 落地后改成这条）。放行者是 `SupplierLifecycleService`，判据三条**同时**成立：`supply_probation_settings.enabled` 为真、连续探测成功数达标、`probation_since + 最短观察时长` 已过。默认配置里第一条就是假的——起步形态是「照常探测、照常记录、不自动放行」，运营看几天数据再打开。`ResumeAccount` 仍然**只**把状态改回 `pending_review`、绝不触碰 `schedulable`——否则供给者点一下就能绕过观察期把号推进池子。

**会话领取必须原子**。领取是一条 `UPDATE ... SET consumed_at = NOW() WHERE session_id = $1 AND user_id = $2 AND consumed_at IS NULL AND expires_at > NOW() RETURNING ...`。写成「查出来 → 判断 → 更新」的话，并发重放会让两个请求都通过检查、同一个授权码被兑换两次、建出两个账号。归属人不符 / 已过期 / 已消费 / 不存在四种情况合并成同一个 `ErrSupplierOAuthSessionInvalid`——区分它们等于提供一个枚举他人会话的信息面；同理 `ErrSupplierAccountNotFound` 合并了「不存在」与「不是你的」。已消费的会话行**不删**（过期清理只扫 `consumed_at IS NULL`），它是「这个账号是谁在什么时候挂上来的」的唯一证据。

**上游账号查重不限 owner**。按 `credentials->>'account_uuid'` 查，命中就拒。同一个上游订阅被两个人分别挂上来，正是「一号两卖」的形态——两边按同一份额度各算各的分成，平台按两份供给计价而实际只有一份。**遗留风险**：查重依赖上游在 token 响应里返回 `account.uuid`；返回为空时（协议变更，或某类账号没有该字段）这一层失效，重复接入只能靠观察期人工发现。

**只要 `user:inference` scope**。走 setup-token 而非完整 OAuth scope：平台需要的只是替供给者转发推理请求，完整 scope 还附带读 profile、建 API key、列会话——供给者把订阅挂上来不等于把账号交出来。`code_verifier` 明文入库是可接受的：PKCE verifier 单独无用（必须配一次性授权码），行 15 分钟过期、一次性消费，相对内存方案的差别只是「进程内存」换成「数据库」，在本仓凭证本来就明文存 jsonb 的现状下不是新增短板。要收紧应当连同 `accounts.credentials` 一起做应用层加密，那是独立的一刀。

### 3.4 前端的四条边界（改动前先读）

**菜单可见性走一次按用户的后端调用，不走 featureFlags 注册表**。上游的 `utils/featureFlags.ts` 只吃 public settings，加一个全站开关要改 11 处文件（清单在那个文件顶部）。而我们真正要问的是「这个部署配了供给池吗、这个用户能不能看见入口」——`/user/supply/status` 一次请求就答完。于是单起 `stores/supply.ts`：拉一次并缓存（侧边栏每次路由切换都重挂载，不缓存就是每跳一页打一次后端），并发去重，**失败一律按"没开放"处理**（fail-closed）——把入口画出来、点进去才发现功能没开，比不画更糟。缓存按 userId keyed：换个人登录必须重问，否则上一个人的开关留在菜单上。刻意让 supply store 自己记 userId，而不是让上游 `auth.ts` 的 `clearAuth` 来调 `reset()`——依赖方向朝里指，就不用动 core。

**status 报两个开关而不是一个**。「接入开着、结算关着」是一个真实且合法的状态（供给者能挂号、用量暂不入账），页面必须能解释它，所以 `SupplyStatusResponse` 同时给 `enabled` 与 `settlement_enabled`，后者为假时页面顶部挂一条琥珀色横幅。同理**结算关着时钱包照常返回**：藏起一个余额看上去像钱丢了。

**i18n 只新增命名空间，绝不碰上游的**。locale 模块是被 spread 进同一个对象的，顶层键撞名会**整段替换**掉原来的——初稿里 `supply.ts` 写了个 `nav: {...}`，那会把 `common.ts` 的整个 `nav` 命名空间干掉，静默且大面积。改成 `supply.navLabel` / `supplyAdmin.navLabel`，并用 `i18n/__tests__/supplyLocales.spec.ts` 钉住两件事：zh/en 键集完全一致（缺键在 vue-i18n 里是把 key 画到界面上，不报错），以及上游 `nav` 仍在。

**前端不重复后端的区间校验**。上下限（`share_ratio_max` / `freeze_hours_max`）由 `GET` 响应带下来，本地只留一份兜底初值供后端不可达时渲染；保存后把**后端返回的规范化值**回填进表单，因为写路径会 clamp。抄一份上下限就是给同一条规则立两个源头，后端改了、前端还在按旧值拦。管理端菜单项**不挂 featureFlag**：管理员必须能进到这一页才能把功能打开。

### 3.5 下线双通道与观察期的五条边界（改动前先读）

**两条通道的差别不是「快慢」，而是「能不能反悔」**。`graceful` 与 `immediate` 都在同一次调用里 `SetSchedulable(false)`——**都立刻停止接新单**，包括粘性会话复用（`SetSchedulable` 同时清掉 sticky 复用）。真正的差别只有两个：终态多久到（graceful 等一个排空窗，immediate 当场进 `retired`），以及在那之前能不能取消（graceful 能，immediate 不能）。**两者都停不掉已经在流的请求**——平台没有打断在途 SSE 的能力，排空窗是一段礼貌等待，不是硬排空。界面文案（`supply.accounts.pauseHint`）必须原样保留这句话：供给者唯一会因此投诉的点就是「我点了下线，为什么还在跑」。

**状态机的两次写各自把「中途失败」落在安全的一边**。下线是 `SetSchedulable(false)` **然后**写状态：中途崩了就是「不接单但状态还没变」——保守的那一边，下一轮 sweep 修好。入池反过来，`SetSchedulable(true)` **然后**写 `active`：中途崩了是「已可调度但状态仍是 pending_review」，下一轮 sweep 读到 `schedulable && pending_review` 直接补写状态（这就是 `sweepPendingReview` 里那条「不管开关开没开都先 reconcile」的分支存在的理由）。反过来写的话，两种崩溃场景分别得到「状态说下线了但还在接单」和「状态说上线了但不接单」，前者是事故。

**排空窗到期判断只信 `drain_until`，缺了就立刻收**。`drain_until` 缺失或解析不出来（手工改坏 extra、旧版本遗留的行）一律当作已到期，直接转 `retired`。相反的容错方向会让一个坏字段把号永远钉在 `draining` 这个中间态里——既不接单、也永远不结束、供给者也看不懂。同理**重复点「下线」不延长窗口**：已经在 `draining` 的号再收到一次 graceful 是 no-op，否则一个手抖的双击就把排空窗翻倍。

**探测花的是供给者自己的额度，所以节流有三层**。每次探测是一个真实上游推理请求，账记在供给者头上。三层是：单账号两次探测的最小间隔（配置项，**下限 5 分钟**，读路径也 clamp，一份手工改坏的 `probe_interval_minutes: 0` 不该让任务每秒去戳人家的号）、单轮至多 20 次探测、`status != active` 的号直接跳过（凭证已经坏了，再探也只是重复烧额度）。批次上限或探测预算截断了工作时会 `slog` 记一条——**不做静默截断**，否则日志读起来像是「全扫完了」。

**这一组参数越界时 clamp 而不是报错，与结算参数刻意相反**。理由是错的方向不同：结算参数填错是**算错钱**，必须当场拒绝让人改对；观察期参数填错最多是「观察久一点/短一点」，而拒绝保存会让运营在一个半配好的状态里卡着。代价是运营可能没注意到自己填的 1 分钟变成了 5——所以 `PUT` 返回 clamp 后的值，前端**必须**回填（`SupplyMarketView.vue` 的 `saveProbation` 里那段回填不是可选的），页面上另挂一句 `clampNotice` 说明这个行为。

## 4. Core Touch 台账

core 侵入处一律：逻辑放新文件，core 处只留单行调用 + `// APEXONE-EXT:` 可 grep 标记 + 详尽注释 + 单测。下表随实现逐行填。

| # | 文件 | 位置 | 改动 | 理由 | 状态 |
|---|---|---|---|---|---|
| 1 | `backend/ent/schema/account.go` | 字段区 + 索引区 | 加 `owner_user_id` (Optional/Nillable) 及其 `index.Fields`；迁移 `224` / `224a_notx`（部分索引 `WHERE owner_user_id IS NOT NULL`） | 供给账号归属。NULL = 管理员自建，调度/计费/删除均不读该字段，向后兼容。刻意做成可空标量而非 Required edge，关掉供给池即可退回纯自营网关；外键 `ON DELETE SET NULL`，供给者销号不级联删掉握着上游 OAuth 凭证的账号 | **已做** `22f21e7fa` |
| 1a | `backend/internal/repository/account_repo_upstream_billing_probe_update_test.go` | `updatedAccountRows` :475 | 补一个 `nil` | go-sqlmock 按 `dbaccount.Columns` 位置构行，加字段即 32→33 列，不补会 panic。**交接件未预见**：ent 加字段会牵动所有位置式 sqlmock fixture | **已做** `22f21e7fa` |
| 2 | `backend/internal/repository/usage_billing_repo.go` | `applyUsageBillingEffects` :181 与 :220 | 插入两处各 4 行：balance 分支前 `spendFromSupplierWallet`，函数末尾 `accrueSupplierRevenue`。逻辑全在新文件 `usage_billing_supplier.go` | 决策人裁定结算 accrue 内联绑计费事务（结算正确性 > 合并便利），同事务同 `RequestID` 幂等保证「消耗 === 入账」。钱包扣减必须在 balance 分支**之前**，「同一请求只扣一处」由 `walletPaid` 一个布尔量单点保证 | **已做** `39b90f1a8` |
| 3 | `backend/internal/service/usage_billing.go` | `UsageBillingCommand` 尾部 | 新增 `Supplier UsageBillingSupplierParams`（比例/冻结窗/是否走钱包），全零 = 关闭 | 结算参数须随命令下传：结算发生在计费事务内部，在那里读 settings 既慢又可能读到与本次请求不同的值。**不是**成本字段——分成基数用已有的 `BalanceCost + SubscriptionCost`（见 §2 更正）。已验证 `buildUsageBillingFingerprint` 用显式字段列表，加字段不改指纹；`quantizeMonetaryFields` 未动 | **已做** `39b90f1a8` |
| 3a | `backend/internal/service/gateway_usage_billing.go` | `billingDeps` 结构体 + `billingDeps()` + `applyUsageBilling` 内 | 加一个 `settingService` 字段及其赋值，`buildUsageBillingCommand` 之后加一行 `applySupplierSettlementParams`。逻辑全在新文件 `gateway_supplier_settlement.go` | 参数取一次快照随命令走完全程。刻意**不**放进 `buildUsageBillingCommand`：那是个纯函数（无 ctx、无依赖），保持它纯的，单测就不必为造一条命令而准备一个 `SettingService`。结算参数不参与请求指纹也不参与金额量化，晚于 `Normalize()` 赋值安全 | **已做** |
| 4 | `backend/internal/server/router.go` | :117-132 调用块 | 加 `routes.RegisterSupplierRoutes(...)` 一行 + 一行 `// APEXONE-EXT:` 注释 | 供给侧用户路由入口。路由**单起 `routes/supplier.go`** 而非往 `routes/user.go` 里插一段：那个文件是上游合并热区，独立文件把路由层的侵入压到这一行 | **已做** |
| 5 | `backend/internal/service/wire.go` + `internal/repository/wire.go` | 各 ProviderSet | `NewSupplierCreditRepository` / `NewSupplierCreditService` / `ProvideSupplierThawService` 三个注册 | wire 标准注册点 | **已做** |
| 5a | `backend/cmd/server/wire.go` | `provideCleanup` 形参 + 停机步骤 | 加 `supplierThaw *service.SupplierThawService` 及其 `Stop()` 步骤；`wire_gen.go` 重新生成（+12 行） | 既为优雅停机，也是**为了让 wire 真的把它构造出来**——`Provide*` 里调了 `Start()`，没有任何消费者引用它 wire 就会把整个 provider 剪掉，任务永远不启动。`wire_gen_test.go` 的 `provideCleanup` 调用同步补一个 `nil` | **已做** |
| 5b | `backend/internal/handler/handler.go` + `handler/wire.go` | `Handlers` 结构体尾部、`ProvideHandlers` 形参与返回值、`ProviderSet` | 加 `Supplier *SupplierHandler` 字段 + 一个形参 + 一个 provider；`repository/wire.go` 与 `service/wire.go` 各加一个 provider；`wire_gen.go` 重新生成（+4 行） | wire 标准注册点。形参插在 `batchImageHandler` 之后、两个 `_` 占位参之前，避开尾部占位约定 | **已做** |
| 6 | `frontend/src/router/index.ts`、`i18n/locales/{en,zh}/index.ts`、`components/layout/AppSidebar.vue` | 路由表两处插入；locale 桶各 +2 行；侧边栏 +6 行 | 两条路由（`/supply`、`/admin/supply-market`）、两个 locale 模块 import+spread、两个菜单项 + 一个 `DollarIcon` + `onMounted` 里一行 `ensureStatus()` | 前端冲突热区，改动全是**追加**：locale 只 spread 新命名空间（不碰上游任何一个，见 §3.4），路由项插在既有项之间，菜单项各一条。`/supply` 不设路由守卫——功能没开的状态由页面自己画，守卫拦掉只会给用户一个没有解释的 404 | **已做** |
| 5c | `backend/internal/service/wire.go` + `backend/cmd/server/wire.go` | ProviderSet / `provideCleanup` 形参 + 停机步骤 | 加 `ProvideSupplierLifecycleService` 注册；`provideCleanup` 加一个形参与一个 `Stop()` 步骤；`wire_gen.go` 重新生成（+9 行），`wire_gen_test.go` 同步补一个 `nil` | 与 #5a 一模一样的理由：provider 里调了 `Start()`，没有消费者引用 wire 就会把它整个剪掉，观察期任务永远不启动。两个后台任务因此都必须出现在 `provideCleanup` 的形参里 | **已做** |
| 5d | `backend/internal/service/wire.go` + `backend/internal/repository/wire.go` | `ProvideSettingService` 形参 / ProviderSet | `ProvideSettingService` 多一个 `SupplyOverflowCounter` 形参并在里面调 `SetSupplyOverflowCounter`；repository ProviderSet 注册 `NewSupplyOverflowCounter`；`wire_gen.go` 重新生成（+1 行） | 计数器是进程级单例（包级 `atomic.Value`），刻意**不**加在 `SettingService`/`GatewayService` 的结构体上——那两个文件是每轮 upstream sync 的合并热区，为一个扩展字段每次都要解一遍冲突。代价是需要一个注入点，而 `ProvideSettingService` 是 `SettingService` 唯一的构造处，读用量的方法也挂在它上面 | **已做** |
| 5e | `backend/internal/service/wire.go` + `backend/internal/service/payment_refund.go` | ProviderSet；`markRefundOk` 与 `finalizePendingRefundSuccess` 各一行 | `NewSupplierCreditService` 换成 `ProvideSupplierCreditService`（内部调 `SetSupplierClawbackHandler`）；退款两处收敛点各插一行 `clawbackSupplierCreditOnRefund`。逻辑全在新文件 `supplier_clawback.go`；`wire_gen.go` 重新生成（1 行） | 追回入口同 #5d 做成进程级单例，不往 `PaymentService` 上加字段（合并热区）。**但不能照 #5a/#5c 那样单起一个 provider**：没有消费者引用它 wire 会整个剪掉——所以挂在 `SupplierCreditService` 的构造里，那个结果被 `SupplierHandler` 消费，必定被构造。退款侧那一行是 best-effort 且**在事务外**：事务内报错会把整个退款事务置为 aborted，吞掉错误也救不回来 | **已做** |
| 8 | `backend/internal/server/routes/admin.go` | `registerSettingsRoutes` 末尾 | 追加六行路由 + 一行 `// APEXONE-EXT:` 注释 | 挂进既有 settings group 而不是自起一个 admin group：那个 group 上有 adminAuth + 面板限流 + 审计 + `AdminComplianceGuard` 四层中间件，复制一份等着它日后与上游漂移，而漂移掉的是合规相关的那几层。handler 方法挂在既有的 `*SettingHandler` 上，因此 wire、`AdminHandlers`、`ProvideAdminHandlers` 三处热区一处没动 | **已做** |
| 7 | `backend/internal/service/gateway_scheduling.go` | :97 函数签名 | **仅函数改名**：`SelectAccountWithLoadAwareness` → `selectAccountInPoolWithLoadAwareness`，函数体一字未改。导出名归新文件 `gateway_supply_overflow.go` 里的同名包装函数 | 交接件说的「零 core 溢出」不成立（见 §2）。耗尽有十几个 return 点，逐个插判断等于把一条新规则摊成十几处侵入；改名 + 外层包装是**一处**。合并冲突面小到只有一行签名，且上游若改了函数体，冲突会照常落在函数体上而不被包装掩盖 | **已做** |
| 7a | — （**未覆盖面，记账用**） | `openai_codex_models_handler.go:44`、`gateway_handler.go:2058` | 这两处走的是 `SelectAccountForModel`，不经包装函数，因此**不溢出** | 首版切片的供给池只面向 Claude Code 形态消费，这两条路径不在范围内。若日后供给池扩到 codex 形态，需在此处补同样的包装 | 已知缺口 |

## 5. 结算不变量（实现必须守住）

1. **消耗 === 入账**：failover 后只有产出被计费响应的账号入账（以 `usage_log.account_id` 为准）；失败/重试/无计费产出零入账。accrue 幂等键 = `usage_log.RequestID`。
2. **冻结窗 ≥ 拒付窗**：使「已释放 = 拒付安全」。冻结期内拒付 → 从供给者冻结 earnings 追回（`SupplierCreditRepository.Clawback`，退款成功后触发）；释放后拒付平台自吃。冻结窗配小了不会报错，只会让追回**撤不满**——缺口体现为 `UncoveredBasis > 0` 与一条 `slog.Warn`，这是这条不变量在运营期唯一的可观测信号。
3. **同一请求只扣一处**：赚取钱包或 `users.balance`，由 `applyUsageBillingEffects` 单点保证。
4. **审计快照**：ledger 每条带快照，供给者可自行核对计量。

## 6. 待定参数（fog，实现期取默认值、运营期调）

前四个已经是**真实可配的**了，落在 settings key `supplier_settlement_settings` 里（见 §3.1）；表里的「拟定默认」就是 `DefaultSupplierSettlementSettings()` 的值，且总开关 `enabled` 默认 `false`。

第二个 key `supply_pool_settings`（`enabled` / `supply_group_id` / `overflow_group_id` / `daily_overflow_limit`）同样默认关，装的是路由而不是钱。第三个 key `supply_probation_settings`（见 §3.5）装的是准入与下线时序，`enabled` 同样默认关。**刻意分成三个 key**：这三组配置因完全不同的原因变动（动分成比例 / 动兜底池 / 动准入门槛），合成一个会让三次意图完全不同的修改共用一条审计记录。

三个 key 都已有管理端 API 与界面（`/admin/supply-market` 页，三张卡各自一个保存按钮）。

| 参数 | 拟定默认 | 说明 |
|---|---|---|
| `enabled` | `false` | 总开关。默认关就是上线策略：代码先随版本进生产、在计费主链路上待着，管理员显式打开才开始动钱 |
| `share_ratio` | 0.70 | 供给者分成，基数 = 消费者实付（非官方价） |
| `spend_from_wallet_first` | `false` | 与 `enabled` 分开是有意的：可以先只开入账让供给者攒着，等钱包侧观察稳了再打开消费出口 |
| 供给池 `rate_multiplier` | 0.5 | 消费者价 = 0.5× 官方价。**不在这两个 key 里**，是分组自己的 `rate_multiplier` 字段，管理端分组页配 |
| `freeze_hours` | `168`（7 天，**占位**） | 须 ≥ 支付通道拒付窗。7 天只是能跑通流程的占位值，**上线前必须按 Stripe/支付方实际拒付窗重设**（Stripe 争议窗远长于 7 天），否则不变量 2「已释放 = 拒付安全」不成立。代码侧只 clamp 到 90 天上限，拦不住「配小了」——这一条只能靠人 |
| 自营池 `rate_multiplier` | `1.0`（**占位**） | 需覆盖真实 API 成本 + 毛利，上线前重设 |
| `daily_overflow_limit` | `0`（不限量，**待定**） | 溢出的日次数上限，在 `supply_pool_settings` 里。默认 0 = 不限量但照常计数——先跑几天看真实溢出量再定值，凭空拍一个上限会在供给正常波动时误伤。打开溢出后应尽快设成一个可预算的数：这是「按自营成本供货却按供给池价收费」这笔亏损的唯一硬上限 |
| 观察期强度/时长 | `enabled=false`、60 分钟、连续 2 次、探测间隔 15 分钟、排空窗 10 分钟 | 已参数化，落在 `supply_probation_settings` 里。默认**不自动入池**：先让它探测、记录，运营看几天再打开。这些数字是能跑通流程的起步值，不是标定过的 |
| 每 IP 账号数上限 N | **待定** | 起步压到住宅户级小 N，按 IP 封禁率动态收紧 |

## 7. 上游合并检查单

每次同步上游执行：

1. 建 `sync/upstream-vX.Y.Z` 分支 → merge tag（**不合 tip**）→ PR
2. 更新 `backend/cmd/server/VERSION`（注意：上游 tag `vX.Y.Z` 里的 VERSION 文件通常滞后一版，以 tag 号为准）
3. `make -C backend generate` 重生成 wire + ent 并验证 build；检查是否产生 drift 提交
4. `grep -rn "// APEXONE-EXT:"` 对照 §4 台账逐处复核
5. stub 全量核查（上游给接口加方法时，查全部实现）
6. 前端查冲突热区：`router/index.ts`、i18n 桶、layout 组件、tailwind/vite config、auth 视图（品牌串 `ApexOne` 常与上游冲突）
7. 验证 `frontend/dist` → `backend/internal/web/dist` embed 同步
8. 跑 `go build ./...`、`go test -tags unit ./...`、backend-ci

### v0.1.177 同步实录（2026-08-18）

- 规模：719 files, +66,088 / −4,547；fork 侧 59 个自有提交
- 冲突 7 处，全部为品牌/测试层面，无业务逻辑冲突：
  - `Makefile` — union（保留 TEE 的 `tdxVerify.liveQuote.spec.ts` + 上游新增 channel-monitor-v2 三个 spec）
  - `frontend/src/views/auth/{RegisterView,EmailVerifyView}.vue` — 取上游 captcha 变量块，恢复 fork 的 `ApexOne` 品牌串
  - `frontend/src/views/admin/__tests__/GroupsView.{columnSettings,duplicate}.spec.ts` — 取上游（其 hoisted `getLiveCapability` mock 已覆盖 fork 内联 mock 防的同一个 unhandled-rejection 坑），保留 fork 的说明注释
  - `frontend/src/api/__tests__/admin.system.rollback.spec.ts` — 取 fork（`UPDATE_REQUEST_TIMEOUT_MS` 常量仍在且等于上游内联的 `15*60*1000`）
  - `backend/internal/payment/provider/stripe_test.go` — add/add union（fork 的 crypto 支付测试 + 上游的 refund 幂等键测试，import 合并）
- 验证：`go build ./...` 通过；`go test -tags unit ./...` 全绿（exit 0）；`make -C backend generate` 通过，产生一处上游自身的 ent 注释 drift（`ent/group.go` 的 `long_context_pricing_enabled` 注释），已单独提交

## 8. 真库验证实录（2026-08-19）

在此之前，五个新迁移**一次都没在真 Postgres 上跑过**——本仓的 sqlmock 测试只验语句形状，验不了列名是否存在、索引谓词是否可推断、并发下的行锁语义。这一轮把它们全部跑通。

**环境**：`go test -tags integration`，testcontainers 起 `postgres:18.1-alpine3.23` + `redis:8.4-alpine`（TestMain 里 `ApplyMigrations` 失败会直接 `os.Exit(1)`，所以"测试跑起来了"本身就是迁移全部成功的证明）。

**结果**：迁移 `224` / `224a_notx` / `225` / `226` / `227` 全部成功应用；`internal/repository`、`internal/server/routes`、`internal/middleware` 三个带 integration 标签的包全绿。

本轮补上的集成测试（此前是空白）：

| 迁移 | 文件 | 只有真库能证的性质 |
|---|---|---|
| 225 | `supplier_credit_repo_integration_test.go`（+3） | 追回只动冻结区、`history_credit` 不变；追回幂等靠 `(action, request_id)` 部分唯一索引；**追回后再跑解冻搬不动那笔钱**——clawback 与 thaw 两条写路径唯一会打架的地方 |
| 226 + 224 | `supplier_onboarding_repo_integration_test.go`（7 例，新文件） | 会话一次性领取（`UPDATE ... WHERE consumed_at IS NULL RETURNING` 的行锁语义）；拿别人 session_id 领不走且**失败的领取不会把会话标记成已消费**（否则是一条免费的拒绝服务）；过期判定用数据库时钟；已消费的会话不被过期清理带走；`owner_user_id` 写一次即定、不能从 A 改成 B；按状态扫号时**状态缺失兜底成 `pending_review`**（拼接 SQL 与 service 常量的契约，漂移了是静默失效）；上游 uuid 查重不看归属也不看 `schedulable` |
| 227 | `supply_overflow_repo_integration_test.go`（5 例，新文件） | **并发下不超发**：60 个 goroutine 抢 10 次配额，放行数必须恰好 10。这是那条 `ON CONFLICT DO UPDATE ... WHERE` 存在的全部理由，写成"先 SELECT 再 UPDATE"在这个测试里必挂。另有配额跨日重置、被拒次数单独计、不限量时照常计数 |

并发那一例刻意**不走 `testEntTx`**：那个 harness 是一个会回滚的单事务，会把所有写串行化，正好掩盖掉要测的东西。它用 `integrationEntClient` 直连并自行清理。

**仍未验证的**（不在真库能力范围内，需要活的上游）：真实 OAuth 授权码兑换、多实例选主（`SupplierThawService` / 观察期任务的 leader lock 只有单进程假锁测试）。
