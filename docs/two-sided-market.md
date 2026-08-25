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
| 失效视图前端 | `frontend/src/api/admin/supplyMarket.ts`（两个只读方法）、`views/admin/SupplyOperationsView.vue`（一段）、`i18n/locales/{zh,en}/supply.ts` 的 `supplyOps.incidents` | — | 挂在运营页账号明细与全站流水之间：那张账号表答「此刻怎么样」，这一段答「这段时间发生过什么」。报表四格里第三格（当前还坏着）**不带窗口**，副标题必须把这件事说出来——并排四个数字混着两种口径而没有视觉线索，运营会把它们当成同一段时间。榜单里 `accounts = 0` 的行比率画成 `—` 而不是 `0.00`（后者看起来像「他很健康」）。整段无写路径，`supply.spec.ts` 里那条「管理端写方法清单」断言因此一字未动 |
| 对账导出前端 | `frontend/src/api/admin/supplyMarket.ts`（`exportWithdrawals` / `exportLedger` + 尾行判定）、`views/admin/SupplyOperationsView.vue`（提现与流水两处按钮）、`i18n/.../supply.ts` 的 `supplyOps.export` | — | 两条 GET 此前没有入口。前端**必须**读尾行：完整 / `TRUNCATED` / 无尾行三档给三种强度的提示，后两档把后缀写进文件名。时间窗用页头那个选择器（一页上两套时间基准，运营迟早会导出一份和屏幕上不是同一段时间的文件），但按钮上要写明"近 N 天"——屏幕上那两张表本身不带窗口，翻的是全部历史。见 §3.9 |
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

### 3.9 对账导出的七条边界（改动前先读）

**这个功能与本仓其余所有接口有一个结构性区别：它的响应体是一份要离开这台机器的文件。** 下载完成之后，那份 CSV 就不再受这套系统的任何约束——它会被放进网盘、发进微信群、附在邮件里转给财务。前八节里那些边界（审计、限流、鉴权、只读）全都到浏览器的下载目录为止。这一节的六条边界几乎都是从这一点推出来的。

**响应头在第一行数据之前就发出去了，因此错误无处可报。** `200 OK` 与 `Content-Disposition` 一旦写出，浏览器就已经在往磁盘上存文件；此后数据库掉线、连接被代理掐断、扫描到一半报错，运营看到的都是"下载完成"。这是流式下载的固有性质，不是实现缺陷——替代方案（先把整份文件攒进内存、成功了再一次性写出）能给出正确的状态码，代价是运营点一次导出就有机会把网关 OOM 掉，而那会连带停掉**所有消费者**的请求。所以错误处理只有两段：写头之前能报就报（服务没装配 → 503 JSON），写头之后只记 `slog.Error`。

**尾行是唯一的补偿，因此它必须永远在。** 文件末尾那一行 `#, exported N rows for X .. Y (UTC)` 承担两件在别处说不了的事：这份文件覆盖了哪段时间、它是不是完整地写完了。中途出错时**刻意不写**这一行——一份没有尾行的文件在结构上就是"没写完"，而那是残缺文件唯一的可辨识特征。撞上 20 万行上限时尾行改口喊 `TRUNCATED ... narrow the time range and export again`：只说截断不说下一步该干什么，运营还是只能再点一次同一个按钮。**一份静默截断的对账文件比一次失败的下载危险得多**——后者你会重试，前者你会照着它打款。

**尾行必须有人去读，否则前一条边界只是一句写在文件里的自言自语。** 前端拿到 blob 之后先读末尾几 KB 判尾行，分三档处理：完整 → 按服务端定的文件名存下；`TRUNCATED` → 存下并**警告**（文件是好的，只是不全，收窄窗口再来一次）；**没有尾行** → 存下并**报错**（服务端写到一半挂了，这份文件不能用来打款）。后两档还各往文件名里加一个 `-TRUNCATED` / `-INCOMPLETE` 后缀——**提示条会消失，文件不会**：一份残缺的对账文件躺在下载目录里，三天后谁都看不出它当时报过警，除非那件事就写在文件名上。判定读不出来时（`FileReader` 失败）一律往"不完整"上判：错判成完整的代价是有人照着残缺文件打款，错判成残缺的代价是重导一次。另外两处细节：文件名一定用服务端 `Content-Disposition` 里那个（窗口写在里面，而文件躺三天之后文件名是唯一还留在外面的上下文），以及 `responseType: 'blob'` 会让**写头之前**那个 503 JSON 错误也变成 blob，于是拦截器取不出 message、界面只能报一句笼统的失败——这是流式下载换来的代价，不是遗漏。

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

### 3.11 链上收款地址绑定的九条边界（改动前先读）

**一个地址只能属于一个人，这条由数据库守，不由应用层守。** 应用层的"先查有没有人绑过、没有就写"在并发下必然漏——两个请求同时查到"没人绑"，然后都写进去。真正守着它的是迁移 `234` 上的 `UNIQUE (network, address_hash)`，仓储里那次前置反查存在的意义**只是错误文案**：唯一索引抛出来的是 `duplicate key value violates unique constraint ...`，那句话不能给用户看。两者的分工必须保持这个方向——把前置反查当成保证、把唯一索引当成兜底，是把因果关系读反了。仓储里那个 `isUniqueConstraintViolation` 分支因此**必须**有一个真库测试走到（`UniqueIndexBitesAndIsRecognized`：绕过仓储直插一行重复地址，断言 `errors.As` 真的认出了 `*pq.Error` 的 `23505`），否则那一支是永远不执行的死代码，而它不执行的那天正是并发真的撞上的那天。

**加密列上挂不了唯一索引，所以有了盲索引。** `address` 与 `payout_account` 用同一套 AES-256-GCM（`enc.v1:` 前缀，见 #5h），而 GCM 是**非确定性**的：同一个地址每次加密出来的密文都不同，唯一索引在密文上挡不住任何东西。因此另立一列 `address_hash = SHA-256(lower(address))`，反 sybil 的唯一约束挂在它上面。**这在这里安全，但这个直觉不能搬到密码上**：一个 EVM 地址是 160 位、不可枚举，而密码来自一个小得多的分布——对密码做一次裸 SHA-256 当索引等于把整张表交给彩虹表。两者的区别是**熵**，不是"哈希了就安全"。

**归一化只有一处，就在仓储里。** 小写落库是为了让「同一个地址」只有一种写法，比较和去重才成立。`payoutWalletHash` 里那次 `strings.ToLower` 看起来是多余的（调用方 `Upsert` 上游已经归一化过了），但它是那个函数**自己**的契约——第一轮变异测试里"盲索引不归一化大小写"活了下来，正是因为它在当前调用路径上不可达。补法不是删掉它，是加一个直接测 `payoutWalletHash` 的单测：下一个从别处调它的人不该需要先读一遍 `Upsert` 才知道要不要自己先小写。

**EIP-55 校验和错与格式错分开报。** 对用户是两句完全不同的话：格式错是"你少粘了几位"，校验和错是"你粘的位数对，但其中有一位不是你以为的那一位"——后者几乎必然是手工改过地址，也是最危险的一种。零地址第三种，因为它谁也收不到钱。合并成一个 400 会让最危险的那一种读起来像个格式问题。

**链上渠道的收款账号只能来自绑定，永远不来自手填，且三个方向全部失败关闭。** `resolveOnchainAccount` 认出这是链上渠道之后，「没绑地址」「读绑定报错」「绑定服务没装配」三种情况一律拒绝建单，不回落到用户手填的那串字符。放行任何一种，结果都是一串未经校验的文本被当成链上地址落进单子——而那正是这整套绑定机制要消灭的东西。第三种（服务没装配）尤其容易被写成回落，因为它看起来像"这套部署没开这个功能"；但渠道白名单里已经放了链上渠道这件事说明它**是开着的**，只是坏了。

**链上渠道的收款账号在前端也没有一个能手填的输入框。** 这条与上一条不是同一道门的两种说法：后端那道在**建单**这一刻生效，挡住"钱打错地方"；前端这道在**画表单**这一刻生效，挡住"让他以为该手填"。少了前端这道，界面会照常渲染出一个输入框、能填、能提交，然后后端回一句"没绑收款地址"——他会转身去找一个这个表单从头到尾没有给过他的绑定入口。判据取自 `withdrawalOptions.onchain_channels`（与渠道白名单同一个响应），不取自绑定接口一起返回的那份 `channels`：两个接口之间的时间差会画出「渠道列表里没有 BSC-USDT、下面却在催你绑 bsc 地址」这种自相矛盾的界面。同一条边界还管住三件小事：**地址原样提交**（前端 `toLowerCase` 一下，EIP-55 校验和就在到达后端之前消失，而那是唯一能发现"你改过其中一位"的信号）、**绑定后显示后端返回的那一串而不是他手里那串**、**已绑定时输入框收起来**。

**管理端一条接口都没有。** 一个能替别人改收款地址的按钮，与一个能替别人提现的按钮是同一件东西。运营需要看某人绑了什么时，走提现单上的 `payout_account` 快照——那是一条已经在 §3.9 里审计过的路径。这条边界的落点在 `routes/supplier_test.go` 的全站清单断言上（横跨用户端与管理端两次注册，断言全站含 `payout-wallet` 的路由恰好是用户侧那三条），因此连一个看起来无害的管理端 GET 都会让它变红——那种 GET 会让「谁绑了哪个地址」变成一份可以在后台随手导出的名单。

