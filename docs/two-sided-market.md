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

**在范围**：`account.owner_user_id`、赚取钱包 ledger、内联结算 accrue、thaw 释放、供给池/自营池配置与溢出、供给者自助 OAuth 接入、供给者仪表盘、下线双通道与观察期、解绑与令牌吊销、供给者协议、**提现（申请 → 人工打款 → `withdraw` 流水）**。

**出范围（后续刀次）**：新供给者保底 floor（ticket 14 A/B）、自动打款（提现的打款动作是人工的，平台不接支付通道出金）、封禁率报表与平台熔断、自动代理池、拟人化时段节流、订阅档位识别、User 信誉分、连接级 draining、API key 型供给。

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
| 供给侧 HTTP | `internal/handler/supplier_handler.go`、`internal/server/routes/supplier.go` | — | `/api/v1/user/supply/*`：status / oauth 两步 / accounts 增删挂起 / wallet / ledger。中间件与用户面一字不差（JWT + BackendModeUserGuard + 面板限流 + 审计），这个"一字不差"由 `supplier_test.go` 逐字比对钉住（见 §9）；所有端点的用户 id **只**取自 JWT，没有一个接受 `user_id` 入参 |
| 接入上限配置 | `internal/service/setting_supply_onboarding.go` | — | 第六个 JSON settings key `supply_onboarding_settings`（每人账号数上限 / 每来源 IP 账号数上限），缓存形态同上。两个字段都以 **0 = 不限** 为约定，越界 clamp 后照存（与观察期同侧）。刻意不折进 `supply_probation_settings`：那个 key 的 `enabled` 意思是「自动入池」，而上限必须在它关着时照样生效，见 §3.3 |
| 接入来源记录 | `internal/repository/supplier_onboarding_repo.go`（三条 SQL）、`internal/service/supplier_onboarding_service.go`（`requireCapacity`） | `230` | `supplier_account_origins`：一个号是从哪个出口 IP 挂上来的。每 IP 上限的判据。**刻意无外键**——本仓销号是软删，`ON DELETE CASCADE` 永不触发，那条 COUNT 因此 `JOIN accounts` 排掉已删的号。见 §3.3 |
| 观察期参数配置 | `internal/service/setting_supply_probation.go` | — | 第三个 JSON settings key `supply_probation_settings`（自动入池开关 / 最短观察时长 / 连续成功次数 / 探测间隔 / 探测模型 / 排空窗），缓存形态同上。与另外两组**刻意不同**：越界值 clamp 后照存而不是报错，见 §3.5 |
| 观察期与排空 | `internal/service/supplier_lifecycle_service.go` | — | 周期任务（5min / leader lock / 单轮 200 个 / 单轮至多 20 次探测），形态照抄 `SupplierThawService`；两件事：`draining` 到期转 `retired`，`pending_review` 探测达标后入池 |
| 管理端配置 HTTP | `internal/handler/admin/setting_handler_supplier.go` | — | `GET/PUT /api/v1/admin/settings/{supplier-settlement,supply-pool,supply-probation,supply-agreement,supply-withdrawal,supply-onboarding}`，六组配置各自一对端点。方法挂在**既有**的 `*SettingHandler` 上（它已经持有 `settingService`），因此 wire 零改动；路由挂进既有的 `registerSettingsRoutes`（见 core touch #8） |
| 供给侧前端 | `frontend/src/api/supply.ts`、`stores/supply.ts`、`views/user/SupplyView.vue`、`i18n/locales/{zh,en}/supply.ts` | — | 接入页与仪表盘合一页：钱包四格 + 两步授权 + 我的订阅表 + 收益流水分页；下线两条通道（两套确认文案）、`draining` 徽章与排空截止时间、观察期进度（探测次数 / `eligible_at` 用「不早于」语气 / 探测失败原因）。见 §3.4、§3.5 |
| 管理端前端 | `frontend/src/api/admin/supplyMarket.ts`、`views/admin/SupplyMarketView.vue` | — | 单起一页而不是往 `SettingsView.vue`（近九千行、上游最痛的合并热区）里加三个 section；三组配置各自一个保存按钮，审计日志才分得清谁改了什么。池卡片里配额输入框与「今日已溢出 N 次 / 被拦 M 次」挨着显示——单看配额说明不了任何事 |
| 管理端运营视图 | `internal/service/supplier_admin.go`（类型+接口）、`supplier_admin_service.go`（夹取+白名单）、`internal/repository/supplier_admin_repo.go`（SQL）、`internal/handler/supplier_admin_handler.go` | — | `GET /api/v1/admin/supply/{overview,suppliers,accounts,ledger}`，回答运营的四个问题：谁挂了几个号 / 这个月要付多少 / 谁的号在被封 / 哪些卡在观察期。**整层只读**，core 侵入一行（见 core touch #9）。见 §3.6 |
| 运营视图前端 | `frontend/src/api/admin/supplyMarket.ts`（新增只读那一组）、`views/admin/SupplyOperationsView.vue`、`i18n/locales/{zh,en}/supply.ts` 的 `supplyOps` 命名空间 | — | 与配置页 `SupplyMarketView.vue` **分开一页**：那页改参数、这页只看数，合成一页会让人一边翻名册一边动了分成比例。看板四格 + 状态桶（点一下就把账号表筛过去）+ 名册 + 账号明细 + 全站流水 |
| 提现（供给者侧） | `internal/service/supplier_withdrawal.go`（类型+接口）、`supplier_withdrawal_service.go`（编排）、`internal/repository/supplier_withdrawal_repo.go`（SQL） | `229` | `supplier_withdrawals` 单据表。**申请即扣款**：提交那一刻金额就从可用余额扣走并落一条 `withdraw` 流水，审批只推进状态。见 §3.7 |
| 提现通知 | `internal/service/supplier_withdrawal_notify.go`（三封信）、`supplier_withdrawal_service.go`（三个调用点）、`setting_supply_withdrawal.go`（`notify_emails`） | — | 新申请通知运营 + 给供给者发扣款回执，终态通知供给者。全程 best-effort、异步、不用请求 ctx，邮件里不含收款账号。见 §3.7 |
| 提现参数配置 | `internal/service/setting_supply_withdrawal.go` | — | 第五个 JSON settings key `supply_withdrawal_settings`（开关 / 起提额 / 每人未决单上限 / 收款渠道白名单 / 给供给者的说明 / **运营通知收件人**）。越界**报错不 clamp**，与结算参数同侧、与观察期参数相反，理由见 §3.7 |
| 提现 HTTP | `internal/handler/supplier_withdrawal_handler.go`、`internal/handler/admin/setting_handler_supplier.go`、`internal/server/routes/supplier.go` | — | 供给者侧四条（`options` / 列表 / 申请 / 撤回，申请那条额外挂 `panelRateLimiter.Heavy()`）；管理端三条（列表 / 标记已打款 / 拒绝），方法挂在 `*SupplierAdminHandler` 上；设置一对挂在既有 `*SettingHandler` 上。返回视图剥掉 `user_id`/`reviewer_id` |
| 提现前端 | `frontend/src/api/supply.ts`、`api/admin/supplyMarket.ts`、`views/user/SupplyView.vue`、`views/admin/SupplyOperationsView.vue`、`views/admin/SupplyMarketView.vue` | — | 供给者侧一张提现卡（在收益与接入之间）+ 申请记录表；运营页在看板下方加一段审批队列——**这是运营页唯一的写路径**，§3.6 的"整层只读"因此改成"只读 + 这一个例外"；配置页第五张卡 |
| 收款账号密文 | `internal/repository/supplier_payout_cipher.go`（seal/open）、`supplier_withdrawal_repo.go`（一处 seal、一处 open） | `232` | `payout_account` 按 AES-256-GCM 入库，值形如 `enc.v1:` + base64(nonce\|\|ct\|\|tag)，复用既有 `SecretEncryptor`（密钥即 `TOTP_ENCRYPTION_KEY`），**不引入第二套密钥管理**。加解密钉在仓储边界（全表只有一处 INSERT、一处 Scan），任何日后新增的读路径自动解密。无前缀者视为 232 之前的历史明文原样放行，故**不需要停机窗口也不需要回填**。见 §3.7 |
| 对账导出 | `internal/service/supplier_export.go`（类型+接口）、`supplier_export_service.go`（时间窗+计数）、`internal/repository/supplier_export_repo.go`（SQL）、`internal/handler/supplier_export_csv.go`（编码）、`supplier_export_handler.go`（HTTP） | — | `GET /api/v1/admin/supply/export/{withdrawals,ledger}`，**流式** CSV。金额一律 `::text` 取 NUMERIC 原文（全仓唯一一处不走 `::double precision` 的读路径），文件末尾必有一行 `#` 尾行报告行数/窗口/是否截断，上限 20 万行、默认窗口 90 天。收款账号在文件里是**明文**——那份文件就是打款工作单。见 §3.9 |
| 争议台账 | `internal/service/payment_dispute.go`（类型+接口+编排）、`internal/repository/payment_dispute_repo.go`（SQL） | `231` | `payment_disputes`：拒付的唯一事实来源。`settled_at` 是"扣钱只跑一次"的闸，重复 upsert 碰不到它。见 §3.8 |
| 争议事件解析 | `internal/payment/provider/stripe_dispute.go`、`internal/payment/dispute.go`（类型与 `DisputeAwareProvider` 接口） | — | 五种 `charge.dispute.*` 都认，**款项动没动只看 `status`**：四种 warning/prevented 是询证，返回 `(nil, nil)`；未知状态一律按"钱没动"。core 的 Stripe provider 一行未动（同包新文件） |
| 争议 webhook 分派 | `internal/handler/payment_webhook_dispute.go` | — | 挂在既有 webhook 的 `notification == nil` 分支上，不新开路由（Stripe 一个 endpoint 收全部事件）。core 侵入两行，见 core touch #10 |
| 供给号失效台账 | `internal/service/supplier_incident.go`（类型+接口）、`supplier_incident_service.go`（扫描/报表/熔断三个出口）、`internal/repository/supplier_incident_repo.go`（SQL） | `233` | `supplier_account_incidents`：一行 = 一次失效。开与关是**互补**的两条判据，一个号同时只能有一条未结事件（部分唯一索引 + `ON CONFLICT ... DO NOTHING`）。由 `SupplierLifecycleService` 每轮驱动，见 §3.10 |
| 失效通知 | `internal/service/supplier_incident_notify.go` | — | 「你的号停了」，**只发供给者不发运营**、**同步**发（`notified_at` 的含义是信真的发出去了）、信里不含上游错误原文。缺 SMTP 或缺用户读取能力时通知器构造成 nil，检测与报表照常工作。见 §3.10 |
| 接入熔断 | `internal/service/supplier_incident_service.go`（`GuardOnboarding`）、`setting_supply_onboarding.go`（两个新字段） | — | 反复往平台塞坏号的人被挡在接入路径上。`supply_onboarding_settings` 加 `max_incidents_per_user`（默认 **0 = 关**）与 `incident_window_hours`（默认 7 天）。判据放在事件服务而不是接入服务：接入服务不需要知道事件是什么。**查询失败放行**，与另外两道闸相反，理由见 §3.10 |
| 失效运营视图 | `internal/handler/supplier_incident_handler.go`、`internal/server/routes/supplier.go` | — | `GET /api/v1/admin/supply/incidents{,/summary}` 两条只读路由，方法挂在既有的 `*SupplierAdminHandler` 上（同 §3.6）。明细可按供给者 / 账号 / 是否未结 / 时间窗筛；报表是四个数 + 封禁率榜单 |
| 争议通知 | `internal/service/payment_dispute_notify.go` | — | 三种状态三封信，收件人复用 `supply_withdrawal_settings.notify_emails`。全程 best-effort、异步、不用请求 ctx。信里必须写清**收信人现在该做什么**——应诉窗只有几天 |

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

