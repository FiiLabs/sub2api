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

另有一处设计缺口需在实现时补：**`service.UsageBillingCommand`（`internal/service/usage_billing.go:19-45`）没有 account_cost / 成本口径字段**。结算 accrue 的基数（`total_cost × account_rate_multiplier`）拿不到，需要给命令结构新增字段并在其构造处填值——这是交接件未预见的一处额外 core touch。

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

## 4. Core Touch 台账

core 侵入处一律：逻辑放新文件，core 处只留单行调用 + `// APEXONE-EXT:` 可 grep 标记 + 详尽注释 + 单测。下表随实现逐行填。

| # | 文件 | 位置 | 改动 | 理由 | 状态 |
|---|---|---|---|---|---|
| 1 | `backend/ent/schema/account.go` | 字段区 | 加 `owner_user_id` (Optional/Nillable) | 供给账号归属。NULL = 管理员自建，调度/计费/删除均不读该字段，向后兼容 | 待做 |
| 2 | `backend/internal/repository/usage_billing_repo.go` | `applyUsageBillingEffects` :174 | 加「扣赚取钱包优先」+「供给者 accrue」两个分支 | 决策人裁定结算 accrue 内联绑计费事务（结算正确性 > 合并便利），同事务同 `RequestID` 幂等保证「消耗 === 入账」 | 待做 |
| 3 | `backend/internal/service/usage_billing.go` | `UsageBillingCommand` :19 | 新增成本口径字段供 accrue 取基数 | 现结构无 account_cost，accrue 拿不到分成基数（交接件未预见） | 待做 |
| 4 | `backend/internal/server/router.go` | :117-132 调用块 | 加 `routes.RegisterSupplierRoutes(...)` 一行 | 供给侧用户路由入口 | 待做 |
| 5 | `backend/internal/handler/handler.go` + `handler/wire.go` + `service/wire.go` + `repository/wire.go` | 各 ProviderSet | 各加一个 provider 注册 | wire 标准注册点，生成式非手维护 | 待做 |
| 6 | `frontend/src/router/index.ts`、`i18n/{en,zh}/index.ts`、`AppSidebar.vue` | 桶文件末尾 | 追加供给侧路由/文案/入口 | 前端冲突热区，只在末尾追加 | 待做 |

## 5. 结算不变量（实现必须守住）

1. **消耗 === 入账**：failover 后只有产出被计费响应的账号入账（以 `usage_log.account_id` 为准）；失败/重试/无计费产出零入账。accrue 幂等键 = `usage_log.RequestID`。
2. **冻结窗 ≥ 拒付窗**：使「已释放 = 拒付安全」。冻结期内拒付 → 从供给者冻结 earnings 追回；释放后拒付平台自吃。
3. **同一请求只扣一处**：赚取钱包或 `users.balance`，由 `applyUsageBillingEffects` 单点保证。
4. **审计快照**：ledger 每条带快照，供给者可自行核对计量。

## 6. 待定参数（fog，实现期取默认值、运营期调）

| 参数 | 拟定默认 | 说明 |
|---|---|---|
| `supplier_revenue_share_ratio` | 0.70 | 供给者分成，基数 = 消费者实付（非官方价） |
| 供给池 `rate_multiplier` | 0.5 | 消费者价 = 0.5× 官方价 |
| `supplier_earning_freeze_hours` | `168`（7 天，**占位**） | 须 ≥ 支付通道拒付窗。7 天只是能跑通流程的占位值，**上线前必须按 Stripe/支付方实际拒付窗重设**（Stripe 争议窗远长于 7 天），否则不变量 2「已释放 = 拒付安全」不成立 |
| 自营池 `rate_multiplier` | `1.0`（**占位**） | 需覆盖真实 API 成本 + 毛利，上线前重设 |
| 观察期强度/时长 | **待定** | 先做成参数，运营期标定 |
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