**解绑刻意不检查在途提现单。** 在途单的收款地址是建单那一刻的快照（`payout_account`），解绑不会改道任何一笔已经在路上的钱。反过来若在这里加一道"有单不让解绑"，一张卡住的单子就能把人的收款地址锁死。

**M1 不往提现单上写 `network` / `token_symbol` / `token_address`。** `ResolvePayoutAddress` 的第一个返回值（链与币）在 M1 里被显式丢弃，那行 `_` 上有注释指向 M3。理由是 `token_address` 要等 M2 的配置，而只写 `network` 会留下一批 M4 的 worker 日后捞得到、却打不出去的半成品行。M1 因此只做一件严格更安全的事——链上渠道的收款账号改从已校验的绑定里取——这让 M1 自身是自洽的：即便有人提前把 `BSC-USDT` 加进提现渠道白名单，走的也只是原来那条人工打款的路，不会有任何东西被自动广播出去。

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
| 9b | `backend/internal/server/routes/supplier.go` | `RegisterSupplierRoutes` 函数体 | 加三条收款地址绑定路由（`GET /payout-wallets`、`PUT|DELETE /payout-wallets/:network`）+ 一段 `// APEXONE-EXT:` 注释 | 用户侧供给路由从十六条变成十九条。`routes/router.go` **一行未动**——#4 那一行调用已经把整组挂进去了。`PUT` 单独套 `panelRateLimiter.Heavy()`：它是供给侧唯一一个会往带唯一索引的表里写行的接口，不限住的话，反复 PUT 别人的地址、看回的是 200 还是"已被占用"，就是一个现成的「这个地址有没有人绑过」查询器。`DELETE` 刻意不套，理由与撤回提现、解绑账号一字不差。**管理端一条都不加**：一个能替别人改收款地址的按钮，与一个能替别人提现的按钮是同一件东西（见 §3.11） | **已做** |
| 5l | `backend/internal/repository/wire.go` + `internal/service/wire.go` + `backend/cmd/server/wire_gen.go` | 两个 ProviderSet；`supplierWithdrawalService` 与 `supplierHandler` 构造处 | 注册 `NewSupplierPayoutWalletRepository` / `NewSupplierPayoutWalletService`；**手工改 4 处**：wire_gen 里新建两行，`NewSupplierWithdrawalService` 多收第五个形参，`NewSupplierHandler` 多收第七个形参 | 绑定仓储收的是与提现仓储**同一个** `secretEncryptor`（同 #5h：多一把密钥就多一次"部署时忘了配"的机会）。提现服务通过一个**窄口**（`supplierWithdrawalAddressResolver`，只有 `ResolvePayoutAddress` 一个方法）依赖它，而不是直接持有整个服务：提现只需要回答"这个渠道该打到哪"，给它绑定/解绑的能力等于让提现路径有改收款地址的可能。构造函数里 `if wallets != nil` 那一支是必需的，理由与 #5f 同形但后果不同——直接赋值得到的"非 nil 接口装着 nil 指针"**不会** panic（`ready()` 挡住了 nil 接收者），只会让链上渠道静默变成"服务不可用"，比 panic 更糟：运维会去查一个根本没坏的服务。**本仓无 `wire` 二进制**，同 #5f/#5h/#5i/#5j | **已做** |
| 11 | `backend/cmd/server/main.go` | `runMainServer()` 内，SIMPLE 模式告警之后 | 加一行 `logPayoutChainStatus()` + 一段 `// APEXONE-EXT:` 注释；函数体定义在同文件末尾（+27 行） | 链上打款的启动自检。密钥没注入 / RPC 指向测试网 / 币地址写错，这三种误配如果不在启动时说，就要等到第一笔提现走到一半才暴露——那时单子已经进了 `processing`。**出错刻意不致命**：提现走不通的代价远小于整个进程起不来（所有用户都登不上），所以自检失败只记一条 `⚠️` 继续启动。自检**只读配置、只问一次 `eth_chainId`，不动任何钱**，且给了 5 秒超时——一个连不上的 RPC 不该把启动拖住。日志里带金库地址但**永远没有私钥**：地址是公开信息（每笔链上交易都写着它），运维靠它在 BscScan 上盯余额 | **已做** |
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
| `freeze_hours` | **`720`（30 天）** | 须 ≥ 支付通道拒付窗，否则不变量 2「已释放 = 拒付安全」不成立。原先是 168（7 天）的占位值，2026-08-21 改成 720 并在 `setting_supplier_test.go` 里**钉死了这个字面量**：代码侧只 clamp 到 90 天上限（`SupplierFreezeHoursMax = 2160`），拦不住「配小了」，而配小了要等到第一笔真实拒付才看得见——所以默认值必须自己站在安全的那一侧，且改它的人得先撞一次那条断言。720 是折中不是安全值：卡组织规则允许持卡人在结算后 120 天内发起争议，全覆盖要 2880 小时，但那意味着供给者干完活四个月才拿得到钱，没有人会来供货；实际拒付绝大多数落在交易后一个月内，漏掉的尾巴落在 `payment_disputes.uncovered_basis` 上。**上线一个月后按那一列的真实累计值再调**。改的是默认值，已存过设置的部署不受影响 |
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
| 234（**2026-08-21 补**） | `supplier_payout_wallet_repo_integration_test.go`（12 例，新文件）、`supplier_payout_wallet_repo_test.go`（2 例，新文件、不带 tag） | 这张表的全部意义是两条唯一索引，而 sqlmock 对索引一无所知——它只会把 `ON CONFLICT` 当字符串还回来。真库证的是：`ON CONFLICT (user_id, network)` 真的推断得到那条索引，于是换绑是**原地改**而不是多出一行（`payoutWalletRowCount` 恒为 1）；另一条 `UNIQUE (network, address_hash)` 上的冲突**必须**抛出来而不被 `ON CONFLICT` 吞掉，那正是"这个地址是别人的"；直读 `address` 列拿到的是 `enc.v1:…`、里面搜不到地址原文，而 `address_hash` 是一列可比对的明文哈希——**这正是一份 `pg_dump` 会看到的东西**（哈希列自己算，不调 `payoutWalletHash`，否则是拿实现证实现）；同一个地址的大小写变体撞在同一行上（盲索引真的归一化了）；换绑成自己已绑的同一个地址是幂等放行而不是"已被占用"；同一地址在**另一条链**上可以再绑（唯一约束带 network）；地址非法时一行都不写；解绑之后那个地址立刻可以被别人绑走；解绑不存在的绑定报 `NotFound` 而不是静默成功。最要紧的一例是 `UniqueIndexBitesAndIsRecognized`：绕过仓储直插一行重复地址，断言 `isUniqueConstraintViolation` 真的从一个**真的** `*pq.Error` 上认出了 `23505`——没有它，仓储里那个并发兜底分支是永远不执行的死代码 |
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

收款地址绑定（§3.11）分三层各跑一轮变异，共 **29 处**，每处都能编译，源码一律用 `shutil.copy` 从备份还原并以 `diff -q` 验证字节一致（不用 `git checkout --`：那会把同一轮里其它未提交的改动一起抹掉）。领域校验 10 处（去掉 `0x` 前缀判断 / 长度放宽 / 不校验十六进制 / 跳过 EIP-55 / 用 `sha3.New256` 而不是 `NewLegacyKeccak256`（这两个函数只差一个填充字节，Go 里长得几乎一样，而换错的表现是**所有**混合大小写地址都被判成校验和错）/ 零地址放行 / 全大写不当成"未加校验和"等）。仓储 9 处（盲索引不归一化大小写 / 前置反查漏判 owner / `ON CONFLICT` 推到错的索引上 / 不认 23505 / 插入时跳过 `seal` / 解绑不报 NotFound 等）。服务与提现取地址 10 处（没绑地址时回落到手填值 / 读绑定出错时吞掉错误 / 绑定服务缺失时回落 / 链上渠道仍优先用手填值 / 取地址排到渠道白名单之前 / `Unbind` 没装配仓储也算成功 / `GetOptions` 把 nil 列表原样吐出 / options 直接吐注册表本身（调用方拿到的是那个切片而不是副本，改一下就改了全站）等）。

两处值得记下来：**「盲索引不归一化大小写」第一轮活了下来**，那是真的测试缺口而不是等价变异——`payoutWalletHash` 里的 `strings.ToLower` 在当前调用路径上不可达（`Upsert` 上游已经归一化过），补法是加一个直接测那个函数的单测，理由见 §3.11。**「前置反查漏判 owner」第一轮 BUILD-FAIL**：把 `if found && owner != userID` 改成 `if found` 会让 `owner` 变成未使用变量，编译不过；换成 `if found && owner >= 0`（同样放行所有情况，但用到了那个变量）之后一次就红。一个编译不过的变异什么也没证明，这类必须重写而不是记成"捕获"。

**仍未验证的**（不在真库能力范围内，需要活的上游）：真实 OAuth 授权码兑换。

## 9. 路由装配的验证实录（2026-08-20）

`internal/server/routes/` 此前对供给侧**零覆盖**。这一层出错的方式恰好是最安静的一类：路由挂错组、少一层中间件、新加的一条忘了跟着挂——三种都不会让任何 handler 报错，只会让一条本该要登录的接口不要登录，或者一条会动余额的写接口不进审计日志。

`supplier_test.go`（不带 build tag，与 `internal/server/routes` 其余测试一致，`make test-unit` 与 `make test-integration` 两边都跑到）分两类断言：