### 3.3 自助接入的九条边界（改动前先读）

**协议门禁在最前面，而且没发布 = 整条自助接入关着**。挂号这件事是在把一份带订阅的上游账号交给平台代为调度，谁承担什么、钱怎么算、平台什么时候能停你的号，这些只能落在一份供给者点过头的文本上；没有它，第一次纠纷时双方各说各话。`requireAgreement` 因此挡在 `StartOAuth` 与 `CompleteOAuth` 两处，三条边界：**未发布是拒绝而不是放行**（`ErrSupplierAgreementNotConfigured`，管理端把 `supply_agreement_settings.version` 留空就等于关掉自助接入——所以后台那张卡片把「已发布 / 未发布」摆在最上面，否则运营会去查为什么没人挂号）；**`CompleteOAuth` 里的检查必须早于 `ClaimSession`**，领取是一次性的，先领后拒等于把供给者那个只能用一次的授权码烧掉，让他重走一遍 OAuth 才发现自己还没签字，这条顺序由单测钉死；**读设置失败一律 fail-closed**，协议读不出来时放行等于在证据缺失的情况下继续收货。

同意的证据是 `(user_id, version)` 上的唯一行，`ON CONFLICT DO NOTHING` 保留最早的一条——重复点击不该把时间戳往后推，出纠纷时要答的是"他最早什么时候同意的"。前端提交的 `version` 是**页面上正在显示的那一版**而不是让服务端自取当前版本：后者只能证明他点了按钮，证明不了他看的是哪一版；版本对不上回 `SUPPLIER_AGREEMENT_VERSION_MISMATCH`，界面该做的是重新拉一遍让他再读，而不是重试。因此改版本号 = 让所有人重新同意一次（改错别字也算），这句话必须印在管理端输入框边上。协议正文按**纯文本**渲染，绝不进 `v-html`——那是一个后台可编辑字段，当 HTML 渲染就是一个存储型 XSS 入口。界面上「从没同意过」与「同意过旧版」两种状态文案必须不同：对一个明明点过同意的人说「请先同意」，他会以为系统坏了。这一组设置越界时后端**报错拒绝**而不像观察期参数那样静默夹回——协议文本被截断一半是没法接受的。

**新号一定不可调度，而且靠两条独立理由**。`CompleteOAuth` 建号时显式写 `Schedulable: false`（不靠「`AccountService.Create` 恰好不设这个字段」的零值——`adminServiceImpl.CreateAccount` 那条路径就显式设了 `true`，靠零值等于把安全性押在上游不改），并且**先不绑分组**。建号到写归属之间有一个窗口，窗口里这个号没有主人；若此时它能被调度，产生的用量会按自营账号计——供给者干了活拿不到钱，且事后无从追认（`usage_log` 不回溯归属）。顺序 `Create → SetAccountOwner → BindGroups` 由单测钉死。

**归属人失效的号必须停下来，而且只能靠周期扫描发现**。用户注销走的是软删（`user_repo.deleteUser` 清掉认证身份后走 mixin 的软删），所以 `accounts.owner_user_id` 上那条 `ON DELETE SET NULL` 一次也不会触发；用户被停用更是完全不碰 accounts。两种情况下账号行一个字节都没变——照常可调度、照常消耗那个人的上游订阅额度，而他已经登不进来。`SupplierLifecycleService.sweepUnavailableOwners` 是唯一的发现途径，判据两条独立（`u.deleted_at IS NOT NULL` **或** `u.status <> 'active'`），漏掉任一条都是静默放行。三条边界：**只停供货，不撤已入账的钱**（那是已交付服务的债，与 §3.6「注销用户仍可能是债主」同源）；**排在其余两个 sweep 之前**（它是闸，后两个是推进器，反过来会让同一轮里刚停掉的号被对齐规则推回池子）；**已 retired 且不可调度的行不返回**（降噪），但 `retired` 却仍 `schedulable` 的行必须返回——那是"状态写着已下线、实际还在接单"的一类，是这条闸真正要抓的。恢复后不自动放回，供给者需自己 Resume 重走观察期。

**`pending_review → active` 只能由后台任务发生，而且默认关着**（原文写的是「本切片没有任何东西做这件事」，#9 落地后改成这条）。放行者是 `SupplierLifecycleService`，判据三条**同时**成立：`supply_probation_settings.enabled` 为真、连续探测成功数达标、`probation_since + 最短观察时长` 已过。默认配置里第一条就是假的——起步形态是「照常探测、照常记录、不自动放行」，运营看几天数据再打开。`ResumeAccount` 仍然**只**把状态改回 `pending_review`、绝不触碰 `schedulable`——否则供给者点一下就能绕过观察期把号推进池子。

**会话领取必须原子**。领取是一条 `UPDATE ... SET consumed_at = NOW() WHERE session_id = $1 AND user_id = $2 AND consumed_at IS NULL AND expires_at > NOW() RETURNING ...`。写成「查出来 → 判断 → 更新」的话，并发重放会让两个请求都通过检查、同一个授权码被兑换两次、建出两个账号。归属人不符 / 已过期 / 已消费 / 不存在四种情况合并成同一个 `ErrSupplierOAuthSessionInvalid`——区分它们等于提供一个枚举他人会话的信息面；同理 `ErrSupplierAccountNotFound` 合并了「不存在」与「不是你的」。已消费的会话行**不删**（过期清理只扫 `consumed_at IS NULL`），它是「这个账号是谁在什么时候挂上来的」的唯一证据。

**上游账号查重不限 owner，且身份键是分层的**。同一个上游订阅被挂两次（自己挂两遍，或被两个人分别挂），是这套系统里唯一一个能凭空造钱的口子——两边按同一份额度各算各的分成，平台按两份供给计价而实际只有一份。所以查重不看归属，命中任何一个身份键就拒。

身份键按强度排：`account_uuid` → `email_address`（`service.SupplierIdentityKeys`）。三条规则由单测钉死：

- **查所有可用的键，不是只查最强的那个**。上游只给了邮箱、没给 uuid 时，如果只认 uuid 就等于放行。
- **一个键都拿不到时是拒绝，不是放行**。`ErrSupplierAccountIdentityUnavailable` 是刻意的拒绝而非故障兜底：没有身份键就查不了重，让供给者重走一遍授权，比放进来一个查不了重的号便宜得多。查重出错同样 fail-closed，不吞。
- **`org_uuid` 刻意不在键里**。团队组织下多个席位共享同一个 org，拿它查重会把合法供给误判成重复——这类误报是静默挡供给，比漏判更难发现。

邮箱比对走 `LOWER(...) = LOWER(...)`：上游把 `Foo@x.com` 与 `foo@x.com` 当同一个账号，按字节比等于「改一下大小写就能把同一份订阅再挂一遍」。键名**绝不拼进 SQL**——`supplierIdentitySQL` 用 switch 把键映射到两条写死的语句，未知键返回空串→报错，于是「加了键但忘了写语句」是响的，而不是一道永远放行的闸。

**解绑必须真的把凭证删掉，而顺序是「停调度 → 抹凭证 → 摘号」**。下线（§3.5）只是不再派单，那份 refresh token 仍然躺在 `accounts.credentials` 里——供给者没有任何办法把授权收回去，整套东西只能靠「相信平台不会用」立着，这对一个双边市场不成立。`DetachAccount` 因此是唯一不可逆的一刀：`SetSchedulable(false)` → `ScrubAccountCredentials`（`credentials = '{}'::jsonb`，同时把 `apexone_supply_state` 写成 `retired` 并记一个 `apexone_supply_detached_at`）→ `accountRepo.Delete`。**2 在 3 之前是强制的**：抹凭证的 `WHERE` 带 `deleted_at IS NULL`，一旦先软删，那条语句此后永远匹配不到，token 就留在一行看不见的记录里长期存在；这条顺序由单测钉死。三件事刻意不做：**不调上游撤销**（Anthropic 没有公开的 revoke 端点，往一个猜出来的 URL 发 POST 只会得到必然失败的请求、一串「撤销失败」日志噪音，以及"平台试过了"的假象——接口回一个 `upstream_revoke_required: true`，由界面明说要供给者自己去 claude.ai 清，将来后端真能远端撤销时把这个布尔翻掉即可）、**不动钱包**（已入账的是债，与 §3.6「注销用户仍可能是债主」同源）、**不清 `owner_user_id`**（留在软删行上，是"这号曾经是谁的"的唯一证据）。抹凭证的 SQL 另带 `owner_user_id = $2`，虽然 `getOwnedAccount` 已经查过一次——两次往返之间那个 TOCTOU 窗口，代价只是 WHERE 里多一个条件。解绑后**同一份订阅可以再挂回来**：两条查重 SQL 都带 `deleted_at IS NULL`，且身份键就存在被抹掉的 `credentials` 里，所以不会出现"退出后再也进不来、还收到一句莫名其妙的『已经连过了』"，这条由真库测试钉死。路由是 `DELETE /accounts/:id` 而**不**套 Heavy 限流：撤回自己的授权是供给者最该畅通无阻的动作，它也不打上游、不耗任何配额。

