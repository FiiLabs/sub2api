# 团队工作空间

Sub2API 支持个人工作空间和团队工作空间。

- 用户始终是登录身份。
- 账单主体拥有余额、订阅、API Key、用量和订单。
- 个人工作空间表示一个用户对应一个个人账单主体。
- 团队工作空间表示一个团队对应一个团队账单主体。
- 仪表盘、API Key、用量、支付、订阅、兑换和订单页面都使用当前工作空间。
- 当前工作空间是团队时，侧边栏显示成员管理页面。

## 成员生命周期

- **创建团队**：任意用户可在工作空间切换器创建团队（创建者成为 owner）；平台管理员可在管理后台为任意用户创建团队。
- **邀请成员**：owner / admin 按邮箱邀请。被邀请人会收到带接受链接的邮件，邀请人也会得到一个可复制的接受链接。邀请只是一条待接受记录，**不会创建账号、也不设置密码**。
- **接受邀请**：被邀请人打开链接并登录（未注册者先走正常注册流程，自行设置密码并完成邮箱验证）。接受时**绑定被邀请邮箱**——当前登录账号的邮箱必须与邀请邮箱一致——随后创建有效成员关系。
- **管理成员**：owner / admin 可修改成员角色、暂停/恢复、移除成员（owner 不能在此被修改或移除）。
- **移交所有权**：owner 可将所有权移交给另一名有效成员，原 owner 降为 `admin`。在移交之前，owner 不能离开或被移除。

团队角色：

| 角色 | 查看用量 | 管理 Key | 管理成员 | 管理账单 | 团队设置 | 解散团队 |
|---|---|---|---|---|---|---|
| owner | 是 | 是 | 是 | 是 | 是 | 是 |
| admin | 是 | 是 | 是 | 是 | 是 | 否 |
| billing | 是 | 否 | 否 | 是 | 否 | 否 |
| developer | 是 | 是 | 否 | 否 | 否 | 否 |
| viewer | 是 | 否 | 否 | 否 | 否 | 否 |

> 上表是**设计意图**。下节是**代码实际强制现状**与需决策的点，供按角色行为做最终裁决。

## 角色行为强制现状与待决策（RBAC 落地扫描）

> 扫描时间 2026-06-24（初版）。**更新 2026-06-24：CPO v1.0 六议题已逐条裁定并落地——原 ⚠️（未强制）项均已强制，详见下表与「CPO 裁决与落地状态」。** ✅=已按设计强制。基于 `team-workspaces` 分支。

### 实际强制矩阵

| 操作 | 是否强制 + 方式 | owner | admin | billing | developer | viewer |
|---|---|:-:|:-:|:-:|:-:|:-:|
| 邀请/修改/移除成员 | ✅ 服务层 `Require(team.members.manage)`，按 URL 的 teamID 查库校验 | ✅ | ✅ | ✗ | ✗ | ✗ |
| 移交所有权 | ✅ owner-only（校验 `team.owner_user_id`） | ✅ | ✗ | ✗ | ✗ | ✗ |
| API Key 建/列/查/改/删 | ✅ 服务层强制 `team.keys.manage`（5 个操作统一过） | ✅ | ✅ | ✗ | ✅ | ✗ |
| 用量 / Dashboard 读取 | 隐式按主体过滤（未显式校验，但全角色本就有 `team.usage.view`，无越权） | ✅ | ✅ | ✅ | ✅ | ✅ |
| **兑换码充值到团队余额** | ✅ 服务前置 `RequireTeamBillingManage`（团队需 `team.billing.manage`）+ 入账写 `redeem_audit_logs` 审计流水 | ✅ | ✅ | ✅ | ✗ | ✗ |
| 团队充值（在线支付） | ✅ 已放开；`/payment` 组中间件按 `team.billing.manage` 强制（与兑换共用同一导出闸 `RequireTeamBillingManage`） | ✅ | ✅ | ✅ | ✗ | ✗ |
| 团队订阅查看 | ✅ 4 接口按 `subject.BillingSubjectID` 路由（gating `QuotaSubjectScoped`）；查看 = 全角色 | ✅ | ✅ | ✅ | ✅ | ✅ |
| 修改团队设置（改名） | ✅ `Require(team.settings.manage)`，`PATCH /teams/:id` | ✅ | ✅ | ✗ | ✗ | ✗ |
| 解散团队 | ✅ owner-only `Require(team.dissolve)`，`DELETE /teams/:id`（余额>0 或有启用中 Key 则拒，安全最小） | ✅ | ✗ | ✗ | ✗ | ✗ |