**行为类**——从 `router.Routes()` 反推，而不是照着 `supplier.go` 抄一遍路径清单。后者只能证明"我写的和我写的一样"，前者对将来新增的每一条路由自动生效，而"新加的一条忘了挂"正是要防的那种遗漏。三条：全部用户路由（写下这段时十九条，收款地址绑定上线后从十六条增至十九，见 §9.3）每一条都真的走过 JWT 与审计（审计桩 abort 掉，于是依赖全空的 handler 一次也没被调用）；未登录时一条不漏地 401；管理端十一条挂在 admin 组下，未登录同样一条都进不去。

这三条**不数数**是刻意的：它们遍历注册结果，新增的路由自动进入循环。代价是它们对"少了一条"完全无感——一条被误删的路由在它们眼里等于零次循环，安静地全绿。那一半由清单类断言补（管理端是计数，用户侧收款地址那三条是逐条点名）。

**清单类**——三处只能从源码读，因为它们在装配测试里**观测不到**：

| 钉住的东西 | 为什么行为测不出来 |
|---|---|
| 中间件链与 `RegisterUserRoutes` 逐字相同（含顺序） | `BackendModeUserGuard(nil)` 与 `panelRateLimiter.Global()` 拿到 nil 依赖时都直接 `c.Next()`，与"根本没挂"是同一个观测结果。顺序同理：审计必须**最后**，否则被前面几层挡下的请求会在审计日志里留下一条根本没发生的"某某访问了提现接口" |
| Heavy 限流的挂载点恰好是 `oauth/start` / `oauth/complete` / `POST /withdrawals` / `PUT /payout-wallets/:network` | `Heavy()` 与 `Global()` 在没有 Redis 的测试里都放行，注册结果里分辨不出谁是谁。而这几条的**反面**（撤回提现、解绑账号、解绑收款地址、同意协议不套 Heavy）最容易被"顺手加一层更安全"改掉——那等于在供给者最急着把钱拿回来、最想撤回授权的时候让他做不到，测试因此把这四条也单独点名 |
| 管理端的写路径只有提现审批那两条 POST | §3.6 那条边界（整层只读，唯一例外是提现审批）在后端唯一的落点。前端有一条同形状的断言钉住 API 客户端的写方法清单；两边都要求"加一条写接口先改断言"，也就是先停下来想一下这个写动作该不该出现在一个看板里 |

那条"管理端一共几条"的计数断言在加对账导出（§3.9）时立刻变红了——两条新的 GET 一挂上去，`should have 7 item(s), but has 9` 就把改动拦在了提交之前。这正是它存在的理由：改断言那一步强迫人回答"新加的这两条该不该出现在这个组里"，而答案（该，因为它们要跟着走审计中间件）是这次唯一需要想清楚的事。失效运营视图（§3.10）上线时它第二次变红——`should have 9 item(s), but has 11`，同样的两条 GET、同样的一次追问。两次都只改了那个数字，但两次都是在那一刻才确认「这条新接口进的是带审计的管理端组」，而不是上线三个月后从日志里发现它没进。

加上三道判空（`h` / `h.Supplier` / `h.Supplier.Admin` 任一为 nil 时一条路由都不注册）：wire 装配失误时正确的表现是这些接口 404，不是一打就 502。管理端那半边直接调 `registerSupplyMarketRoutes` 而不走 `RegisterAdminRoutes`，因为后者在 `h.Admin` 为 nil 时会自己先崩（上游既有行为，每个 `registerXxxRoutes` 都直接读 `h.Admin.<字段>`），那样测的就成了上游的容错。

### 9.1 「我是谁」只有一个来源

路由挂对了，还剩另一半：handler 自己会不会认一个来自请求的身份。`internal/handler/supplier_identity_test.go` 只证这一件事。

它单独成文，是因为这条性质失守时**没有任何症状**。多读一个 `user_id` 入参不会让任何测试变红、不会让任何请求报错，只会让一个人能把账号挂到别人名下、能查别人的流水、能把钱提到自己这里——三件事看起来都像功能正常工作。行为测试也覆盖不到：要发现它，得先想到去构造一个"带着 A 的 token、请求体里写 B"的请求，也就是得先怀疑这段代码。

因此断言是**结构性**的——不检查某次调用的结果，检查这段代码里有没有第二条路径可以回答"我是谁"：

- `*SupplierHandler` 上**每一个**导出方法（写下这段时 19 个，一一对应十九条用户路由；`ListWithdrawals` 用户侧与管理侧同名，但分属两个类型，管理侧那个不在此列）都走 `h.currentUserID(c)` 或 `h.mutateAccount(...)`。收敛到一个入口，是让"取不到 id 时怎么办"这个决定只有一份——十九份实现里迟早有一份忘了在 `!ok` 时 return，于是它带着 `userID=0` 往下走。
- `mutateAccount` 自己第一件事就是取 id。没有这条，上一条可以被"随便加个走 mutateAccount 的方法"绕过。
- 用户侧一行都不从请求里读 `user_id`（query / form / param / json tag 四种写法各查一遍）。这是上一条的另一半：上面证的是"正确的来源被用了"，这条证的是"没有第二个来源"——一个方法完全可以既调 `currentUserID`、又在请求体里认一个 `user_id` 覆盖掉它。
- 用户侧直接读 JWT 上下文的地方**恰好一处**。管理侧（`reviewerID` 之后那半边）不在此列：那里的 `user_id` 是筛子，是运营视图该有的东西，鉴权在路由组的 adminAuth 上。

4 处变异反证：某个端点绕过 `currentUserID` 直接读 JWT 上下文 / 请求体结构里多一个 `user_id` 字段 / `mutateAccount` 先动账号再确定是谁 / 提现申请改从查询参数拿 `user_id`。

**这份"用户侧有哪些文件"的清单在 2026-08-21 从手抄改成了从文件系统发现**（扫包内非测试的 `.go`，留下含 `func (h *SupplierHandler)` 的）。原因是它此前列着两个写死的文件名，而它要防的恰恰是"有人新加了一个端点、忘了把它纳进来"——一份手抄的清单在那种时候不会报错，只会安静地少测一个文件。也就是说这套断言本身有一个和它要防的那个漏洞一模一样的漏洞。加收款地址绑定那个文件时正好撞上：三个新端点在被显式加进清单之前，完全在这四条断言的视野之外。`supplier_admin_handler.go` 自然不会被选中——它挂的是 `*SupplierAdminHandler`，管理侧的 `user_id` 是筛子。

### 9.2 变异清单

路由层 12 处变异逐条反证：去掉审计层 / 把路由挂到未认证的 `v1` 组上 / `POST /withdrawals` 丢掉 Heavy / 给 `DELETE /accounts/:id` 套上 Heavy / 运营视图多一条写接口 / 少一条只读接口 / 去掉两道判空各一次 / 去掉后台模式闸 / 去掉面板限流 / 把审计挪到 JWT 之前 / **在 `user.go` 里给用户组加一层而供给侧不动**——每一条都被对应用例逮住。最后那条是这组测试存在的主要理由，它模拟的就是下一次同步上游时最可能发生的事。

### 9.3 收款地址绑定的装配与 HTTP 层（2026-08-21）

三条新路由（§3.11、#9b）让 `supplier_test.go` 里那条 Heavy 清单断言**立刻变红**——`PUT /payout-wallets/:network` 一挂上去就多出了第四项。这是它第三次拦下一次改动，和前两次一样，改断言那一步强迫人回答"这条新接口为什么该套重限流"，而答案（该，它是供给侧唯一一个会往带唯一索引的表里写行的接口）是这次唯一需要想清楚的事。

新增的两条清单断言补的是循环类断言够不到的地方：

- **全站含 `payout-wallet` 的路由恰好是用户侧那三条。** 断言横跨用户端与管理端两次注册，因为要钉住的那件事只有合起来看才成立（§3.11：管理端一条都不能有）。它同时补上了循环类断言的另一半——"该有的都在"：一条被误删的路由在循环里等于零次循环，安静地全绿。
- **`payout-wallets` 下的路径参数只能是 `:network`。** 地址一旦进了路径就会进 access log、反代日志和浏览器历史，而一条能把某个账户的钱全部取走的地址不该出现在这三个地方里的任何一个。这条从注册结果读而不是从源码读，因为要证的恰好是路由树里那个参数段的名字。

`concreteRequestPath` 顺带学会了按参数名填值（`:network` → `bsc`）。这不影响任何断言（gin 匹配任意非空片段，而这些测试全在中间件层收尾），只是让失败信息里出现的是 `/payout-wallets/bsc` 而不是 `/payout-wallets/1`——后者会让读日志的人以为链号是个整数。

**路由层 9 处变异**逐条反证：三条各自被挂到未认证的 `v1` 组上 / 绑定丢掉 Heavy / 解绑被顺手加上 Heavy / 地址挪进路径参数 / 读绑定那条被删掉 / 解绑那条被删掉 / 管理端多出一条能看绑定的 GET。

**HTTP 层 10 处变异**（`supplier_payout_wallet_handler_test.go`，新文件）：不认识的链退化成 404 / 链根本不校验 / 装配失误报成"不支持这条链" / 绑定先解请求体后校验链 / 解绑静默成功 / 解绑回的 `bound` 写成 true / 请求体坏了照样往下走 / 绑定改认请求体里的 `user_id` / 读绑定出错时报成空列表 / 空绑定列表回成 null。

其中三条第一轮没被逮住，修法各不相同，都记在这里：