**接入上限有两道闸，真正有阻力的是第二道**。`requireCapacity` 与 `requireAgreement` 挂在同样的两处（`StartOAuth` 与 `CompleteOAuth`），同样**必须早于 `ClaimSession`**，理由一字不差：领取是一次性消费，先领后拒等于烧掉供给者手上那个只能用一次的授权码。四条边界：**每人上限只是礼貌性护栏**——再注册一个用户就是一个新的 `user_id`，额度重新开始数；**每 IP 上限挡的正是那个绕过**，注册账号免费、换出口网络不免费，判据是 `supplier_account_origins`（迁移 230）里那条在 `CompleteOAuth` 建号成功后写下的记录；**每 IP 那道默认关着**（`max_accounts_per_ip = 0`），运营商级 NAT、校园网、公司出口后面站着成百上千个真实的人，一个偏小的默认值会把他们静默挡在门外，而症状「注册了但挂不上号」不会有人来报障——所以管理端那张卡片在这道闸开着时画一段琥珀色警示；**读配置失败的回退方向刻意不对称**，每人上限退回默认的 5（比运营通常配的更严），每 IP 上限退回不限（更宽松），因为一个因数据库抖动就挡住整个出口网络的闸，损失比它防的那点刷号大得多。两个错误码分开（`SUPPLIER_ACCOUNT_LIMIT_REACHED` / `SUPPLIER_NETWORK_LIMIT_REACHED`）：前者他自己能纠正（解绑一个旧号），后者不能，报混了他会去翻自己那一列空空如也的账号。数的是**当下**未解绑的号而不是历史累计（两条 COUNT 都带 `deleted_at IS NULL`），否则一个正常换号的供给者会永久耗尽自己的额度。来源写入失败**不中断接入**，只记日志：号已经建出来、已经有主了，此刻返回错误撤销不了这两件事，只会让供给者看到一个失败提示、实际却挂上了的号——然后他会重试，于是有了第二个号。

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

### 3.6 管理端运营视图的五条边界（改动前先读）

**整层只读，唯一的例外是提现审批，而这两件事都是决定不是遗漏**。管理端能写的只有 §3.1 那五组设置，加上提现单的「标记已打款 / 拒绝」。改账号归属、改余额、手工放行观察期都不在这一刀里——它们会动钱和归属，各自需要审计与复核路径；混进一个"看板"接口里，迟早会被当成看板随手点。提现审批之所以能进来，是因为**一张已经扣了钱的单子必须有人能推进它**：钱在申请时就离开了可用余额，没有审批入口就等于把供给者的钱冻在一个谁也动不了的中间态。它也只改状态和退款，不碰归属、不碰账号、不碰任何余额之外的东西。前端的 `adminSupplyMarketAPI` 有一条单测钉住写方法的**完整清单**（不是"必须为空"），加一个写方法就必须先改这条断言——这正是让人停下来想一下的地方。

**常量必须 `fmt.Sprintf` 进 SQL，不许重打一遍**。状态字面量（`pending_review` / `active` / ...）和 jsonb 键名（`apexone_supply_state` 等）全部来自 `service` 包常量。理由是漂移在这里**完全静默**：后端改了状态名而 SQL 里还写着旧的，看板不会报错，只会把一批账号算进错误的桶，或让它们从扫描里整个消失。同理状态缺失一律兜底成 `pending_review`（`COALESCE(NULLIF(extra->>'...',''), 'pending_review')`），与 `supplier_onboarding_repo.go` 的扫号口径一份。

**调用方传的键一个字节都不进 SQL 文本**。排序键（`owed`/`history`/`accounts`/`recent`）经 `supplierRosterOrderBy` 的 switch 映射到写死的 `ORDER BY` 片段，未知值返回 `""` → service 层报 `SUPPLY_ADMIN_INVALID_SORT`，**不静默回落到默认排序**——一个悄悄回落的白名单会让"前端改了键名、后端没跟上"永远没人发现。排序片段全部以 `, u.id ASC` 收尾，否则同值行在翻页间会换位，翻两页看到同一个人。这与身份键（§3.3）是同一条规则，也是同一个理由：它紧挨着一张带余额的表。

**"供给者"只有一个定义，而且是并集**。看板顶部的人数与名册的分页总数取自同一段 `supplyRosterUserSetSQL`：有过供给账号的人 **∪** 钱包四个数任一不为零的人。两处各写一份的下场是运营看到"37 个供给者"却只翻得出 31 行。取并集而不是只看账号，是因为"号全删了但还欠着钱"的人必须留在名册上——把他藏起来，那笔待付负债会在对账时凭空冒出来。同理名册 join `users` 时**不加** `deleted_at IS NULL`：注销用户仍可能是债主。

**管理端流水的过滤器是独立类型，刻意不与供给者侧共用**。`SupplyAdminLedgerFilter.UserID == 0` 表示"看全站"，而供给者侧的同名字段 `<= 0` 必须拒绝。把两者合并成一个类型，供给者侧任何一处漏传 user_id 的 bug 就会从"查不到"升级成"看到所有人的账"。

**流水行的类型两边共用，但消费者身份只有管理端看得到**。`SupplyAdminLedgerEntry` 内嵌 `SupplierCreditLedgerEntry`（理由见该类型注释：两份结构体迟早有一份忘了跟着加金额字段）。共用带来的代价是 `source_user_id` 会顺着供给者侧的读路径出网——翻页拉一遍就是一份"谁在用我的号"的 user_id 序列。因此 `SupplierCreditService.ListLedger` 返回前统一走一次 `stripConsumerIdentity`：**抹在 service 而不是 handler**，因为这一层就是"供给者视角"的边界，日后再挂一个读流水的 handler 不会重新漏。管理端保留该字段——追一笔拒付必须能定位到消费者。两条路径的差别仅此一个函数，由 `supplier_credit_service_test.go` 钉住（同时断言序列化后的 JSON 里**没有这个键名**：只断言字段为 nil 的话，`omitempty` 被删掉会变成一个 `"source_user_id": null`，键名本身已经在告诉供给者这个维度存在）。

### 3.7 提现的九条边界（改动前先读）

**申请即扣款，不是审批时才扣**。提交的那一刻金额就从 `available_credit` 扣走并落一条 `withdraw` 流水，单据只是"这笔钱要打到哪儿"的凭证。反过来做（审批时才扣）会留下一个人人可用的套利窗口：挂满未决单，然后在审批之前把同一笔余额花掉，运营批的每一张都是超发。代价是这句话必须写在供给者看得见的地方——余额在他点下按钮的瞬间就少了一块，不解释就是一次客服工单。UI 上三处都说了：表单上方的 `deductHint`、提交成功的 toast、撤回确认框。

**退款走一条独立的 `withdraw_revert` 流水，绝不改原来那条**。追加式流水的意义就是任何一行落盘后不再变；把 `withdraw` 抹掉或改金额，供给者账上会凭空少一段历史，出纠纷时没法回放。因此拒绝与撤回都是「插一条反向流水 + 加回余额」，`withdraw` 与 `withdraw_revert` 两个数在管理端看板上**分开显示、永不轧差**——退回的笔数本身就是信号：它说明渠道配错了或者审核标准在漂。

**退款有两道闸，缺一道都会双倍退钱**。第一道是状态更新的 `WHERE status = 'pending'`（并发下只有一个请求能把单子推离 pending）；第二道是流水表 `(action, request_id)` 上的部分唯一索引，`request_id` 取 `withdraw_revert:<单号>`。只有第一道时，一个能直接改库的人（或将来某个补状态的运维脚本）把状态改回 pending 就能再退一次；只有第二道时，并发的两个拒绝请求都会去插同一条流水，一个成功一个撞索引——退款金额是对的，但另一个请求拿到的是一个数据库错误而不是"这单已经处理过了"。所以 `refundSupplierWithdrawal` 里 `if !inserted { return nil }` 那一支不是防御性冗余，它是第二道闸的**正常返回路径**，已被专门的测试钉住（把状态篡改回 pending 后再退一次，余额必须纹丝不动）。

**参数越界报错，不 clamp**。与结算参数同侧、与观察期参数（§3.5）相反。理由是这一组的失效是静默的：起提额被悄悄夹到上限，结果是全站没有一个人提得出钱，而管理端页面上一切正常——运营只会收到零星的"提现按钮点不动"，查上一星期。观察期参数夹错最多是观察久一点，性质完全不同。

**渠道按完全相等匹配，只 trim 首尾空格**。不做大小写归一、不做模糊匹配：`USDT` 和 `usdt` 是两个渠道，改名等价于把老渠道下线。这看着不友好，但另一边更糟——归一化之后运营在白名单里写的字符串和打款系统里的渠道标识对不上，而**错的是一笔已经打出去的钱**。因此渠道改名必须当成一次下线来做，管理端那张卡片上写明了这一点。前端把渠道列表按**换行**分隔编辑而不是逗号：渠道名里出现逗号是完全合法的（"银行卡, 仅限境内"）。

**「开着但没配渠道」必须能被两边分别看见**。这是这一组最容易出现的坏状态：开关打开了、渠道白名单是空的，于是每一次申请都被硬拒。供给者侧因此把"平台没开提现"和"开了但渠道在维护"画成两段不同的文案——后者明说是平台侧配置问题、不要重试；管理端那张卡片在这个状态下把 banner 画成红色。`options` 接口的 `available` 是**两个条件的与**（开着 且 有渠道），前端不自己拼这个判断。

**通知是这个功能能运转的必要条件，不是体验优化**。提现是整条链路上唯一一个**钱先离开、结果后到**的动作，而它在两端各有一个静默失败：供给者侧余额少了一块、单子挂在那里，他唯一能做的是每天回来刷页面；运营侧**没有任何人被告知有单要处理**，后台不会自己弹出来。因此 `SupplierWithdrawalNotifier` 发三种信（新申请→运营、新申请→供给者的扣款回执、终态→供给者），全部 best-effort：在 goroutine 里发、用 `context.Background()` 而不是请求 ctx（客户端一断连信就发不出去，而这个失败只在日志里）、失败只记 `slog.Error`。**一笔已经落库的提现不能因为 SMTP 超时而回滚**——钱已经扣了，回滚意味着单子没了但流水还在。通知的调用点一律在 `err != nil` 检查**之后**，否则会发出一封关于并不存在的单子的邮件。撤回**不发信**：那是供给者自己刚点的按钮，界面上已有确认框和 toast。邮件里**不放收款账号**——它是 PII，而邮件会被转发、被搜索、被留在收件箱十年；运营需要它时后台看得到。收件人配在 `supply_withdrawal_settings.notify_emails`，与配额告警的收件人（`SettingKeyAccountQuotaNotifyEmails`）**刻意分开**：收钱的是财务，收告警的是运维，合成一份会训练两边都去过滤这类邮件。收件人格式错误**报错而不是静默清洗**（与同一组里的渠道相反）：渠道少一个供给者立刻就看得见，收件人少一个没有任何可见症状。「开着但没配收件人」与「开着但没配渠道」一样是必须能被看见的坏状态，后端下发 `notify_configured`，管理端那张卡片在这个状态下画成琥珀色（比没配渠道的红色低一档：功能还能用，只是没人被叫过来）。

**返回视图剥掉 `user_id` 与 `reviewer_id`，但保留 `review_note` 与 `external_ref`**。前两个是身份，出网就是一份 id 序列（同 §3.6 的 `source_user_id`）；后两个是供给者必须看到的：一张被拒的单子不给理由，等于让他重新提交一次同样被拒的单。`external_ref`（打款凭证号）是他去自己的收款渠道对账的唯一抓手。剥离做在 handler 的 `supplierWithdrawalView` 上——与 §3.6 那处做在 service 层不同，因为提现单类型本来就只有供给者侧和管理端两个消费者，且管理端走的是另一个 handler，没有"日后再挂一个 handler 会重新漏"的面。