**防越权（已验证健全）**：成员类操作用 `Require(actorUserID, URL里的teamID, 权限)` 按库里该 actor 在该 team 的**实际角色**校验，不信任前端传的"当前工作空间"头，因此「对 A 团队是 owner、却拿去操作 B 团队」会被拒。

### CPO 裁决与落地状态（v1.0，2026-06-24）

> 裁决依据 [Gateway-Workspace-Function-Definition.md](./Gateway-Workspace-Function-Definition.md)。6 议题已逐条裁定并落地（工作项 `read-side-subject` / `workspace-rbac-fixups`）。

1. **developer 可全权管理团队所有 Key（含他人所建）** —— 裁决：**维持共享模型**（团队 Key 属团队主体，凡有 `team.keys.manage` 者皆可建/列/改/删）。无需改动；需收紧的客户用「暂停成员」或降级 viewer 控制。
2. **兑换码任意成员可充值、无归属审计** —— 裁决：**收紧 + 补审计**。已落地：兑换前置 `team.billing.manage` 校验（commit `88b91021`）+ `redeem_audit_logs` 审计流水（who/when/code/amount/subject，事务内，commit `085840e7`+`dde92b2c`）。
3. **团队在线支付禁用** —— 裁决：**开放，限 `team.billing.manage` 发起**。已落地（commit `2ab7e34d`，本轮并入，覆盖原「排入 Phase 2」时序）；后端订单创建/列表/履约本就按团队主体归属。
4. **团队订阅读到个人订阅（归属错误）** —— 裁决：**纳入团队主体**（查看=全角色 `usage.view`，购买/变更=`team.billing.manage`）。已落地：4 个 GET 接口按 `subject.BillingSubjectID` 路由 + 迁移兜底回填（commit `892f13eb`）；订阅变更走兑换码，其 billing 闸见议题 2。
5. **解散/团队设置无用户端入口** —— 裁决：**开放**（设置=owner+admin，解散=owner only）。已落地：权限 key 对齐 `team.dissolve`/`team.ownership.transfer`（commit `38e25c4a`）；团队设置改名 `PATCH /teams/:id`（commit `c980748b`）；解散 `DELETE /teams/:id` owner-only 安全最小（余额>0 或有启用中 Key 则拒，单事务软删 成员→团队→计费主体，commit `db979bbb`）。
6. **用量/Dashboard 全角色可见** —— 裁决：**维持全角色可见**。无需改动。

**贯穿落地**：所有 `team.billing.manage` 入口（兑换 / 在线支付 / 订阅购买）共用同一导出闸 `handler.RequireTeamBillingManage`，避免逐接口口径漂移。读侧收尾（T0.4）另已修复：个人 `/keys` 不再串入团队 Key、团队 `platform-quotas` 不再恒空（按账单主体读，gating `QuotaSubjectScoped`）。

## 已知限制

- **按平台的 USD 限额已改为按团队计费主体生效（灰度中）。** 日/周/月平台限额（`user_platform_quotas`）
  现可按 `billing_subject_id` 拦截与回写：团队成员消耗共享团队主体限额，个人工作空间行为不变。
  由开关 `billing.quota_subject_scoped` 控制（默认开）；设为 false 即整体回退到历史 `user_id` 维度（kill-switch）。
  实现见 [impl/platform-quota-subject.md](./impl/platform-quota-subject.md)（`user_id` 已改可空、新增
  `(billing_subject_id, platform)` 部分唯一索引、拦截/回写/注册/管理端全切、历史已回填）。
  仍待：staging 集成验证后删除开关与旧 user 路径收尾（见该方案 §8）。