**「链根本不校验」与「先解请求体后校验链」活下来**，是因为 service 层也校验链，单看一个格式正确的请求，两层的结果一模一样——handler 那道检查看起来是纯冗余。它不是：差别在**顺序**上。链传错了且请求体也坏了时，正确的答案是"链不对"，而不是让调用方先改好请求体、再发一次才发现真正的错在路径上。补了一条 `NetworkCheckedBeforeBody`，两个变异一起变红。这也是那道看似冗余的检查存在的唯一证据。

**「请求体坏了照样往下走」活下来**，是因为解不开的请求体留下一个空 `Address`，而空地址恰好也过不了校验——于是一个 JSON 语法错误被报成 `SUPPLIER_PAYOUT_ADDRESS_INVALID`，前端拿着它去让用户重新粘一个完全正确的地址。原测试只断言了 400 和"没写库"，两者在变异下都成立。补法是断言**报的是哪一个错**。

顺带修掉一处真的错：三个 handler 在服务没装配时回的是 `SUPPLIER_PAYOUT_NETWORK_INVALID`（400）。那会把一次 wire 装配失误显示成"不支持这条链"——用户去换链重试，运维去查链配置，而真正坏掉的东西不在这两个地方的任何一个。改成新的 `ErrSupplierPayoutWalletUnavailable`（503），并让 service 的 `unavailable()` 返回同一个哨兵，两层不再各说各话。

### 9.4 收款地址绑定的前端（2026-08-21）

**先修了后端一处会静默传染到前端的错：`SupplierOnchainChannel` 三个字段一个 json tag 都没有。** 它**原样**出现在两个用户可见的响应里（提现 options 的 `onchain_channels`、绑定表单的 `channels`），没有 tag 时 Go 拿导出名当键发出去——`Channel` / `Network` / `Token`，与这个 API 里其他每一个字段的写法都不一样。编译过、测试过、请求 200，只有前端按惯例写的 `channel` 永远读到 `undefined`，于是渠道名渲染成一片空白。加了 tag，并补一条 `TestPayoutWireShapeIsSnakeCase` 用键的**全集**钉住三个结构体的线上形状（多一个字段和少一个字段一样值得看一眼——这两个响应带着一个人的链上身份）。`Token` 的 json 名刻意是 `token_symbol`：M3 起单子上会多一个 `token_address`，那时一个叫 `token` 的字段指哪个就要靠读文档才知道。

**表单在"链上渠道"与"人工渠道"之间分岔，链上那一侧根本不存在自由输入框。** 后端在建单时会用 `ResolvePayoutAddress` 覆盖掉手填的账号（§3.11），所以这不是唯一一道门；但那道门在**建单**这一刻才生效，前端这道在**画表单**这一刻就生效——两道挡的是不同的东西：后端挡住"钱打错地方"，前端挡住"让他以为该手填"。判据取自 `withdrawalOptions.onchain_channels`（与渠道白名单同一个响应），不取自绑定接口一起返回的那份 `channels`：两个接口之间的时间差会画出「渠道列表里没有 BSC-USDT、下面却在催你绑 bsc 地址」这种自相矛盾的界面。

另外三处决定：**地址原样提交，前端不做 `toLowerCase`**——EIP-55 的校验和就编码在字母的大小写里，前端归一化一下，"你改过其中一位"这个唯一能挡住不可逆损失的信号就在到达后端之前消失了。**绑定成功后显示后端返回的那一串**，不是用户手里那串：两者不一致时他会以为自己绑的是屏幕上这个形态，下次跟交易记录对不上。**已绑定时输入框收起来**，换绑要先点一下且不预填旧地址——地址是看一眼就够的东西，让它一直摊在可编辑的输入框里只是多一次误改机会。

**没有链上渠道的部署一次也不打绑定接口**（`onchain_channels` 为空就跳过），代价是多一趟往返、只发生在用得上的部署里；收益是一个没装配起来的绑定服务不会让每次进页面都弹一条与用户无关的 503。

**这一层顺手逮到一个已经上线的真 bug。** `submitWithdrawal` 里写着 `withdrawalForm.value.amount.trim()`，而 `amount` 的运行时类型是 `string | number`——Vue 的 `vModelText` 对 `<input type="number">` 会自动套上 `.number` 的转换（`castToNumber = number || props.type === 'number'`），填了数字之后拿到的是 `number`，只有清空时才留下空串。也就是说**每一次真实的提现提交**都在那行抛 `TypeError`，按钮看起来毫无反应。TS 看不见它，因为 v-model 的写入绕过了 ref 的类型；此前也没有任何一个测试挂载过这个视图。改成先 `String(...)` 再判空，并把 ref 的类型改成 `string | number` 让它说实话。

**前端 13 处变异**（`SupplyView.payoutWallet.spec.ts` 11 条 + `api/__tests__/supply.spec.ts` 新增 6 条）逐条反证：绑定改用 POST / 地址塞进 URL / 前端先把地址转小写 / 链名不转义直接拼进路径 / 链上渠道也画自由输入框 / 人工渠道也被当成链上渠道 / 链上提现改送表单里手填的账号 / 没绑地址也放行建单 / 绑定后显示手里那串 / 已绑定时仍然摊开可编辑输入框 / 没有链上渠道的部署也去问绑定 / 金额当成一定是字符串 / 绑定时连 trim 都不做。

### 9.5 链上客户端（2026-08-21）

**没有引 go-ethereum。** 它会带进 7 个新模块、强制升级 3 个已有依赖，其中 c-kzg / blst 是 cgo 的——而本项目的发布产物是 `CGO_ENABLED=0` 的静态二进制。换来的是一整套用不到的东西：EVM、状态树、P2P、共识。真正需要的面很窄：一种交易类型（EIP-155 legacy）、五个 JSON-RPC 方法、四个合约调用。

自己写的**只有 RLP 和 ABI 这两段纯编码**。真正难的两块没自己写：secp256k1 用 `decred/dcrd/dcrec/secp256k1/v4`（已在依赖图里，且正是 go-ethereum 非 cgo 路径用的同一个库），keccak256 用 `golang.org/x/crypto/sha3`（已是直接依赖）。**新增模块数：0。**

这套取舍的安全性来自它的失败模式：编码错了，签名对不上、节点直接拒收，链上什么都不会发生。唯一会"钱打错地方"的是 ABI 里收款地址那一段，而那一段被**金标向量逐字节钉住**——EIP-155 官方参考交易、一笔真实形态的 BSC USDT transfer、`disperseToken` 的完整字节串。**全部在第一次运行就对上了**，这是"手写编码等价于 go-ethereum"这个说法唯一的实证。

顺带钉住一件容易错到没人发现的事：`sha3.NewLegacyKeccak256()` 不是 `sha3.New256()`。两者签名一样、都编译、都是 32 字节，空输入的结果分别是 `c5d24601…` 和 `a7ffc6f8…`。用错的表现只是"节点说签名无效"。测试用空输入向量把这条钉死。

**选 legacy 交易而不是 EIP-1559**，理由不在链上而在 M3：手续费要从供给者收益里扣，legacy 的费用是一次乘法（gasPrice × gasLimit），扣多少在建单那一刻就是确定的；1559 的实际花费取决于出块时的 baseFee，会让"扣了多少"和"实际花了多少"永远差一点。BSC 上两者的价格差可以忽略。

**这个包里唯一真正难的东西是 nonce。** 用同一个 nonce 重签重发 → 链上认得出是同一笔，最多上一次；换一个新 nonce 重发 → 两笔独立的转账，供给者收到两次钱。三条防线都是可测的：

- **广播失败时返回的是本地算出来的哈希**，不是节点回的。哈希在签名那一刻就定了，广播超时（连接断、502、网关吞了响应）时我们不知道节点看没看到——但只要哈希记下来了，下一轮就能去查收据，而不是当成"没发过"再发一笔。
- **`already known` / `nonce too low` 这类回应不算失败。** 它们的含义正是"这笔已经在池子里了"，判成失败会让上层重发。
- **`WaitForConfirmation` 等不到时返回错误，不返回 `failed`。** 返回错误 = 还不知道；判成 `failed` 会让一笔可能已经成功的转账被退款，等于双付。链上明确 revert（收据在、`status=0x0`）是**唯一**一种可以放心退款的失败。

**节点拒绝与传输失败是两种东西**，因为它们的正确应对相反。前者是 JSON-RPC 的 `error` 字段（节点看过了、说不行），后者是超时 / 连接断 / 502 / 网关回的 HTML（不知道节点看没看到）。这条差别做成了类型上的区分——`*rpcError` 只在前一种情况下出现，`asRPCError` 是唯一的判据。

**链头倒退单独挡了一次。** 多节点负载均衡后面，两次 `eth_blockNumber` 可能落在不同高度上，后一次比前一次矮。`head - mined + 1` 在无符号数上会下溢成天文数字，于是一笔刚上链一个块的交易被判成"确认充分"。判据是 `if head < mined { return false, nil }`——当成还不够深继续等。

**配置只从环境变量读，不进 `internal/config`。** 那个包里的字段全带 `mapstructure` 标签，意味着它们可以从配置文件来、也可能被写回配置文件；金库私钥不能沾这条路。私钥连一次都不会被格式化进任何字符串——错误消息里只有长度，没有内容。