**收款账号在库里是密文，但在运营的审批页上是明文——这是刻意的**（迁移 `232`）。这条边界要说清楚它防的是什么、不防什么。

它防的是**离开数据库的那份数据**：一份 `pg_dump`、一次定时备份、一个给分析用的只读账号、一条把行打进日志的排查语句。这几条路径的共同点是它们都绕过应用层，因此也绕过一切权限与审计——`payout_account` 是这套系统里唯一一个泄漏后能被直接拿去冒名收款、且当事人**换不掉**的字段（换银行卡不像换密码），所以它值得单独加一层。

它**不防**运营本人。审批页上必须显示明文：那张页面就是打款工作单，运营要照着上面的账号把钱转出去，加密到他眼前才解开等于没加密。真正约束运营的是另外两样东西——管理端整个 group 挂着审计中间件（谁在什么时候看了哪张单子有记录），以及提现的写路径只有"标记已打款"和"拒绝"两条（§9 的路由清单钉住了这一点）。**把加密当成权限控制是这里最容易犯的错**：它是一层存储侧的防泄漏，不是一层访问控制。

三条实现上的决定，改动前先读：

1. **带版本前缀 `enc.v1:`，不用"试着解密、失败就当明文"**。后者要靠 GCM 的认证失败来分辨两种情况，于是"密钥配错了"与"这是一行历史明文"长得一模一样——而前者必须炸、后者必须放行。前缀把这个判断变成一次字符串比较。带版本号是为了将来换算法时旧行仍可读。
2. **解不开时返回错误，绝不返回空串**。这是整段代码里唯一要紧的一条：若返回空串，运营看到的是一张收款账号为空的提现单，他会以为供给者没填，然后去问、或者干脆按备注里的信息打款。一个错误会让这张单子留在待办里，而那正是它此刻该待的地方。
3. **同一账号两次加密的结果必须不同**（GCM 随机 nonce，已有测试钉住）。确定性密文意味着任何拿到库的人不解密就能看出"这两个供给者填的是同一张卡"——那恰好是刷号排查最想知道的信息，也恰好是不该在一份泄漏的备份里免费送出去的信息。

**`accounts.credentials`（上游 OAuth 令牌）本轮未加密**，这是一个明确的已知缺口，不是遗漏：那是上游的列，约二十处消费者，且自助接入的查重逻辑要在它的 jsonb 内部按身份键（`credentials->>'uuid'` / 邮箱）搜索——加密会直接打断这些查询，要做得配一套盲索引（HMAC 列）。当前部署跑在 Phala TDX CVM 内，磁盘本身是加密的，缺口的实际暴露面小于普通部署。要做的话是一个独立的、需要回填的改动。

### 3.8 拒付／争议的八条边界（改动前先读）

**这是不变量 2 的另一半，而且是更重要的那一半**。§3.1 末段那条 clawback 挂在**我们主动发起的退款**上；这一节挂在**别人替我们发起的退款**上——持卡人绕过平台直接找发卡行撤销交易。区别在于后者不产生任何 `payment_intent` 事件、我们不发起任何请求，在 `payment_disputes` 这张表出现之前，`charge.dispute.*` 走到 provider 那个 switch 里命中 `return nil, nil`，webhook 回 200，然后**什么都不发生**。拒付是这套系统里唯一一件平台会真的赔钱的事，它此前在库里没有任何痕迹。

**副作用只挂在 `open` 上，不等 `closed`**。Stripe 在创建争议的同时就把款项从我们账上扣走；等争议关闭要 60–90 天，那时冻结区里对应的分成早已解冻、甚至已经提现出去。「等结果出来再说」听起来稳妥，实际是保证追不回来。代价是判我们赢时会出现一次"多扣了"——那由下一条处理。

**赢了不自动回补，这是刻意的**。争议判赢时钱回到我们账上，但已经扣掉的消费者余额与已经追回的供给分成**不自动还回去**。回补消费者余额是一行加法；回补供给者的分成不是——追回撤销的是一条条带各自冻结到期时间的入账，「重新入账」是一条全新的写路径，跑两遍就是凭空造钱。而只补消费者、不补供给者，等于让干活的那一方独自承担一次判错，比两边都不补更糟。所以胜诉那封信里必须原样写着「系统不会自动回补」并把两个该补的数字摆出来，由人去补。这条是 §6 里的待定策略。

**"款项动没动"只看 `status`，与事件名无关**。五种 `charge.dispute.*` 带的是同一个 Dispute 对象，钱在谁手里完全写在 `status` 里。四种 warning/prevented 是**询证**：Stripe 通知我们有人在问，款项一分没动，必须返回 `(nil, nil)`。把询证认成拒付，运营看到的现象是「有人问了一句，供给者的钱就没了」。将来 Stripe 新增的任何状态一律默认按"钱没动"处理——漏一次追回可以事后补，凭空收走供给者的钱不能。

**幂等闸在库里，不在代码里**。`payment_disputes.settled_at` 非空 = 两个副作用已经跑过。UPSERT 的 `ON CONFLICT` 子句里**完全没有** `settled_at` 与四个结算金额，`ClaimForSettlement` 是一条 `UPDATE ... WHERE settled_at IS NULL RETURNING id`。这两件事合起来才成立：同一个争议 Stripe 会推五次，其中 `closed` 那次几乎必然发生在结算之后，upsert 若能把闸门推回去，这笔拒付就会被扣第二遍。ON CONFLICT 里另外几条 `COALESCE`/`CASE` 防的是同一类事的另一个方向——后到的推送字段未必更全（`payment_intent` 展开与否在不同事件里不一样），直接 `EXCLUDED` 覆盖会让一条本来对上了订单的记录在几十天后变成对不上。`basis_amount` 结算后冻住，因为那时它已经是对账凭据而不是一个待定的估算。这一组全部由真库测试反证过（见 §8）。

**对不上订单的争议照样落库，而且是最该有人看的一类**。同一个 Stripe 账户被多套环境（预发／另一个部署）共用时，webhook 会推来别人的争议；那种情况下 `order_id` 为 NULL，行仍然要写，日志用 WARN 而不是 ERROR（它未必是故障），邮件里明说「系统没有对任何余额或分成做过改动」并把人指到支付后台去认领。这张表因此**刻意没有外键**。

**订阅订单的拒付走人工**。余额订单要扣回的是一个数，订阅订单要撤的是天数——那件事需要 `GetActiveSubscription` + `ExtendSubscription`/`Revoke` 一整套，且有「订阅已经过期撤不动」的分支。在一条不能失败、不能重试、不能回滚的 webhook 上跑那套，失败时没有任何补救路径。所以只扣余额订单，订阅订单的处置写在给运营的邮件里。

**失败不让 webhook 报错**。返回 error 会让 handler 回 500，Stripe 于是重投；而重投打到的是同一个已经 claim 过的争议，副作用不会再跑一遍（`settled_at` 挡着），重投唯一的效果是刷日志。所以这条路径上的错误一律记 ERROR 后咽掉，webhook 照常回 200——真正需要人介入的信息在日志和运营邮件里，不在 HTTP 状态码里。同理**通知不能成为新的失败方式**：`PaymentDisputeNotifier` 没装配时静默返回，与追回单例同样的理由。但**台账没装配时仍然留一条 ERROR 日志**，那是「拒付发生过」的唯一痕迹。

### 3.9 对账导出的六条边界（改动前先读）

**这个功能与本仓其余所有接口有一个结构性区别：它的响应体是一份要离开这台机器的文件。** 下载完成之后，那份 CSV 就不再受这套系统的任何约束——它会被放进网盘、发进微信群、附在邮件里转给财务。前八节里那些边界（审计、限流、鉴权、只读）全都到浏览器的下载目录为止。这一节的六条边界几乎都是从这一点推出来的。

**响应头在第一行数据之前就发出去了，因此错误无处可报。** `200 OK` 与 `Content-Disposition` 一旦写出，浏览器就已经在往磁盘上存文件；此后数据库掉线、连接被代理掐断、扫描到一半报错，运营看到的都是"下载完成"。这是流式下载的固有性质，不是实现缺陷——替代方案（先把整份文件攒进内存、成功了再一次性写出）能给出正确的状态码，代价是运营点一次导出就有机会把网关 OOM 掉，而那会连带停掉**所有消费者**的请求。所以错误处理只有两段：写头之前能报就报（服务没装配 → 503 JSON），写头之后只记 `slog.Error`。

**尾行是唯一的补偿，因此它必须永远在。** 文件末尾那一行 `#, exported N rows for X .. Y (UTC)` 承担两件在别处说不了的事：这份文件覆盖了哪段时间、它是不是完整地写完了。中途出错时**刻意不写**这一行——一份没有尾行的文件在结构上就是"没写完"，而那是残缺文件唯一的可辨识特征。撞上 20 万行上限时尾行改口喊 `TRUNCATED ... narrow the time range and export again`：只说截断不说下一步该干什么，运营还是只能再点一次同一个按钮。**一份静默截断的对账文件比一次失败的下载危险得多**——后者你会重试，前者你会照着它打款。

**收款账号在文件里是明文，这与 §3.7 的加密不矛盾。** §3.7 那层密文防的是离开数据库的数据（`pg_dump`、备份、只读分析账号、日志行），它从一开始就**不防运营本人**——审批页要显示明文，因为那是打款工作单，而这份导出文件是同一张工作单的批量版本。约束运营的是另外两样东西：这两条路由挂在管理端那个带审计中间件的组里（事后答得出"上个月那份带着全部供给者收款方式的表格是谁导的"），以及 `Cache-Control: no-store`（不让任何一层代理替我们把它留下来）。**把加密当成权限控制是这里最容易犯的错**。

**金额走 `::text`，是全仓唯一一处。** 其余所有读路径都 `::double precision`，因为那些值要参与计算。导出不计算，因此没有理由让 `DECIMAL(20,8)` 先绕一趟二进制浮点——流水里 `basis_amount × share_ratio = amount` 这个等式是供给者自行核对分成的唯一依据，浮点往返之后它就不一定还成立。真库测试用 `0.00000001`（最小刻度）和 `0.333333` 钉住了这一点。

**文件要在别人的表格软件里打开，于是有三条与 Excel 直接相关的决定**：写 UTF-8 BOM（少三个字节，Windows 版 Excel 按本地代码页解码，收款账号里的开户人姓名全成乱码，而运营多半会以为是自己电脑的问题）；文本列前置单引号中和公式起手字符 `= + - @ \t \r`（`user_note` 与 `payout_account` 由**供给者**填写，`=HYPERLINK(...)`、DDE 那一类把戏至今有效）；时间一律 UTC 的 RFC3339。中和**不对金额列做**——那会把负数变成文本，整列在表格里既求不了和也排不了序，等于把一个安全问题换成一个对账问题，而金额来自 NUMERIC 列，本来就注入不进东西。

