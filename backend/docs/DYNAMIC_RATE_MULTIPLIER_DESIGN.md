# 动态倍率方案设计

## 背景

系统里已经存在两类倍率：

- `groups.rate_multiplier`：对外分组费率倍率，用于计算用户余额或订阅额度消耗速度。
- `accounts.rate_multiplier`：接入账号计费倍率，用于计算上游账号成本，并快照到 `usage_logs.account_rate_multiplier`。

当前逻辑中，用户扣费使用分组倍率或用户专属分组倍率，账号成本统计使用最终承接账号倍率。也就是说，如果一次请求从 `0.4x` 账号切换到 `0.5x` 账号，用户侧扣费倍率不会变化。这能保证用户价格稳定，但无法让同一个分组在不同成本账号承接时自动保持利润。

## 目标

允许指定分组根据“最终承接请求的账号倍率”动态计算用户侧扣费倍率。

示例：

```text
账号倍率 0.4x + 利润加价 0.1x = 用户扣费 0.5x
账号倍率 0.5x + 利润加价 0.1x = 用户扣费 0.6x
```

该能力必须按分组显式开启，默认关闭，不能影响现有分组的计费行为。

## 非目标

- 不向普通用户暴露内部账号 ID 或账号倍率。
- 第一版不让路由选择依赖动态倍率。
- 不回填或修改历史 usage log。
- 不替代渠道模型定价。动态倍率只是在已解析出的模型价格基础上做倍率缩放。

## 数据模型

在 `groups` 表增加以下字段：

- `dynamic_rate_enabled BOOLEAN NOT NULL DEFAULT FALSE`
- `dynamic_rate_mode VARCHAR(20) NOT NULL DEFAULT 'off'`
- `dynamic_rate_margin DECIMAL(10,4) NOT NULL DEFAULT 0`
- `dynamic_rate_min_multiplier DECIMAL(10,4)`
- `dynamic_rate_max_multiplier DECIMAL(10,4)`

支持的模式：

- `off`：使用现有固定分组倍率或用户专属分组倍率。
- `account_plus_margin`：`账号倍率 + dynamic_rate_margin`。
- `account_markup`：`账号倍率 * (1 + dynamic_rate_margin)`。

上下限在模式计算之后应用：

```text
如果配置了 min_multiplier：effective = max(min_multiplier, effective)
如果配置了 max_multiplier：effective = min(max_multiplier, effective)
```

如果动态倍率已开启但模式非法，为安全起见回退到固定倍率。

## 计费流程

当前流程：

```text
固定倍率 = 用户专属分组倍率 > 分组默认倍率 > 系统默认倍率
费用 = 模型价格 * 固定倍率
usage_logs.rate_multiplier = 固定倍率
usage_logs.account_rate_multiplier = 最终承接账号倍率
```

新增流程：

```text
固定倍率 = 用户专属分组倍率 > 分组默认倍率 > 系统默认倍率
账号倍率 = 最终承接账号的计费倍率
实际用户倍率 = resolveDynamicRate(group, 固定倍率, 账号倍率)
费用 = 模型价格 * 实际用户倍率
usage_logs.rate_multiplier = 实际用户倍率
usage_logs.account_rate_multiplier = 最终承接账号倍率
```

最终承接账号只有在路由和 failover 完成后才能确定。因此动态倍率解析放在 usage 记录阶段，而不是请求认证阶段。

## 余额与额度预检查

多数 handler 会在最终选中账号之前调用 `CheckBillingEligibility(...)`，此时还不知道最终承接账号，也没有本次请求的 token 消耗。因此预检查不能做精确扣费预估，只做“最低请求成本门槛”判断。

动态倍率开启时，预检查倍率按以下规则解析：

```text
预检查倍率 = dynamic_rate_max_multiplier（动态倍率开启且已配置）
预检查倍率 = 固定分组倍率（动态倍率关闭，或未配置 dynamic_rate_max_multiplier）
最低请求成本 = 0.00000001 * 预检查倍率
```

余额模式下，用户余额只要能覆盖最低请求成本就允许进入路由阶段，避免固定分组倍率高于实际动态倍率时出现“用户有余额但无法发起请求”的客诉。

订阅额度和 user × platform quota 预检查同样使用最低请求成本判断剩余额度：

```text
已用额度 + 最低请求成本 > 限额时拒绝
```

最终扣费仍以 usage 记录阶段解析出的真实动态倍率为准。预检查只负责避免明显无余额或额度耗尽的请求进入上游，不承担精确冻结或预扣职责。

## 用户可见性

普通用户 usage log 已经暴露 `rate_multiplier`，该字段应表示本次请求真实使用的用户侧扣费倍率。

管理员 usage log 额外暴露 `account_rate_multiplier`，可用于分析利润：

```text
用户收入 = total_cost * rate_multiplier
账号成本 = total_cost * account_rate_multiplier
毛利 = 用户收入 - 账号成本
```

普通用户接口不得暴露 `account_rate_multiplier`。

## 实现点

后端：

- 增加 group 字段到 Ent schema 和 SQL migration。
- 增加字段到 `service.Group`、admin group input、DTO、auth cache snapshot 和 repository 映射。
- 新增动态倍率解析函数。
- 在两条 usage 记录路径接入解析逻辑：
  - `GatewayService.RecordUsage`
  - `OpenAIGatewayService.RecordUsage`
- 增加动态倍率解析单元测试。

前端：

- 在管理员分组创建和编辑表单增加动态倍率配置：
  - 启用开关
  - 模式选择
  - 加价参数
  - 最低倍率
  - 最高倍率
- 在管理员分组列表中显示动态倍率状态。

## 兼容性

所有现有分组默认保持：

```text
dynamic_rate_enabled = false
dynamic_rate_mode = off
```

因此现有分组计费行为保持不变。