**默认落在拒绝那一边，而且 `PAYOUT_MOCK` 单独设置不生效。** 没配好是最常见的运维状态（新环境、密钥没注入、RPC 写错），它必须落到「每个会动钱的方法都拒绝」。如果默认是假装成功，表现就是工单被安静地标成已付、供给者余额被清零，而链上什么都没发生，还没有任何一条错误日志。假客户端要 `PAYOUT_ENABLED` 与 `PAYOUT_MOCK` **同时**为真才拿得到——一个环境变量被误抄进生产配置是会发生的事，要两个同时抄错才会让生产环境假装打款；只设 `PAYOUT_MOCK` 的那种误配落到 Disabled，拒绝，看得见。

**本轮没有把真客户端接进 DI。** 消费者要到 M4 的 worker 才出现，现在接进去就是一个死变量（而 wire 会把没有消费者的 provider 整个剪掉，见 #5a/#5c 那个坑）。改成在 `main.go` 里做一次启动自检（#11），把配置错误提前到启动那一刻。

**变异 44 处逐条反证**，覆盖编码、签名、RPC、客户端、配置五层：

- **RLP（4）**：整数不去前导零 / 大整数的零编成 `0x00` / 整数的零留一个 `0x00` / 列表长度前缀数项数而不是字节数 / 短串上界写成 55。
- **ABI（8）**：用 NIST SHA3 而不是 keccak（选择器、哈希各一次）/ 字左对齐而不是右对齐（地址错位）/ `values` 偏移写死成 `0xc0` / 超 256 位截断 / 负数放行 / 短返回值补零当 0 读 / 批量长度不一致不拦。
- **签名（9）**：`v` 里不带 chainID（退回 EIP-155 之前）/ chainID 只乘一次 / 待签名负载末尾少两个零 / 负载里不带 chainID / 地址取摘要前 20 字节 / 算地址时没去掉 `0x04` 前缀 / chainID 为 0 也肯签 / 私钥回显进错误消息 / 全零私钥放行 / 交易哈希算错。
- **RPC（3）**：取 nonce 用 `latest` 而不是 `pending` / HTTP 错误码当成节点拒绝 / 空的十六进制数量当 0 读。
- **客户端（10）**：无视调用方给的 nonce / 确认超时判成 `failed` / 链头倒退不拦 / 真拒绝当成"已广播过" / "已广播过"当成失败 / 精度写死 18 / 零地址放行 / 别的链也照发 / 批量人数上限不拦 / approve 不等确认 / 估价失败报错而不是回落。
- **配置与工厂（7）**：开关认任何非空值为真 / 写错的数字静默回落 / 启用时不校验必填项 / 零个确认放行 / 没配好时默认给 mock / 单独一个 mock 开关就够 / 造不出真客户端时给 mock。

其中两条第一轮没被逮住，都不是补一条断言就完事：

**「确认超时判成 failed」活了下来**，而这个包里最不能出错的判断恰好就是它。测试确实存在，但它打在了错误的分支上：`pollEvery` 是 1ms、ctx 是 30ms，本地 httptest 又快到微秒级，于是 ctx 总在某次 HTTP 调用**当中**过期，函数从"节点连不上"那条路返回——那条路另有测试，而真正的超时分支（`select` 里的 `ctx.Done()`）根本没人走过。这也是生产里唯一会发生的那条路（`pollEvery` 是 3 秒，收据查询是毫秒级，等待必然停在 `select` 上）。改法是把轮询间隔调得**比 ctx 长**，让第一次收据查询顺利返回 nil 然后必然停在 `select` 上，并补断言 `errors.Is(err, context.DeadlineExceeded)` 与"错误里带交易哈希"。

**「零编成 `0x00`」的第一版是个等价变异。** 把 `rlpBig` 里的 `value.Sign() == 0` 条件删掉之后测试仍然全绿——因为 `big.NewInt(0).Bytes()` 本来就是空切片，两条路给出同一个 `0x80`。这不是测试的漏洞，是变异写错了：它没有改变任何行为。真正能把 0 编错的写法是让零分支返回 `rlpBytes([]byte{0})`，以及让 `trimLeadingZeros` 留下最后一个零字节——换成这两条，都是红的。**一个 GREEN 的变异要先问它是不是等价变异，再去改测试**，否则会为了一个不存在的漏洞加一条永远为真的断言。

同一轮还修掉一个纯属自己写错的锚点（`if head < mined {` 那行在源码里跟着两行注释，整段匹配落空报了 `ANCHOR-BAD`）。锚点匹配不上要与 GREEN 分开计——它证明的不是测试弱，而是变异根本没被应用。

服务层那 16 处变异（M1 建立的矩阵）本轮重跑仍是 16/16 红，其中「换算改用一次浮点乘法」的反例在本轮被换成了实测出来的两个（`0.47` 与 `0.00000011`）：原来那个反例（`0.07`）是照 18 位精度挑的，而变异实际发生在 8 位精度上，于是它逃了一次。反例要从**变异真正发生的那个精度**上扫出来，不能凭直觉挑。

### 9.6 建单时的链上快照（M3，2026-08-22）

M1 定下「钱打到哪」，M2 造出「谁去打」，M3 是把两者钉在单子上的那一刻：`network` / `token_symbol` / `token_address` / `fee_amount` 四列，由 `SupplierWithdrawalService.applyChainSnapshot` 在建单**之前**填好，随建单参数一起进那一个事务（迁移 234 的列早就在，本轮一列没加）。

**四列是一组，写就四列齐全，不写就一列不写。** worker（M4）靠 `network` 捞单、靠 `token_address` 决定发哪个合约的币；只写一半会留下一批捞得到、却打不出去的半成品行。而它写错的两个方向都不报错、都没有日志，都要等 worker 跑起来才看得见：多写了（本该人工的单子带上 `network`）是钱扣了却推不动，少写了（本该上链的没写全）是单子安静地躺在人工队列里，而供给者以为几分钟到账。

**唯一真正需要决定的事：链上渠道 + 链上客户端结算不了这种币时，建单该不该失败。** 上一轮的注释写的是"必须失败关闭"，本轮改成了**退回人工工单**（四列全留零值，单子照建）。理由有两条：

- 报错等于**把一条本来走得通的路关掉**。M1/M2 期间「BSC-USDT 进白名单、运营看着绑定地址手工转账」是一条完整可用的路径，它不该因为 M3 上线而变成 503——一个没配金库的部署会突然提不了现，而它昨天还好好的。
- 失败关闭想防的那件事在这里**不可能发生**。只要没写 `network`，M4 的 worker 就永远捞不到这张单子，也就不存在"钱扣了、worker 打不出去"的卡死。留白比报错既更安全也更少破坏。

同一个判据（`settleOnChain`：渠道在注册表里 + 这套部署接了客户端 + 客户端认这条链上的这种币）三处共用：建单、表单的手续费报价、以及"这个渠道此刻自动不自动"。三道缺一不可——只看前两道，会在一个金库里装着 USDC 的部署上把单子标成 USDT 的。

**收款地址的来源与结算方式是两件事，分开判。** `resolveOnchainAccount` 管前者（拿不到绑定地址一律**失败关闭**，M1 那条规矩不动），`applyChainSnapshot` 管后者（结算不了就退人工）。合成一处判断的话，"没配金库"会顺带把"必须用绑定地址"也关掉，于是手填地址又溜回来了。

**手续费是从 `amount` 内部切出去的，不是加在它外面。** `amount` 仍然是从可用区扣走的总额——ledger 的 `withdraw` 流水、退款、对账导出全按它走；链上实发 = `amount - fee_amount`；被拒时按 `amount` **全额**退（gas 还没花出去）。`NetAmount()` 是派生的，不落库：落一个能由另外两列算出来的数，就多了一处能与它们不一致的地方，而不一致时没人知道该信哪个。

**`fee >= amount` 拒绝建单，取 `>=` 而不是 `>`**：实发 0 的转账照样要烧一次 gas，那笔 gas 是平台白花的，换回一张链上金额为零的交易记录。报 4xx（`FEE_EXCEEDS_AMOUNT`）——这不是故障，是"提的太少了"，正确动作在调用方手里。

**估不出手续费（NaN / 无穷 / 负数）报 503，不当成 0。** 当成 0 是在说"这个渠道不收手续费"，而真相是链上客户端算出了一个不是数的东西（币价配成 0、RPC 回了垃圾）。前者静默地让金库替所有人垫 gas 且没有任何日志，后者是个能被运维看见并修好的故障。同一条规矩在表单那边的形态是**整条略过**而不是报一个 0：`onchain_fees` 与 `onchain_channels` 的**差集**才是前端唯一能据以判断"这个渠道现在走人工、全额到账、慢一点"的东西。

**手续费落库前先收敛到 8 位（`SanitizeChainFee`）。** 估算值来自一串浮点乘法，小数位能有二十几位；不收敛就交给仓储，`DECIMAL(20,8)` 那一列会替我们截断，于是**服务算出来的 net 与按库里那一行算出来的 net 不是同一个数**——而 M4 打款读的是库里那一行。差值在 1e-9 量级，小到不影响任何人的钱，大到足够让两个本该相等的数不相等。收敛方向取四舍五入而不是向上：在 1e-8 美元这个量级上多收少收都不是钱，"服务与数据库算出同一个数"才是要的东西。测试里连**幂等**都钉了（收敛过的值再收敛一次是它自己），因为那正是这句话的另一种说法。