两条工程上的选择顺带记在这里：**没有分页循环**（LIMIT/OFFSET 翻页导出会在两页之间给表留下插入新行的机会，同一行可能出现两次或一次都不出现；一个游标从头扫到尾看到的是一致快照），以及**截断探针查 `limit+1` 行而不是先 `COUNT(*)`**（后者要多扫一次表，且数完到扫完之间的新增行会让两个数字对不上；探针只回答"后面还有没有"，那正是唯一需要回答的问题，而它本身绝不进文件）。**默认窗口 90 天**而不是"不给条件就导全部"：截断截掉的是**最新**那一段（按时间正序推），"导全部"会给运营一份从开天辟地开始、恰好把他想要的那部分截掉的文件。

### 3.10 供给号失效事件的八条边界（改动前先读）

**这张表存在的唯一理由是 `accounts` 没有历史。** 账号状态只回答「此刻怎么样」，而这个功能要回答的三个问题全是时间上的：这个号是**什么时候**坏的（决定那封信该不该发）、这个人**这个月**坏了几次（封禁率报表）、他**最近**坏得是不是太频繁（接入熔断）。这三个问题在 `accounts` 上一个也问不出来——号从 `error` 恢复成 `active` 的那一刻，"它坏过"这件事就没了。所以迁移 `233` 建的是一张追加式台账：一行 = 一次失效，`detected_at` / `notified_at` / `resolved_at` 三个时刻各记各的。

**开与关必须互补，这是全模块最重要的一条。** 开的判据是「有主人 + 未软删 + 状态非空且非 `active` + 状态不是 `retired`」，关的判据是它逐条的反面**外加**「号已经不在了」。两者若不互补，坏的方向有两个且都不响：留下一条永远关不掉的事件（运营看板上一个永远归不了零的数字），或者每一轮扫描先关再开同一条事件——那意味着**供给者每 5 分钟收到一封同样的邮件**。互补性由一条部分唯一索引（`WHERE resolved_at IS NULL`）与 `ON CONFLICT (account_id) WHERE resolved_at IS NULL DO NOTHING` 兜底：即便判据将来漂了，一个号同时也只可能有一条未结事件。真库那 22 处变异里有 10 处就是逐条拆这两个表达式（见 §8）。

**供给者自己按的下线不是事故。** `retired` 与 `draining` 的区别在这里第一次有了实际后果：`retired` 被排除（他自己解绑的号坏了，不该收到"你的号出问题了"），`draining` 不排除（排空中的号**还在接单**，坏了照样要通知）。这条判据之所以能这么干净地写出来，是因为 §3.5 那个决定——供给者发起的暂停只写 `extra` 里的 `supply_state` 与 `schedulable`，**从不碰 `accounts.status`**。于是 `status <> 'active'` 恒等于"上游那边出事了"，没有第二种解释。反过来说：将来谁让下线路径去写一次 `status = 'disabled'`，这张台账立刻开始给所有主动下线的人发事故信。

**状态为空算健康。** `accounts.status` 上游只有 `active`/`disabled`/`error` 三个取值，但历史行里可能是空串。把空算成坏的表现非常具体：功能上线的第一分钟，给一批存量供给者群发"你的号停了"。这条在真库测试里差点漏掉——共用的 fixture 会把空 status 兜底成 `active`，于是"空状态"那个用例造出来的其实是一个健康号，变异因此活了一轮（见 §8）。

**`notified_at` 的含义是「信发出去了」，不是「我们试过发」。** 因此发信是**同步**的（与 §3.7 提现那三封信相反：那边的调用方是 HTTP 请求，不能等 SMTP；这边的调用方是后台扫描，它必须知道结果），且**先发后标**、失败不标。单轮只发 50 封而开事件一轮 500 条，是因为前者是 N 次 SMTP 往返、后者是一条 SQL，而整轮跑在生命周期任务 10 分钟的总超时里——一次波及几百个号的上游故障中，信会分几轮发完，`notified_at` 保证没有人因此收到两封。`MarkNotified` 上那个 `AND notified_at IS NULL` 是同一件事的多实例版本：它拦不住两封信都已发出（那一刻已经过去了），但它保证列里留下的是**第一封**的时刻。

**信只发给供给者，且不含上游错误原文。** 一个号坏了是那个人的损失，不是运营的待办；运营要的是趋势（封禁率报表），不是每个坏号一封邮件——真到了该惊动运营的量级，那是告警的事，不是收件箱的事。原文（可能带 token 片段、内部地址、整段上游 JSON）只留在管理端报表里，入库前还先截到 `SupplierIncidentErrorMaxLen`。

**熔断闸默认关着，且查询失败一律放行。** 后半句是与另外两道接入闸（每人 / 每 IP）**相反**的方向，理由不对称：那两道数的是当下的账号数，一次简单 COUNT，失败几乎只意味着库挂了，而库挂了接入本来也走不通；这道数的是历史事件，它失败时接入路径其余部分完全正常——为一次统计查询的抖动把一个正常供给者挡在门外，代价大于放进来一个坏号（下一轮扫描照样记下他，闸恢复后照样拦得住）。数的是**事件次数**而不是坏号个数：同一个号反复坏是最强的信号，按号去重会把它压成 1。窗口与阈值分成两个配置字段（`max_incidents_per_user` / `incident_window_hours`），且窗口的 `0` 与本仓其它所有上限的 `0` 含义**不同**——那些是"不限"，这个只是"没填"，兜默认 7 天，因为 0 小时的窗口会让一道看起来开着的闸永远拦不住任何人。给用户的错误文案刻意不写具体阈值（那等于把绕过方法一起发给他），也刻意说"最近"和"稍后再试"（被上游一次大面积封号波及的正常供给者需要知道这不是永久的）。