**快照放在 `Request` 的最后一步**，在每一道免费校验之后：估 gas 是这条路径上唯一可能触网的动作，而开关、渠道白名单、备注长度、金额下限全都能在不花任何代价的情况下否掉这次申请。反过来先估价再校验，等于让每一次填错金额都去问一次链。

**报价不是承诺。** `onchain_fees` 与建单时各算一次是刻意的（两者之间隔着一次 gas 波动），但**判据**必须是同一个：如果表单说"这个渠道自动打款、手续费 0.3"而建单走了人工路径，供给者会盯着一个永远不动的"预计几分钟到账"。把报价锁住（缓存 5 分钟然后照它扣）只会让"界面上写的"和"链上真花的"在一个更长的窗口里对不上——界面该说的是"预计"，不是"将会"。

**装配：`payoutchain.ProviderSet` 本轮接进 wire。** M2 时它还是死变量（wire 会把没有消费者的 provider 整个剪掉），现在提现服务就是那个消费者。工厂拆成 `build`（挑客户端，**不触网**）与 `Resolve`（build + 向节点确认链 ID），让「这套部署是 disabled / mock / live 中的哪一种」只有一处判断——否则启动日志说 LIVE、而注入进服务的那个是 Disabled，这种不一致查起来会要命，因为日志本身就是排查的起点。`ProvideChainClient` **不返回错误**（provider 一报错整个进程起不来，而这里能出的错全是"打款配好没"那一类，不该让所有用户都登不上）、**不问节点**（VerifyChain 留给启动自检那一次，它有自己的超时且失败不致命）。`ProviderSet` 只导出客户端本身，不导出 `Resolved`：`Mode` 与 `Summary` 是给启动日志的，让它们进 DI 图等于给了业务代码一个"如果是 mock 就……"的分支口。

**一条踩到的坑：手写注释活不过 `make generate`。** `cmd/server/wire_gen.go` 里三条前几轮加的中文注释在这次重新生成时被**静默删掉**了。改法不是再手改一遍生成文件，而是把带设计意图的那两条搬进手维护的 `internal/repository/wire.go`（第三条只是在描述生成结果，直接弃掉）。**生成文件里不要留任何只存在于那里的信息**——它下一次 `make generate` 就没了，而没有人会注意到。

**变异矩阵：服务层 17 个 + DTO 层 4 个，21/21 全红，没有等价变异。** 服务层那 17 个（`/tmp` 里两批跑完，每跑一个还原一次、结尾三方 diff 确认全还原）按靶子分四组：快照写入 7 个（漏写 network / token_symbol / fee_amount、token_address 落成币种符号、`>=` 松成 `>`、估不出手续费当 0 放行、不收敛精度）、settleOnChain 判据 3 个（不问客户端、漏判 nil 客户端、漏判人工渠道）、报价 3 个（估不出当 0、不管能不能结算全报、空时发 null）、SanitizeChainFee 与派生量 4 个（放行非有限值、放行负数、OnChain 把空白 network 当链、NetAmount 忘减——后两个在领域类型上）。值得记的是抓住它们的测试高度集中：报价那 3 个几乎全折在 `TestWithdrawalOptionsOmitsUnsettleableChannelsFromQuotes` 的四个子测上——"差集语义"这一个断言面，单独看着最像多余，杀伤最大。

**DTO 那 4 个变异（M18–M21）是这一层存在的证明。** 供给侧的 `supplierWithdrawalView` 本轮补上 `fee_amount`（**不带** omitempty：人工单上它是 0，而 0 与 undefined 对前端不是一回事，后者做减法得到 NaN）、`net_amount`（由 `NetAmount()` 算——钱的公式只在领域层活一份，抄进 TypeScript 就有第二份）、`network` / `token_symbol`（界面靠它说"几分钟"还是"工作日内"）；刻意**不带** `token_address`（结算细节，管理端与对账导出拿得到）。四个变异分别是：净额忘减、fee_amount 加 omitempty、不映射 fee_amount、token_address 跟着漏出去——全部被 handler 层的白名单式 wire-shape 测试（`TestWithdrawalViewWireShape` 等 4 条）当场抓住。白名单而不是逐个 Contains，因为这一层真正要防的是**多**发（reviewer_id 就在领域对象上，一次顺手的字段复制就会把它带出门），而多发只有全集断言能看见。

**前端把手续费亮了两次**：表单里选中链上渠道时给报价 + 按当前金额算的到账预览（金额盖不住手续费时提前警告，与后端 `>=` 拒绝同一条线）；列表里对 `fee_amount > 0` 的单子写一行「手续费 X · 到账 Y」——Y 用的是响应里的 `net_amount`，spec 里刻意喂了一个不等于 `amount - fee` 的假 net 来证明前端没有自己重算。拿不到报价时说的是「转人工打款」，**绝不显示 0**——0 元手续费是一个承诺，而后端没做过这个承诺。9 条 vitest（`SupplyView.withdrawalFee.spec.ts`）+ 类型检查全绿。

**真库两条（延续 §3.11 的规矩：raw SQL 的分支只有真 Postgres 能证伪）**：一条钉「四列写进去、读出来、终态推进后原样还在，且**按库里那一行算的 net 与服务层是同一个数**」——fee 喂的是把 8 位小数占满的 0.12345678，截断类回归只在最后几位现形，而 sqlmock 里的 DECIMAL 是我以为的 DECIMAL；另一条钉「人工单三列落 **NULL**、fee 落 0，不是空串」——M4 的 worker 用 `network IS NOT NULL` 捞单，空串会让一张人工单变成一张链不存在的链上单，worker 捞到、打不出去、无限重试，而空串与 NULL 的区别在应用层根本看不见。连同旧的 16 条提现真库用例一起全绿。

### 9.7 链上打款 worker（M4，2026-08-24）

M3 把四项快照钉在单子上，M4 是把钱真正发出去的那一段：`SupplierPayoutWorker` 每 30 秒扫一轮（选主 + 租约），每张单子走五步——捞单（只续租）→ 预留 nonce（翻 `processing`）→ 广播 → 记哈希 → 等确认（`paid` / `failed`）。任何一步的答案是「还不知道」，就交还租约留给下一轮。迁移 235 只补两样：`broadcasted_at`（放弃等确认的时钟，只写一次）和捞单谓词的部分索引；状态机、租约、nonce、tx_hash 的列 234 早就备好。

**整个 M4 只有一条不能错的排序：nonce 在广播之前落库，且重试原样复用。** `BeginPayout` 是广播前的最后一道闸，一条条件更新做三件事：钉 nonce（`chain_nonce IS NULL OR chain_nonce = $2`——同号幂等、换号拒绝）、翻 `processing`、以及用「命中零行」告诉 worker 单子已被别人处理掉。它落不下去的两种原因（条件没中 / 库不可达）应对相同：**不许广播**。反过来，哈希落不下去只是多一轮重播——同一个 nonce 链上最多认一笔，重播代价为零。两个写库动作的失败处理因此完全不同，这不是不一致，是各自失败模式的镜像。

**「还不知道」永远不是终态。** 广播报错（传输失败，节点看没看到不知道）→ 退避重试；确认等不到（`WaitForConfirmation` 回错误）→ 退避重试；确认回一个不认识的状态（客户端违约）→ 也按还不知道处理。唯一允许写 `failed` 的三种情形：链上明确 revert（收据在、status=0）、快照残缺 / 净额非正（只可能是手改库）、以及**放弃期限到了**。

**放弃期限（30 分钟）是为一个真实的死角准备的**：广播传输失败后的重播会重签，gasPrice 变了哈希就变了，而真正上链的可能是上一次签的那笔——我们等的哈希永远不会出现。没有期限，单子在 `processing` 里无限转圈，而管理端对 `processing` 刻意无权处置（防双付），死锁。到点后 worker 把单子标成 `failed`，`last_error` 写明「结果不明，退款前先查链」并带上哈希。这不是把不确定变成确定，是把不确定交给唯一有资格处置它的角色。时钟用 `broadcasted_at` 而不是 `created_at`（单子可能建成几天后 worker 才开）或 `updated_at`（每次续租都碰它，期限永远到不了）；`RecordPayoutTx` 里它包在 `COALESCE` 里只写第一次，且用 `clock_timestamp()` 而非 `NOW()`——后者在真库测试的回滚事务里是常量，「重播刷新了时钟」这类回归会不可见。

**`failed` 不是终态，也不自动退款。** 哪怕是确定 revert 的那种：revert 几乎总意味着结构性配置错误（金库没钱、合约不对），自动退款会抹掉现场并邀请供给者立刻重试一次注定同样失败的申请。运营从 `failed` 出发有两条路（`FromFailed` 只有管理端传真）：核实后标记已打款（链上其实成了 / 人工补打），或拒绝退款——退款仍走原有的两道闸，「会加钱的代码」全系统只有 `refundSupplierWithdrawal` 一处，worker 的队列接口上根本没有那个方法。供给者的撤回**不能**碰 `failed` 单：尤其「结果不明」那种，退款之前必须有人先去链上看一眼。通知也照这个分工：`failed` 只发运营（带 last_error + tx_hash + 「核实前退款可能双付」），供给者收到的下一封信永远是终态那封。

**与管理端的互斥有三层，各挡一个时刻**：捞单的 `FOR UPDATE SKIP LOCKED` 挡「正在被人工处理的行」（跳过，不阻塞）；租约挡「捞走之后、钉 nonce 之前」（`Resolve` 对租约未到期的单子报 `PROCESSING`，与「已处理完」是两句不同的话）；`processing` 状态本身挡「钉了 nonce 之后」——那一刻起可能已有交易在内存池里，人工的一次拒绝退款就是双付的另一半。管理端界面上 `processing` 刻意没有按钮，`failed` 有两个（标记已打款 / 拒绝退款），并把 `last_error` 与 `tx_hash` 摆在行里——运营核实之前不该点任何按钮，而不摆出来他就得去翻库。

**顺序处理，不并发。** 同一个金库地址的交易靠 nonce 排队，并发广播要么自己管一段 nonce 区间（错一个全堵住）要么互相撞。选主也是正确性的一部分：两个实例处理不同单子会各自向节点要 nonce、拿到同一个号。吞吐的正解是批量合约（M5），不是并发。捞单按 id 升序：先来的先打，供给者之间不插队。

**其余决定**：捞单只捞 `network IS NOT NULL`（真库钉过：人工单落 NULL 不落空串，这里是那条断言的收益兑现处）；合约地址用单子上的快照、不再问客户端（建单后换过币也按建单那一刻的约定发）；打的是**净额** `amount - fee_amount`（发毛额 = 把手续费白送，且每笔都送、没有任何报错）；`DisabledChainClient` 部署照常启动 worker，单子在 `NextNonce` 上收到明确的「没配置」、带 15 分钟长退避留在 `pending`——运维修好配置这条路就通，或管理端随时人工处理，而不是消失在一个没启动的任务里；未决单计数从 `pending` 扩成 `pending/processing/failed`（卡在链上流程里的钱一样挂在单子上，不占名额等于放开闸门）；`external_ref` 在 paid 时写成 tx_hash（给人对账与给程序对账同源不同列）。

**变异矩阵：worker 14 + 仓储状态机 15，29/29 全红。** worker 那 14 个打在单元测试上（发毛额、不复用 nonce、Begin 失败照样广播、广播/确认的"不明"判成 failed、放弃期限永远不到、已有哈希重播、revert 不进 failed、Disabled 用短退避、快照残缺放行、净额为零放行、哈希没落库照样确认、预算耗尽不收手、paid 不带哈希）；仓储那 15 个打在真库上（捞单的四条谓词各一、Begin 的两条件、放弃时钟被重播刷新、终局不判状态、failed 顺手写 resolved_at、未决计数缩回 pending、租约退避、last_error 的三种清空错法、供给者可撤 failed 单）。

三条跑矩阵时踩的坑，都属于「变异本身写坏了」而不是测试弱：把 `$6`/`$7` 从 SQL 里删掉的变异红得很热闹，但红因是 `pq: could not determine data type`——**参数数量/类型错配是 ANCHOR-BAD 的近亲，必须换成保留参数引用的写法重跑**（`$7 AND FALSE` / `$7 OR TRUE`）；`$6 IS NOT NULL` 还不够，PG 推不出裸参数的类型，要 `$6::boolean IS NOT NULL`。换完之后三个变异各自只红在为它准备的那条测试上，才算真的被咬住。另有两处**刻意不计入**的等价变异：Resolve 的租约条件与 failed 门槛在 Go（先锁行再判）与 SQL（条件更新）各有一层，单独打掉任何一层，另一层会以同一个错误兜住——这是防「有人绕过应用层改库」的纵深，不是测试的漏洞；把两层**同时**打掉的组合变异（R9）单红在「供给者不能撤 failed 单」那条真库测试上。

### 9.8 批量发放（M5，2026-08-24）

M4 的 worker 逐单打款，每张一笔 gas；M5 把同一轮里**同链同币**的单子合成一笔 `disperseToken`（合约与客户端能力 M2 就绪，见 §9.5）。改动全部落在 worker 内部——仓储、状态机、迁移一个都没动：批量与单笔共用同一台五步状态机，差别只在「一个 nonce / 一个哈希背后站着几张单子」。

**RunOnce 从「逐单」改成「规划 → 确认 → 广播」。** `plan` 先把捞到的单子分三摊：坏快照当场转运营；带哈希的按 `(network, hash)` 并成**确认组**——一次批量广播的产物共享一个哈希，一次查询定一整组的生死（批量在链上 all-or-nothing，不存在半成）；待广播的按 `(network, token)` 并成**广播组**，恢复期（`chain_nonce` 已钉）再按 nonce 细分。确认排在广播之前：等答案的单子最老、供给者在数分钟，而广播是新增负债，晚半轮没人察觉。分组顺序 = 捞单顺序（id 升序），两轮之间组的组成因此是确定的。

**批量路径上唯一一条看不见的硬约束：额度检查在要号之前。** `EnsureBatchAllowance` 里的 approve 自己要占金库地址上的一个 nonce，顺序反了它会把我们刚要到的号吃掉，批量交易重发时撞 nonce too low。这条顺序有专门的探针测试（记录 `allowance`/`nonce` 调用序）和对应的变异（S3 跳过额度检查）钉着。

**恢复的全部规则可以压成一句：组成允许缩水，换号绝对禁止。** 上一轮批量把同一个号钉给了整组，崩溃后这一组凭「共享的 nonce」重新聚成同一批、用那个号重播——同号安全（链上最多认一笔），所以中途有单子被人工处理掉、批次缩水，无妨；但钉了**不同** nonce 的恢复单绝不能合并（批量只有一个 nonce 字段，合并等于给其中一张换号，那正是双付的形状），为此恢复期分组键里带着 nonce 本身。恢复期还有两条特判：**跳过额度检查**——如果上一轮的批次其实已上链，额度已扣，补一笔 approve 白占一个新号；如果没上链，额度原封没动；额度真不够，批量会在链上明确 revert，那是一个能走 failed → 运营的干净结局。**批量合约中途被关掉**时，共用一个号的恢复组拆不成单笔（每笔要自己的号），只能带 15 分钟长退避原地等配置修回来，`last_error` 把话说明白——这是运维动作，不是抖动。

**降级与掉队**：没配批量合约的部署，多张同币单安静退回逐笔（批量是省 gas 的优化，不是功能前提）；组里某张在钉号时发现已被别人处理，无声退出，其余照播；某张的哈希落库失败，它退出确认组带着钉住的 nonce 回队（下一轮凭 nonce 归队、重播被节点认成同一笔、哈希补上），其余照常进终局——「一张没有 tx_hash 的 paid 单在对账里是个洞」这条规矩在批量里逐张适用。

**规模**：客户端的批量上限是 100 个收款人（超过会撞区块 gas 上限、整批 revert，`batchTotals` 在本地就挡），worker 单轮捞单上限 20，永远到不了那个数——所以 worker 侧不做切块，这一点写在这里免得将来把捞单上限调过 100 时想不起来。

**变异矩阵：26/26 全红**（单笔路径 14 个按新结构重锚 + 批量路径 12 个），批量那 12 个是：合批条件写反、混币合批、跳过额度检查、恢复期重走额度+要号、恢复期分组丢 nonce、批量发毛额、掉队的还留在批里、批量广播失败写终局、revert 只 fail 第一张、确认组不合并、哈希没落库照样进终局、恢复期无合约时拆成单笔。其中「恢复期分组丢 nonce」需要一条专门的测试（两张钉了不同号的单子必须各走各的）才能咬住——同号恢复的测试对它是等价的，因为合并后的组恰好还是那个号。

### 9.9 BSC 测试网真链验证实录（2026-08-24）

§9.5 的金标向量只能证明「编码等价于 go-ethereum」，证明不了「节点收下并打包了我们签的交易」。本轮用一套测试网配置（chain 97，publicnode RPC）把 M2–M5 的客户端能力对着真链跑了一遍，harness 落在 `internal/payoutchain/livetest_test.go`（独立的 `livetest` 标签，不进任何常规跑道；配置全走环境变量，与生产同一套读法；**拒绝在 chain 56 上跑**）。所有转账打给金库自己——self-transfer 的事件、余额变动、确认流程与打给别人一模一样，代币一分不少，只有 gas 是真花的。

**结果：全绿。** 金库 `0xc11143e0…`，单笔 transfer 与经 disperse 合约（`0x44fa3c2e…`）的批量各广播一笔、各自确认（tx `0xf42506…` / `0xd57dff…`），额度早前已 approve 无需补，事后代币余额一分不变，整轮 gas 0.000007 BNB。EstimateFee 走了 fallback（`estimated=false`）——`PAYOUT_BSC_NATIVE_USD` 没配就是这个预期行为。

**真链第一枪就抓出两个只有真环境能暴露的缺口，都已当场修掉：**

1. **6 位精度的 USDT 整个进不来。** 测试网这个 USDT 合约是 6 位小数（与以太坊主网 USDT 相同），而 `ToTokenAmount` 对 decimals < 8 直接拒绝——M2 时留的明账（"真要支持得先想清楚零头怎么记账"）。现在把账想清楚了：**净额向下取整到代币精度**。方向与手续费侧同一条原则（宁可金库留零头，绝不多发）；零头 < 10^-decimals、不属于任何一张单子、不进任何流水，对账口径就是「链上实发 = 净额向下取整到代币精度」。拒绝方案在真实世界不可用：手续费是 8 位估算值，净额几乎必然带第 7、8 位的尾巴，每一笔都会被拒。取整后归零仍然拒绝——那不是零头，是整笔钱小于代币最小单位，放行就是"安静地转账 0"。
2. **客户端一直无视 `params.Token`。** worker 忠实地把单子上的合约快照传进来，客户端却恒用自己配置的合约发——「按建单那一刻的约定发」这个承诺只在建单时刻由 TokenAddress 判据保证，配置在建单**之后**换币（USDT→USDC）的话，会给收款人发另一种币，而链上不会报任何错。补了 `checkToken`：快照地址（或符号）与金库配置不一致就拒绝广播，单子带着明确的 last_error 走 failed → 运营。空串放行——那是"调用方没有要钉的东西"。