**报表里「当前未结」不带窗口，其余三个数带。** `opened` / `resolved` / `suppliers` 问的是"这段时间发生了什么"，`open` 与榜单里的 `open_incidents` 问的是"现在还有几个坏着"——一个坏了三个月的号不该因为它是在窗口之前坏的就从后者里消失。榜单的驱动表是"窗口内出过事的人"，所以榜上不会出现零事件的行（那是一张"谁在坏"的榜，不是全体供给者名册），但连账号数用的是 `LEFT JOIN`：号全解绑了、事件还挂着的人必须留在榜上，而他的 `Rate` 分母为零——比率因此在 Go 侧算并显式判零，不在 SQL 里写 `NULLIF`（那个 `NULLIF` 漏一次就是运营点开报表看到整页 500）。**最后：整层对账号是只读的**，检测到坏号不会自动禁用、下线或解绑任何东西。那是供给者的资产，平台的动作止于"记下来 + 告诉他"。

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
| 5f | `backend/internal/service/wire.go` + `backend/cmd/server/wire_gen.go` | ProviderSet；`supplierWithdrawalService` 构造处 | 注册 `NewSupplierWithdrawalNotifier`，`NewSupplierWithdrawalService` 多收一个 notifier 形参；`wire_gen.go` **手工改**（+1 行、改 1 行） | wire 标准注册点。通知器走**构造注入**而不是 #5d/#5e 那种进程级单例：它没有 core 侵入面（`EmailService`/`UserRepository`/`SettingService` 三个都是既有依赖），单例只会让它变得不可测。构造函数里 `if notifier != nil` 那一支是必需的——直接赋值会得到一个"非 nil 接口装着 nil 指针"，于是"没配邮件"变成提现主路径上的一次空指针 panic。**本仓无 `wire` 二进制**，`wire_gen.go` 一律手工同步 | **已做** |
| 8 | `backend/internal/server/routes/admin.go` | `registerSettingsRoutes` 末尾 | 追加十二行路由 + 一行 `// APEXONE-EXT:` 注释（结算 / 供给池 / 观察期 / 供给者协议 / 提现 / 接入上限各一对 GET+PUT） | 挂进既有 settings group 而不是自起一个 admin group：那个 group 上有 adminAuth + 面板限流 + 审计 + `AdminComplianceGuard` 四层中间件，复制一份等着它日后与上游漂移，而漂移掉的是合规相关的那几层。handler 方法挂在既有的 `*SettingHandler` 上，因此 wire、`AdminHandlers`、`ProvideAdminHandlers` 三处热区一处没动 | **已做** |
| 7 | `backend/internal/service/gateway_scheduling.go` | :97 函数签名 | **仅函数改名**：`SelectAccountWithLoadAwareness` → `selectAccountInPoolWithLoadAwareness`，函数体一字未改。导出名归新文件 `gateway_supply_overflow.go` 里的同名包装函数 | 交接件说的「零 core 溢出」不成立（见 §2）。耗尽有十几个 return 点，逐个插判断等于把一条新规则摊成十几处侵入；改名 + 外层包装是**一处**。合并冲突面小到只有一行签名，且上游若改了函数体，冲突会照常落在函数体上而不被包装掩盖 | **已做** |
| 7a | — （**未覆盖面，记账用**） | `openai_codex_models_handler.go:44`、`gateway_handler.go:2058` | 这两处走的是 `SelectAccountForModel`，不经包装函数，因此**不溢出** | 首版切片的供给池只面向 Claude Code 形态消费，这两条路径不在范围内。若日后供给池扩到 codex 形态，需在此处补同样的包装 | 已知缺口 |
| 5g | `backend/internal/repository/wire.go` + `backend/internal/service/wire.go` + `backend/cmd/server/wire_gen.go` | 两个 ProviderSet；`wire_gen.go` 两行 | `NewSupplierCreditRepository` → `ProvideSupplierCreditRepository`（内部调 `SetPaymentDisputeStore`）；`NewSupplierWithdrawalNotifier` → `ProvideSupplierWithdrawalNotifier`（内部调 `SetPaymentDisputeNotifier`） | 争议台账与通知器都是进程级单例，理由同 #5d/#5e（不往 `PaymentService` 上加字段）。**台账只能从 repository 那侧装配**：`repository` import `service`，反过来不成立。宿主选 `ProvideSupplierCreditRepository` 有两条理由：wire 保证会构造它（单例注入的东西没有消费者，独立 provider 会被剪掉 → 第一次真实拒付时 `store == nil`，见 #5a/#5c 那个坑），且追回写的正是它拥有的那张表。通知器同理挂在提现通知器上——两者共用同一份 `notify_emails` | **已做** |
| 5h | `backend/cmd/server/wire_gen.go` | `NewSupplierWithdrawalRepository` 构造处 :319 | **手工改 1 行**：多传一个已存在的 `secretEncryptor`（:90 处已构造，本是 TOTP 用的） | 收款账号加密（迁移 `232`）。刻意复用既有的 `SecretEncryptor` 而不是新起一份密钥配置：多一个密钥就多一次"部署时忘了配"的机会，而这个字段配漏了的表现是**提现申请全站失败**（`seal` 在没有加密器时报错、不降级成明文）。`internal/repository/wire.go` 一字未动——`NewSupplierWithdrawalRepository` 本来就在 ProviderSet 里，wire 按类型自动补上这个形参；只有手工维护的 `wire_gen.go` 需要跟一行。**本仓无 `wire` 二进制**，同 #5f | **已做** |
| 5i | `backend/cmd/server/wire_gen.go` | `supplierHandler` 构造处 :319 附近 | **手工改 3 行**：新建 `supplierExportRepository` / `supplierExportService` 两行，`NewSupplierHandler` 多传一个形参；`internal/repository/wire.go` 与 `internal/service/wire.go` 各加一个 provider | 对账导出。导出仓储收的是与提现仓储**同一个** `secretEncryptor`——那份文件里的收款账号必须是明文（§3.9）。导出没有并进 `SupplierAdminRepository` 也没并进 `SupplierWithdrawalRepository`：前者整层不碰收款账号，不该为了导出多拿一把密钥；后者已有自己的 sqlmock 与真库两套测试，合进去等于把两组依赖搅在一起。handler 方法照旧挂在既有的 `*SupplierAdminHandler` 上，`AdminHandlers` / `ProvideAdminHandlers` / `handler/wire.go` 三处热区一行未动（同 #9）。**本仓无 `wire` 二进制**，同 #5f/#5h | **已做** |
| 10 | `backend/internal/handler/payment_webhook_handler.go` | `handleNotify` 的 `notification == nil` 分支 | 插入两行：一行 `// APEXONE-EXT:` 注释 + 一行 `h.handleDisputeNotify(...)`。逻辑全在新文件 `payment_webhook_dispute.go` | 争议事件挂在既有 webhook 上而不是新开路由：Stripe 后台一个 endpoint 收全部事件类型，分流要运营在后台再配一个 endpoint 并勾对事件类型——那是一件不做也不报错、做错了也不报错的运维动作，而它错了的表现是「拒付静默地不被处理」，与上线前一模一样。挂的位置是 `notification == nil`，那个分支的含义正是「验签通过但不是我们认识的支付事件」，因此**支付主链路一行不动**，认得的支付事件根本走不到这里。同步执行而非 goroutine：Stripe 超时 20 秒，而这条路径通常几十毫秒；异步会让失败日志与本次推送对不上号 | **已做** |
| 5j | `backend/internal/service/wire.go` + `internal/repository/wire.go` + `backend/cmd/server/wire_gen.go` | 两个 ProviderSet / `supplierHandler` 构造处附近 | 新增 `NewSupplierIncidentRepository` / `NewSupplierIncidentNotifier` / `NewSupplierIncidentService` 三个注册，并把既有的 `NewSupplierOnboardingService` 包一层成 `ProvideSupplierOnboardingService`；`ProvideSupplierLifecycleService` 多一个形参。**手工改 3 处**：wire_gen 里新建三行、接入服务改走新 provider、生命周期服务多传一个实参 | 失效事件同时被两个已有服务消费，而两处都只能用 setter：`SetIncidentGuard`（接入服务）与 `SetIncidentSweeper`（生命周期任务）写成构造参数会**成环**——事件服务不依赖它们，它们依赖事件服务，但接入服务的构造又发生在事件服务能被观测到之前。setter 需要一个能表达这一步的 provider，于是有了 `Provide*` 壳。`SetIncidentSweeper` 必须排在 `Start()` **之前**：`Start()` 之后第一轮随时可能起跑，那一轮读到的是 nil 还是真对象取决于赛跑结果。构造顺序上事件仓储/通知器/服务三行排在接入服务之前。**本仓无 `wire` 二进制**，同 #5f/#5h/#5i | **已做** |
| 5k | `backend/internal/handler/supplier_handler.go` | `NewSupplierHandler` 形参表 | 加第六个形参 `incidentService`，转手作为 `NewSupplierAdminHandler` 的第四个实参 | 失效运营视图的两条路由挂在既有的 `*SupplierAdminHandler` 上（同 #9），因此依赖从 `Handlers.Supplier` 这条既有链路下传。`AdminHandlers` / `ProvideAdminHandlers` / `handler/wire.go` 三处热区仍是一行未动。既有 handler 单测两处调用点各补一个 `nil` | **已做** |
| 9a | `backend/internal/server/routes/supplier.go` | `registerSupplyMarketRoutes` 函数体 | 加两条只读 GET（`/incidents`、`/incidents/summary`）+ 一行 `// APEXONE-EXT:` 注释 | 管理端供给侧路由从九条变成十一条。`routes/admin.go` **一行未动**——#9 那一行调用已经把整组挂进去了，这正是当初单起 `registerSupplyMarketRoutes` 的收益：此后每加一条运营接口，core 的路由文件都不再需要改动。`supplier_test.go` 里那条计数断言随之从 9 改到 11，改断言那一步强迫人回答"这两条该不该出现在这个组里"（见 §9） | **已做** |
| 9 | `backend/internal/server/routes/admin.go` | `registerSettingsRoutes(admin, h)` 之后 | 加 `registerSupplyMarketRoutes(admin, h)` 一行 + 一行 `// APEXONE-EXT:` 注释；函数体定义在 `routes/supplier.go` | 管理端运营视图的路由组（起步九条：四条只读 + 提现那三条：列表 / 标记已打款 / 拒绝 + 对账导出那两条 GET；失效视图两条后为十一条，见 #9a）。写路径为什么能进这一层见 §3.6 首段。挂进既有 admin group（adminAuth + 面板限流 + 审计 + `AdminComplianceGuard` 四层中间件），理由同 #8。handler **不进 `AdminHandlers`** 而是挂在既有的 `Handlers.Supplier` 下（`h.Supplier.Admin`）——`AdminHandlers` 结构体、`ProvideAdminHandlers` 形参表、`handler/wire.go` 是三处上游合并热区，每加一个管理端 handler 就要各留一行。用户侧与管理侧刻意是**两个类型**：路由挂错时是编译期的事，而不是把一个全站流水接口挂到了 `/user/supply` 下面 | **已做** |

## 5. 结算不变量（实现必须守住）

1. **消耗 === 入账**：failover 后只有产出被计费响应的账号入账（以 `usage_log.account_id` 为准）；失败/重试/无计费产出零入账。accrue 幂等键 = `usage_log.RequestID`。
2. **冻结窗 ≥ 拒付窗**：使「已释放 = 拒付安全」。冻结期内拒付 → 从供给者冻结 earnings 追回（`SupplierCreditRepository.Clawback`），**两个触发点走同一个入口**：我们主动发起的退款成功之后（§3.1 末段），以及持卡人发起的拒付（§3.8，`charge.dispute.created`）。释放后拒付平台自吃。冻结窗配小了不会报错，只会让追回**撤不满**——缺口体现为 `UncoveredBasis > 0`、一条 `slog.Warn`，以及 `payment_disputes.uncovered_basis` 里一个可以累计查询的数。这是这条不变量在运营期唯一的可观测信号，也是 §6 里 `freeze_hours` 该调到多少的唯一一手数据。
3. **同一请求只扣一处**：赚取钱包或 `users.balance`，由 `applyUsageBillingEffects` 单点保证。
4. **审计快照**：ledger 每条带快照，供给者可自行核对计量。

## 6. 待定参数（fog，实现期取默认值、运营期调）

前四个已经是**真实可配的**了，落在 settings key `supplier_settlement_settings` 里（见 §3.1）；表里的「拟定默认」就是 `DefaultSupplierSettlementSettings()` 的值，且总开关 `enabled` 默认 `false`。

第二个 key `supply_pool_settings`（`enabled` / `supply_group_id` / `overflow_group_id` / `daily_overflow_limit`）同样默认关，装的是路由而不是钱。第三个 key `supply_probation_settings`（见 §3.5）装的是准入与下线时序，`enabled` 同样默认关。第四个 key `supply_agreement_settings` 装协议文本与版本号，版本留空 = 自助接入整条关着（见 §3.3）。第五个 key `supply_withdrawal_settings`（见 §3.7）装提现开关、起提额、每人未决单上限、收款渠道白名单、说明文本与运营通知收件人，`enabled` 同样默认关、渠道白名单默认为空。第六个 key `supply_onboarding_settings`（见 §3.3）装每人 / 每来源 IP 的账号数上限，两个字段都以 0 = 不限为约定，每 IP 那道默认就是 0。**刻意分成六个 key**：这几组配置因完全不同的原因变动（动分成比例 / 动兜底池 / 动准入门槛 / 改协议 / 换收款渠道 / 收紧刷号），合成一个会让几次意图完全不同的修改共用一条审计记录。

六个 key 都已有管理端 API 与界面（`/admin/supply-market` 页，六张卡各自一个保存按钮）。**四个总开关默认全关，这是上线策略而不是待办**：代码随版本先进生产，管理员逐个打开。但「提现开着 + 渠道白名单为空」是一个必须避开的中间态，它在两边的表现见 §3.7。

| 参数 | 拟定默认 | 说明 |
|---|---|---|
| `enabled` | `false` | 总开关。默认关就是上线策略：代码先随版本进生产、在计费主链路上待着，管理员显式打开才开始动钱 |
| `share_ratio` | 0.70 | 供给者分成，基数 = 消费者实付（非官方价） |
| `spend_from_wallet_first` | `false` | 与 `enabled` 分开是有意的：可以先只开入账让供给者攒着，等钱包侧观察稳了再打开消费出口 |
| 供给池 `rate_multiplier` | 0.5 | 消费者价 = 0.5× 官方价。**不在这两个 key 里**，是分组自己的 `rate_multiplier` 字段，管理端分组页配 |
| `freeze_hours` | `168`（7 天，**占位**）→ 建议上线前改 `720`（30 天） | 须 ≥ 支付通道拒付窗。7 天只是能跑通流程的占位值，**上线前必须按 Stripe/支付方实际拒付窗重设**，否则不变量 2「已释放 = 拒付安全」不成立。代码侧只 clamp 到 90 天上限（`SupplierFreezeHoursMax = 2160`），拦不住「配小了」——这一条只能靠人。**建议值 720（30 天）**：卡组织规则允许持卡人在结算后 120 天内发起争议，理论上"安全"要 2880 小时，但那意味着供给者干完活四个月才拿得到钱，没有人会来供货；实际拒付的绝大多数落在交易后一个月内。30 天是「追得回大部分 + 供给者还愿意来」的折中，而漏掉的那条尾巴不是无声的——它落在 `payment_disputes.uncovered_basis` 上。**上线一个月后按那一列的真实累计值再调**，而不是继续拍脑袋 |
| 自营池 `rate_multiplier` | `1.0`（**占位**） | 需覆盖真实 API 成本 + 毛利，上线前重设 |
| `daily_overflow_limit` | `0`（不限量，**待定**） | 溢出的日次数上限，在 `supply_pool_settings` 里。默认 0 = 不限量但照常计数——先跑几天看真实溢出量再定值，凭空拍一个上限会在供给正常波动时误伤。打开溢出后应尽快设成一个可预算的数：这是「按自营成本供货却按供给池价收费」这笔亏损的唯一硬上限 |
| 观察期强度/时长 | `enabled=false`、60 分钟、连续 2 次、探测间隔 15 分钟、排空窗 10 分钟 | 已参数化，落在 `supply_probation_settings` 里。默认**不自动入池**：先让它探测、记录，运营看几天再打开。这些数字是能跑通流程的起步值，不是标定过的 |
| 每人账号数上限 | `5` | 已参数化，落在 `supply_onboarding_settings.max_accounts_per_user`。0 = 不限。只是礼貌性护栏——换个用户就绕过了，见 §3.3 |
| 每 IP 账号数上限 N | `0`（不限，**待定**） | 已参数化，落在 `supply_onboarding_settings.max_accounts_per_ip`。默认关是刻意的：CGNAT / 校园网 / 公司出口后面是成百上千个真实的人，凭空拍一个住宅户级小 N 会静默挡掉他们，而这类误伤没有人会来报障。应当先看几周真实 IP 分布，再配一个远大于「一户人家」的数，之后按 IP 封禁率收紧 |
| 胜诉后是否回补 | **不回补**（当前实现） | 见 §3.8。判我们赢时消费者余额与供给者分成都不自动还回去，只发一封把两个数字摆好的信给运营，由人补。要把它自动化，缺的不是那行加法而是「重新入账」这条写路径——它必须自己是幂等的，否则一次重投就是凭空造钱。真实拒付量起来之前不值得为它新增一条能造钱的路径；`payment_disputes` 里 `status='won' AND settled_at IS NOT NULL` 的行数就是判断"值不值得"的读数 |

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

第 4 步里有一处**不需要靠人眼盯**：`routes/user.go` 的中间件链一旦被上游改动（多一层合规闸、多一道封禁检查），`routes/supplier.go` 会安静地少那一层——不报错、不变红。`internal/server/routes/supplier_test.go` 里那条链清单把两个文件逐字比对，上游这么改的时候它会红，见 §9。

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
| 226 + 224 | `supplier_onboarding_repo_integration_test.go`（9 例，新文件） | 会话一次性领取（`UPDATE ... WHERE consumed_at IS NULL RETURNING` 的行锁语义）；拿别人 session_id 领不走且**失败的领取不会把会话标记成已消费**（否则是一条免费的拒绝服务）；过期判定用数据库时钟；已消费的会话不被过期清理带走；`owner_user_id` 写一次即定、不能从 A 改成 B；按状态扫号时**状态缺失兜底成 `pending_review`**（拼接 SQL 与 service 常量的契约，漂移了是静默失效）；上游 uuid 查重不看归属也不看 `schedulable`；**邮箱查重大小写不敏感**（`LOWER()` 比对真的走得通，不是靠 sqlmock 认字符串）；未知身份键是报错而非静默放行 |
| 227 | `supply_overflow_repo_integration_test.go`（5 例，新文件） | **并发下不超发**：60 个 goroutine 抢 10 次配额，放行数必须恰好 10。这是那条 `ON CONFLICT DO UPDATE ... WHERE` 存在的全部理由，写成"先 SELECT 再 UPDATE"在这个测试里必挂。另有配额跨日重置、被拒次数单独计、不限量时照常计数 |
| —（无迁移相关） | `supplier_leader_lock_integration_test.go`（5 例，新文件） | 多实例选主。此前只有两组单进程测试：service 侧一个内存假锁、repository 侧 miniredis，都答不出部署时真正关心的那个问题——**N 个实例同时起来，到底有几个跑了这一轮**。真后端证的是：真 Redis 下持锁期间后来者一个都进不来、释放后新实例立刻能进（不会永久饿死）、锁的 TTL 真的设上了、八个实例同时起跑只选出一个 leader 且**同时在跑的永远只有一个**；Redis 报错时真的退到了 Postgres advisory lock（去 `pg_locks` 里按 fnv 算出的 id 认那把锁，"裸跑"与"持锁"在这里才分得开）且释放时连会话一起收掉；解冻与生命周期用的是两把不同的锁。写法上从 `Start()` 打进去，走的是与生产完全一致的路径 |
| 231（**2026-08-20 补**） | `payment_dispute_repo_integration_test.go`（9 例，新文件） | 这张表的全部保证都写在一条 UPSERT 的 `ON CONFLICT` 子句里，而 sqlmock 只会把那条 SQL 原样还回来。真库证的是：`ON CONFLICT (dispute_id)` 真的推断得到迁移 231 的唯一索引（推断不上不是降级而是报错，表现为"第二次推送整条 webhook 500"）；**结算之后再来的推送碰不到 `settled_at` 与四个结算金额**，也碰不到已冻住的 `basis_amount`——这是"一笔拒付只扣一遍钱"的最后一道保证；结算之前 `basis_amount` 仍可被修正；订单关联**只补不覆盖**（孤儿争议事后能被补上订单，已对上的不会被字段更少的推送打回孤儿）；`resolved_at` 只记第一次关闭；`ClaimForSettlement` 串行重放五次只有第一次拿到 true，**16 个 goroutine 同时抢也恰好只有一个赢**；空 `dispute_id` 在进库前就被三个方法各自拦下 |
| 232（**2026-08-20 补**） | `supplier_withdrawal_repo_integration_test.go`（+2）、`supplier_payout_cipher_test.go`（6 例，新文件、不带 tag） | 单测能证密文形状，证不了**库里到底存了什么**。真库证的是：`payout_account` 列真的放得下约 1 060 个 base64 字符（229 建的是 `VARCHAR(256)`，迁移 232 没跑到的话这条插入直接报错，而不是悄悄截断），直读那一列拿到的是 `enc.v1:…`、里面搜不到卡号——**这正是一份 `pg_dump` 会看到的东西**；建单回读、列表、审批回读三条读路径都解了密（少解一条的表现是运营照着一串 base64 去打款）；以及绕过仓储直插一行明文（模拟升级那一刻库里的实际状态：一张已经扣过钱、还等着打款的旧单子）后，它照常读得出来、也照常推得到终态——那是"不需要停机窗口"这个承诺的全部依据 |
| —（无新迁移，**2026-08-20 补**） | `supplier_export_repo_integration_test.go`（8 例，新文件） | 这两条导出查询是本轮唯一用 sqlmock 测不出**任何**东西的部分——它只把 SQL 当字符串比对，而这里每条断言问的都是"Postgres 拿到这串东西之后做了什么"。真库证的是：按 `status` 筛不撞 ambiguous column（`supplier_withdrawals` 与 `users` **都有** `status` 这一列，而导出比屏幕上的列表多一个 `LEFT JOIN users`，这条路只有"导出且按状态筛"这一个组合能走到）；两个 `LEFT JOIN` 的 **LEFT**——`reviewer_id` 为 NULL 的待处理单子必须在文件里，写成 INNER 的话"还没打的款"会整批消失，而那恰恰是运营导这份表的第一个理由；`::text` 拿到的是 NUMERIC 原文（`0.00000001` 不被舍成 `1e-08`、`0.333333` 是比例快照原文）；`limit+1` 探针置位 `truncated` 且那一行**不进文件**，而恰好导满时不算截断；时间窗真的筛、顺序真的是 `created_at ASC`；密文行与 232 之前的历史明文行同处一份文件都读得出来 |
| 233（**2026-08-21 补**） | `supplier_incident_repo_integration_test.go`（16 例，新文件） | 这张表的核心性质是**开与关必须互补**，而互补性是两条 SQL 表达式之间的关系——sqlmock 把它们都当字符串，看不出两条加起来是不是恰好覆盖了全集。真库证的是：`ON CONFLICT (account_id) WHERE resolved_at IS NULL` 真的推断得到迁移 233 那条部分唯一索引（推断不上不是降级而是报错），于是**连扫三轮只开出一条事件**；号恢复后事件被关掉、复发时开的是**第二条**新事件而不是复用旧的；自营号（无归属人）、空状态号、`retired` 号一个都不被开事件，而 `draining` 号照常开（它还在接单）；关的那一侧三条路径各自成立——号被软删（`LEFT JOIN ... AND a.deleted_at IS NULL`，写成 INNER JOIN 的话事件会永远留在“当前坏着”里指向一个不存在的号）、归属被摘、主人自己按了解绑；上游错误原文真的被截到 `SupplierIncidentErrorMaxLen`；`MarkNotified` 的 `AND notified_at IS NULL` 真的保留**第一封**信的时刻（这条只有在事务里种一个人为提前两小时的值才测得出来——同一事务内两次 `NOW()` 是同一个值）；待发队列按 `detected_at ASC` 出（积压时先通知已经停了最久的那个人）；明细列表的四个筛子与“未结置顶”的排序；报表里**「当前未结」不带窗口而其余三个数带**（一个坏了三个月的号仍要出现在“现在还有几个坏着”里）；以及号全解绑的供给者仍在榜上且他的 `Rate` 是 0 而不是一次除零 |
| —（无新迁移） | `supplier_admin_repo_integration_test.go`（7 例，新文件） | 运营视图全是跨表聚合，sqlmock 只会把写好的 SQL 原样还回来，连 `COUNT(*) FILTER (...)` 是不是合法都不知道。真库证的是：自营账号（`owner_user_id IS NULL`）一个都没混进任何一个数字；jsonb 状态缺失兜底进 `pending_review`；看板人数恒等于名册分页总数，且**号全删了但钱包还有余额的人仍在名册里**；四个排序键的 `ORDER BY` 片段都真的合法、翻页不重不漏；未知排序键报错；健康度/状态/归属三个筛子各自成立且观察期字段真的从 jsonb 里解得出来；流水窗口用的是数据库时钟（`NOW() - make_interval(days => $1)` 的参数类型推断只有真库能证），把行挪到 60 天前它就从 30 天窗口里消失、把窗口拉到 90 天它又回来 |

并发那两例（溢出配额、争议占坑）刻意**不走 `testEntTx`**：那个 harness 是一个会回滚的单事务，会把所有写串行化，正好掩盖掉要测的东西。它们用 `integrationEntClient` 直连并自行清理。

运营视图那一组反过来必须走 `testEntTx`，但断言一律是**先取基线、再比增量**：那几个查询是全站聚合，写死绝对值等于假设整个库里只有这一个测试的数据，那个假设迟早被下一个测试文件打破。