两处都补了测试（floor 的方向、归零拒绝、零精度拒绝；错地址/错符号拒绝、一致与空串放行），5 个针对性变异全红。环境变量名的对照也值得记一笔：正确的是 `PAYOUT_BSC_SIGNER_KEY` / `PAYOUT_BSC_TOKEN_ADDRESS` / `PAYOUT_BSC_DISPERSE_ADDRESS`（前缀 + 网络名 + 项），凭记忆写成 `PAYOUT_SIGNER_KEY` 这类形态的话，Enabled 校验会在启动时报缺项——不会静默。

**收尾巡检补掉的最后一个漏项（2026-08-24）：对账导出没带链上列。** 提现导出的 CSV 还是 M3 之前的形状——`amount` 按毛额走是刻意的（§3.7），但财务对链上流水需要 `fee_amount`（算实发）、`net_amount`、`network`/`token_symbol`（把文件劈成两半：人工的对银行流水，链上的对区块浏览器）与 `tx_hash`（对链的键，与 `external_ref` 刻意冗余——后者将来可能装别的渠道的凭证）。五列一律**追加在行尾**，不动老列序号：这份文件的下游可能按列序号读，往中间插列等于把它们的每个值都挪一格。`net_amount` 由数据库算（`amount - fee_amount`），与服务层同数由 §9.6 那条真库测试钉着；真库新增一条用例逐列断言链上单与人工单的形态（人工单的 fee 是 NUMERIC 的零不是空串——它是数字列）。顺带把前端类型注释里的旧状态集合补上 processing/failed。

### 9.10 金库配置进控制台 + 提现链上-only（M6，2026-08-25）

两个决定都是用户拍的板：**私钥加密进控制台**（第七个 settings key `supply_payout_chain_settings`），**人工渠道整个下掉**（提现只剩链上一条路）。

**私钥的三条纪律，各有测试与变异钉着：** 形状不对不加密（64 位十六进制先验后封，错误消息里不带输入内容——它可能就是一把差一位的真私钥）；明文不落库（AES-256-GCM + `enc.v1:` 前缀，与收款账号同一把钥匙同一套保护，settings 的 validate 把任何非密文形态的非空私钥拒在库外）；读路径不吐钥匙（`GetSupplyPayoutChainSettings` 恒抹 SignerKey——**连密文都不出**，密文走专用通道 `SupplyPayoutChainSignerCiphertext` 只给持有解密器的 Manager；管理端回显的只有 `signer_configured` 和从私钥推导出的金库地址；更新时留空 = 沿用旧钥匙）。§9.5「私钥不沾 mapstructure」的老决定没有被推翻，是被搬家了：不沾配置文件这条路的理由（会被写回、会进备份）在加密落库 + 只写不读的形态下不再成立。

**热更换（payoutchain.Manager + Holder）。** 消费者（提现服务、worker）持有的是一个原子转发器，保存配置即重建客户端并换进转发器，无需重启。热换的安全性不是新论证的——M4/M5 的三道护栏（nonce 钉死、checkToken、Disabled 明确拒绝）本来就是为「配置在单子中途变了」准备的，热换最坏的结果是几张单子多退避一轮。Reload 有两个**方向相反**的失败分支，各防一种事故：配置读不出来（库抖动、钥匙解不开）→ **不换**，一次抖动不该把正在打款的 LIVE 降级成 Disabled；配置读出来了但造不出客户端（钥匙内容坏了）→ **换成 Disabled**，继续用旧客户端等于按一份已被替掉的配置打款。配置来源的优先级：settings 存过 → console 是唯一事实（env 全部失效）；没存过 → 回落 env（存量部署迁移期）；`PAYOUT_ENABLED+PAYOUT_MOCK` 双开关的 mock 压过一切且**刻意不进控制台**——一个能在生产界面上点出来的「假装打款」开关早晚会被点出来。启动自检并进 Manager 的首次 Reload，main.go 里那份 env 自检退役（两处各核一次链、各打一条日志，迟早有一条骗人）。

**提现链上-only（反转 M3 的「留白退人工」）。** 渠道列表不再来自 settings.Channels 白名单，改由「金库此刻能结算什么」派生（`settleableChannels`，与建单、报价同一个 `settleOnChain` 判据）；建单时结算不了从「退人工工单」改成**拒绝**——那条人工路不存在之后，一张四列留白的单子既不会被 worker 捞到、也没有人工队列接住，只会安静地躺着，而钱已在建单那一刻扣走。三种结算不了的形态（没接客户端 / Disabled / 金库是别的币）统一报 NOT_CONFIGURED（指向平台配置）；渠道校验仍在取绑定地址之前；绑定那道门原样（金库配好了不放松地址来源）。管理端提现卡的渠道编辑换成一句「渠道由链上金库派生」的说明（编辑一份不再被读的名单只会造出面板上的错觉），状态横幅里的「没配渠道」分支同步摘除——判据只能有一个来源。用户表单摘掉整个人工分支：自由输入收款账号的输入框、提交时的人工路径、fee 的 manualFallback 文案全部移除——「手填地址被当成链上地址送出去」这件事在前端不再有路径。

**存量语义**：老的人工单照常被 MarkPaid/Reject 处理（Resolve 不看渠道）；`SupplyWithdrawalSettings.Channels` 字段保留（旧 JSON 还在库里）但不再被读。前端 API 面新增写方法要过哨兵测试的名单，`updatePayoutChainSettings` 是一次明确决定后列入的。

**变异**：六个针对性闸 6/6 红——其中两个第一轮 GREEN 都是真漏洞：mock 单开关生效（缺「只设 MOCK」的用例）、真 SettingService 的抹钥匙没被测到（桩自己做了抹除，真实现删掉那一行测不出来——**桩复刻了被测行为，就把被测行为遮住了**，这一课与 §9.5 的等价变异同级）。Manager 8 条、settings 12 条、提现改造后的服务层全量（含 M1/M3 测试按新语义重写：三个「退人工」变「拒绝」、人工渠道用例变「渠道无效」、白名单用例变「忽略白名单」）、前端 1638 条全绿。

**§9.10 补记（M6c，2026-08-25）：通知与表单按链上-only 的现实再瘦一圈。** 「新申请→运营」的邮件停发：那是人工打款时代的命脉（钱已扣、没人被叫来就永远没人处理），自动结算下每单一封只会训练财务过滤这个发件人；`notify_emails` 保留但换了语义——唯一消费者是 `NotifyPayoutFailed`（failed 单的钱扣着等人工裁决，没这封信它就卡在一张没人知道的单子上），设置卡改名「打款异常告警收件人」，琥珀警告的文案随之换掉。供给者的扣款回执与终态通知不动（回执是尽力而为的凭证，不是建单前置条件——供给者没绑邮箱时一封都不发、也不报错）。`notice`（给供给者的说明）保留为运营的公告位：不发版就能在表单上说话的唯一位置。用户表单在只有一个渠道时不再画下拉（自动选中 + 一行「{channel} · 自动打款到你绑定的地址」），多渠道时下拉回来；人工渠道时代的死 i18n 键（收款账号三件套、manualFallback、withdrawalAccountRequired、管理端渠道编辑三件套与 noChannelNotice）一并清掉。buildSupplierWithdrawalAdminEmail 随停发删除——留一封永远不会发出的信只会误导下一个读代码的人。

**§9.10 补记二（2026-08-25）：撤编辑入口时要把校验一起送走。** M6b 撤掉渠道编辑后第一次真实保存就撞了回归：`validate` 里「开着必须给渠道」还活着，而管理端的保存请求恒带空渠道列表——字段落进「只能是空、空又不合法」的姿势，整张设置卡锁死。修法：validate 摘掉渠道要求（「开着但金库没配好」由金库卡的 status 说话），`Available()` 收窄成只答开关（「真能提吗」由 GetOptions 的 settleableChannels 回答），并用一条测试钉死这次的回归形态。

**§9.10 补记三（2026-08-25）：加密钥匙首启自动生成并落盘，零手动配置。** `totp.encryption_key` 没配时不再每次启动随机一把（那正是补记二之后撞上的第二个坑：私钥密文在重启后集体变孤儿，症状 `message authentication failed`），而是**首次启动生成一次、写进数据目录的 `totp_encryption.key`（0600）**，之后每次启动读回同一把——密文的寿命不再取决于进程的寿命，运维一行都不用配。刻意落文件而不是数据库：这把钥匙护着的密文就躺在库里（金库私钥、收款账号、TOTP），钥匙进库等于把锁和钥匙锁进同一个抽屉，一份 pg_dump 同时带走两者。三条命门各有测试钉着：重启读回**同一把**；**坏文件不覆盖**（那可能是一把被截断的真钥匙，覆盖等于把已有密文永久变成孤儿——退临时钥匙 + durable=false，让 Seal 的门挡住新的私钥保存）；目录只读时退回老行为（临时钥匙 + durable=false）。路径优先级跟着配置文件走（CONFIG_FILE 的目录 > DATA_DIR > /app/data > .）：钥匙躺在 config.yaml 旁边，备份数据目录时自然一起带走。