选主那一组两样都不用：它测的是跨进程互斥，任何事务隔离都会把它变成自说自话。它直连 `integrationDB` 与 `testRedis(t)`，用一个能把任务体卡住的仓储桩制造"一轮还没跑完"的窗口。文件放在 `internal/repository` 而不是 `internal/service`，因为判定逻辑在 service、锁实现在 repository，而 service 不能反向 import——**组装起来的样子**只有在 repository 里才看得见。

迁移 231 是 2026-08-20 才加进来的，它那一组按同样的规矩补齐（10 处 SQL 变异逐条反证：把 `settled_at` 塞进 `SET` 列表、去掉 `basis_amount` 的 `CASE`、把 `COALESCE` 换成裸 `EXCLUDED`、拿掉占坑的 `AND settled_at IS NULL`、把冲突键指到没有唯一索引的列上等，每一条都被对应用例逮住）。

选主那五例都用改坏实现的方式反证过确实会红：把 `SETNX` 换成 `SET`（互斥失效）、去掉"缓存报错退到数据库"那条分支（变成裸跑）、把 advisory lock 的 `db.Conn` 换成 `db.QueryRow`（连接还回池子后同会话可重入，互斥静默消失）——三种改法各自被对应的用例逮住。

迁移 232 那一组同样逐条反证过（5 处变异，每处都能编译）：插入时跳过 `seal` 直存明文、`open` 解密失败返回空串而非报错、去掉 `open` 里的前缀判断（历史明文被喂进 `Decrypt`）、`seal` 不加版本前缀、`scan` 末尾整段不解密——五条各被对应用例逮住，其中"返回空串"与"不加前缀"两条只有单测看得见，"直存明文"与"不解密"两条只有真库看得见。

对账导出（§3.9）逐条反证过 13 处变异，每处都能编译，且**刻意分布在三层**：仓储侧六处（WHERE 不加表名前缀 → 撞 ambiguous / reviewer 用 INNER JOIN / 不多要探针那一行 / 金额绕一趟 float64 / 按时间倒序 / 收款账号不解密），服务侧两处（窗口不覆盖 filter 里的时间 → 文件名与实际查询的范围各说各话 / `Available()` 不看仓储），HTTP 与编码侧五处（公式字符不中和 / 出错也写尾行 → 给半截文件盖上完整的章 / 截断不在文件里说 / 不写 BOM / 文件名用当下而非窗口）。分层是有意义的：仓储那六处只有真库看得见，编码那五处只有单测看得见——一份在 Excel 里被当成公式执行的单元格不会让任何一条服务端日志变红。

失效事件（§3.10）逐条反证过 **33 处**变异，每处都能编译，分布在两层。仓储侧 22 处：开的判据五处（不排除 `retired` / 空状态算坏 / 不排除自营号 / 不截断错误原文 / 去掉 `ON CONFLICT` 幂等）、关的判据五处（JOIN 不排除软删 / 漏掉「归属被摘」 / 漏掉 `retired` / 把还坏着的也关掉 / `LEFT JOIN` 改 `INNER`）、通知队列三处（去掉「只记第一封」闸 / 把已发过的也捞出来 / 改成先通知最新出事的）、明细列表三处、报表与榜单五处（当前未结夹上窗口 / `open_counts` 夹上窗口 / `windowed` 丢掉窗口 / 没有号的供给者被挤掉 / 去掉除零护栏）、熔断一处（不看时间窗）。服务侧 11 处，其中三处是**顺序**而非取值：`Sweep` 必须先关后开（先开后关会让一批刚恢复的号在同一轮里先被开一条事件再被关掉——供给者收到的是一封「你的号坏了」，而他的号此刻是好的）、发信必须在标记之前、发信失败不许标记。

这一组里有两个变异第一轮活了下来，两者的性质完全不同，都记在这里：**「空状态算坏」活下来是测试的错**——共用 fixture 会把空 `status` 兜底成 `active`，那个用例造出来的其实是一个健康号，修法是在测试里直接改库把它改回真正的空串。**「关的判据漏掉『号已不在』」活下来是被测代码的性质**：`a.id IS NULL` 与紧邻的 `a.owner_user_id IS NULL` 在 `LEFT JOIN` 补出的空行里恒同真假，去掉前者行为完全不变，它是一个等价变异，不可能被任何测试钉住。那一支仍然留在表达式里（它让「号被删了」这件事有个名字，否则下一个人读到的会是「归属被摘的号要关事件」），代价写在源码注释里；真正守着删号路径的是 JOIN 上的 `a.deleted_at IS NULL`，替它的变异因此改成拿掉那个条件，一次就红。

**仍未验证的**（不在真库能力范围内，需要活的上游）：真实 OAuth 授权码兑换。

## 9. 路由装配的验证实录（2026-08-20）

`internal/server/routes/` 此前对供给侧**零覆盖**。这一层出错的方式恰好是最安静的一类：路由挂错组、少一层中间件、新加的一条忘了跟着挂——三种都不会让任何 handler 报错，只会让一条本该要登录的接口不要登录，或者一条会动余额的写接口不进审计日志。

`supplier_test.go`（不带 build tag，与 `internal/server/routes` 其余测试一致，`make test-unit` 与 `make test-integration` 两边都跑到）分两类断言：

**行为类**——从 `router.Routes()` 反推，而不是照着 `supplier.go` 抄一遍路径清单。后者只能证明"我写的和我写的一样"，前者对将来新增的每一条路由自动生效，而"新加的一条忘了挂"正是要防的那种遗漏。三条：十五条用户路由每一条都真的走过 JWT 与审计（审计桩 abort 掉，于是依赖全空的 handler 一次也没被调用）；未登录时十五条一条不漏地 401；管理端十一条挂在 admin 组下，未登录同样一条都进不去。

**清单类**——三处只能从源码读，因为它们在装配测试里**观测不到**：

| 钉住的东西 | 为什么行为测不出来 |
|---|---|
| 中间件链与 `RegisterUserRoutes` 逐字相同（含顺序） | `BackendModeUserGuard(nil)` 与 `panelRateLimiter.Global()` 拿到 nil 依赖时都直接 `c.Next()`，与"根本没挂"是同一个观测结果。顺序同理：审计必须**最后**，否则被前面几层挡下的请求会在审计日志里留下一条根本没发生的"某某访问了提现接口" |
| Heavy 限流的挂载点恰好是 `oauth/start` / `oauth/complete` / `POST /withdrawals` | `Heavy()` 与 `Global()` 在没有 Redis 的测试里都放行，注册结果里分辨不出谁是谁。而这三条的**反面**（撤回提现、解绑账号、同意协议不套 Heavy）最容易被"顺手加一层更安全"改掉——那等于在供给者最急着把钱拿回来、最想撤回授权的时候让他做不到，测试因此把这三条也单独点名 |
| 管理端的写路径只有提现审批那两条 POST | §3.6 那条边界（整层只读，唯一例外是提现审批）在后端唯一的落点。前端有一条同形状的断言钉住 API 客户端的写方法清单；两边都要求"加一条写接口先改断言"，也就是先停下来想一下这个写动作该不该出现在一个看板里 |

那条"管理端一共几条"的计数断言在加对账导出（§3.9）时立刻变红了——两条新的 GET 一挂上去，`should have 7 item(s), but has 9` 就把改动拦在了提交之前。这正是它存在的理由：改断言那一步强迫人回答"新加的这两条该不该出现在这个组里"，而答案（该，因为它们要跟着走审计中间件）是这次唯一需要想清楚的事。失效运营视图（§3.10）上线时它第二次变红——`should have 9 item(s), but has 11`，同样的两条 GET、同样的一次追问。两次都只改了那个数字，但两次都是在那一刻才确认「这条新接口进的是带审计的管理端组」，而不是上线三个月后从日志里发现它没进。

加上三道判空（`h` / `h.Supplier` / `h.Supplier.Admin` 任一为 nil 时一条路由都不注册）：wire 装配失误时正确的表现是这些接口 404，不是一打就 502。管理端那半边直接调 `registerSupplyMarketRoutes` 而不走 `RegisterAdminRoutes`，因为后者在 `h.Admin` 为 nil 时会自己先崩（上游既有行为，每个 `registerXxxRoutes` 都直接读 `h.Admin.<字段>`），那样测的就成了上游的容错。

### 9.1 「我是谁」只有一个来源

路由挂对了，还剩另一半：handler 自己会不会认一个来自请求的身份。`internal/handler/supplier_identity_test.go` 只证这一件事。

它单独成文，是因为这条性质失守时**没有任何症状**。多读一个 `user_id` 入参不会让任何测试变红、不会让任何请求报错，只会让一个人能把账号挂到别人名下、能查别人的流水、能把钱提到自己这里——三件事看起来都像功能正常工作。行为测试也覆盖不到：要发现它，得先想到去构造一个"带着 A 的 token、请求体里写 B"的请求，也就是得先怀疑这段代码。

因此断言是**结构性**的——不检查某次调用的结果，检查这段代码里有没有第二条路径可以回答"我是谁"：

- `*SupplierHandler` 上全部 16 个导出方法（= 15 条用户路由，`ListWithdrawals` 用户侧与管理侧同名分属两个类型）每一个都走 `h.currentUserID(c)` 或 `h.mutateAccount(...)`。收敛到一个入口，是让"取不到 id 时怎么办"这个决定只有一份——十六份实现里迟早有一份忘了在 `!ok` 时 return，于是它带着 `userID=0` 往下走。
- `mutateAccount` 自己第一件事就是取 id。没有这条，上一条可以被"随便加个走 mutateAccount 的方法"绕过。
- 用户侧一行都不从请求里读 `user_id`（query / form / param / json tag 四种写法各查一遍）。这是上一条的另一半：上面证的是"正确的来源被用了"，这条证的是"没有第二个来源"——一个方法完全可以既调 `currentUserID`、又在请求体里认一个 `user_id` 覆盖掉它。
- 用户侧直接读 JWT 上下文的地方**恰好一处**。管理侧（`reviewerID` 之后那半边）不在此列：那里的 `user_id` 是筛子，是运营视图该有的东西，鉴权在路由组的 adminAuth 上。

4 处变异反证：某个端点绕过 `currentUserID` 直接读 JWT 上下文 / 请求体结构里多一个 `user_id` 字段 / `mutateAccount` 先动账号再确定是谁 / 提现申请改从查询参数拿 `user_id`。

### 9.2 变异清单

路由层 12 处变异逐条反证：去掉审计层 / 把路由挂到未认证的 `v1` 组上 / `POST /withdrawals` 丢掉 Heavy / 给 `DELETE /accounts/:id` 套上 Heavy / 运营视图多一条写接口 / 少一条只读接口 / 去掉两道判空各一次 / 去掉后台模式闸 / 去掉面板限流 / 把审计挪到 JWT 之前 / **在 `user.go` 里给用户组加一层而供给侧不动**——每一条都被对应用例逮住。最后那条是这组测试存在的主要理由，它模拟的就是下一次同步上游时最可能发生的事。
