# 生产部署手册:sub2api(控制面)+ private-ai-gateway(TEE 网关)

本手册讲如何把"team 走 TEE"这套部署到**生产**。和本地测试(`local-testing-runbook.md`)最大的区别:

| | 本地 | 生产 |
|---|---|---|
| TEE | dstack **simulator**(假 attestation) | **真 dstack CVM**(Phala Cloud 或自建 dstack + KMS) |
| 网关部署 | 直接跑二进制 | git-launcher + 钉死 commit + attestation |
| 密钥 | 明文文件 | dstack 加密 secrets / KMS,仅 enclave 内解密 |
| sub2api↔网关 | 都在本机 localhost | 跨网络:**TLS + control token**,且只对网关放行 |
| 安全保证 | 无(仅功能) | 真实:relying party 可验证 attestation + receipt |

> ⚠️ **现状提醒(务必先读)**:private-ai-gateway 是 `0.1.0` 开发者预览。其 `deploy/` 自带的 compose 是 **no-middleware 模式**(只跑网关)。**"网关 + executor 一起在同一 TEE CVM 里"的 compose 接线仓库未提供**,需要你自建(本手册第 5 节给方案与草案)。durable 透明日志、生产存储等也仍是 open items。

---

## 1. 生产拓扑

```
                    公网(TLS)
用户/Claude Code ───────────────► [TEE CVM]  private-ai-gateway(frontend+backend) + executor
                                      │  ▲           │
                                      │  │ consult(仅元数据, HTTPS+token)
                                      │  │           ▼
                                      │  └────► [非 TEE 服务器] sub2api(:控制面/api/control) + Postgres + Redis
                                      ▼
                              上游大模型(Anthropic / 机密 provider ...)
```

- **非 TEE 侧**:sub2api + PostgreSQL + Redis。负责 API key、team、路由决策(route_map)、计费。**只接触元数据,不接触 prompt。** 也对外提供 personal 用户的普通代理(若你同时跑 personal)。
- **TEE 侧(dstack CVM)**:private-ai-gateway + executor。**所有 prompt/响应、上游凭证只在这里。** executor 通过 HTTPS 调非 TEE 的 sub2api `/api/control`(只发 key 哈希、model、token 计数)。

> 数据面(prompt)永不离开 TEE 与上游;控制面(元数据)才走到 sub2api。这正是"team 只走 TEE"的安全前提。

---

## 2. 前置选择

### 2.1 TEE 平台(网关侧)
- **Phala Cloud(推荐,托管 dstack)**:无需自建 TEE 硬件,用 `phala` CLI 部署,CVM 自带 `/var/run/dstack.sock`。
- **自建 dstack**:自有 Intel TDX 服务器 + 部署 dstack + KMS。

### 2.2 非 TEE 侧
- 一台普通服务器(或 k8s),跑 sub2api + 托管/自建 PostgreSQL + Redis。
- 反向代理(Nginx/Caddy)做 TLS 终止。

---

## 3. 安全与密钥(生产必须做对)

| 密钥/项 | 放哪 | 说明 |
|---|---|---|
| **control token** | 两端一致:sub2api 配置 + 网关 executor 的 env | 用 `openssl rand -hex 32` 生成;网关侧经 **dstack 加密 secrets** 注入,不要明文进 compose 仓库 |
| **上游 API key**(Anthropic 等) | **网关 upstreams.json**(TEE 内),经 dstack secrets | 绝不放到 sub2api;sub2api 看不到上游 key |
| **sub2api DB 密码 / admin token** | sub2api 侧 secret 管理 | — |
| **TLS 证书** | 反代;网关可配 `tls.domain_certificates` 把 SPKI 绑进 attested keyset | 让客户端能把 TLS 绑定到被证明的网关身份 |

网络隔离:
- sub2api 的 `/api/control/*` **只允许网关出口 IP 访问**(防火墙/反代 allowlist)+ 强制 TLS + control token 三重。
- 普通用户面(personal `/v1/*`)与控制面 `/api/control/*` 建议分流/分端口,控制面不对公网开放。

---

## 4. 部署 A:sub2api(控制面,非 TEE)

1. **DB/缓存**:部署 PostgreSQL + Redis;确保 DB 角色能 `CREATE EXTENSION pgcrypto`(迁移 157 回填 key_hash 需要),否则预装 pgcrypto。
2. **配置 `config.yaml`**:
   ```yaml
   server:   { host: 0.0.0.0, port: 8080 }
   database: { host: ..., port: 5432, user: ..., password: ..., dbname: sub2api, sslmode: require }
   redis:    { host: ..., port: 6379 }
   consult:
     control_token: "<强随机,与网关一致>"
     placeholder_account_id: <tee-gateway 账号 id;迁移后查 SELECT id FROM accounts WHERE name='tee-gateway'>
     route_map:
       "claude-*": { route_id: "claude:sonnet-4-6", format: "openai" }   # 按你的网关上游对齐
   ```
3. **启动**(自动跑迁移 157/158):`./sub2api`(用进程管理器 systemd/k8s 常驻)。
4. **反代 + TLS**:对外 `https://api.example.com`;把 `/api/control/*` 限制为仅网关可达。
5. **团队与 key**:创建 team;team 成员的 key 在 team 下创建(自动带 `team_id` → 走 TEE)。route_map 的 `route_id` 必须与网关 upstreams 的 `<name>:<model>` 一致。

> 路由真相来源是 `consult.route_map` → 网关 upstreams;`tee-gateway` 账号仅作计费占位,不需要建 group 指向它(详见团队/分组说明)。

---

## 5. 部署 B:private-ai-gateway + executor(TEE CVM)

### 5.1 基线:网关本身(no-middleware,仓库现成)
仓库 `deploy/` 用 git-launcher 一键部署(`deploy/README.md`、`deploy/compose.yaml`):钉死 `REPO_URL`+`COMMIT_SHA`,在 TEE 内 `cargo build` 并运行,从 dstack KMS 取密钥。Phala 上:
```bash
cd deploy
PRIVATE_AI_GATEWAY_REPO_COMMIT=<审计过的完整40位commit> \
PRIVATE_AI_GATEWAY_ADMIN_TOKEN=<长随机> \
phala deploy -n private-ai-gateway -c compose.yaml
```
但这只是网关(no-middleware),**还没有 executor、也没接 sub2api**。

### 5.2 关键缺口:把 executor 也放进同一 CVM(需自建)
仓库未提供带 middleware 的生产 compose。你需要让**同一个 TEE CVM 内**同时:
1. 跑网关,且其静态配置含 `executor` 段(指定两个 UDS);
2. 跑 executor(Node),env 指向这两个 UDS + 外部 sub2api。

**自建方案(二选一):**

- **方案①:扩展 entrypoint(简单)**:在网关镜像/entrypoint 里,构建网关的同时 `cd middleware/executor && npm ci && npm run build`,然后用一个进程管理(如 supervisord / 一个 shell)在容器内**同时拉起网关和 executor**,共享 `/run/pag/*.sock`。网关静态配置加:
  ```json
  "executor": { "uds_path": "/run/pag/executor.sock", "backend_uds_path": "/run/pag/backend.sock" }
  ```
  executor 的 env:
  ```
  PRIVATE_AI_GATEWAY_EXECUTOR_UDS_PATH=/run/pag/executor.sock
  PRIVATE_AI_GATEWAY_BACKEND_UDS_PATH=/run/pag/backend.sock
  PRIVATE_AI_GATEWAY_CONTROL_URL=https://api.example.com/api/control   # 指向非 TEE 的 sub2api
  PRIVATE_AI_GATEWAY_CONTROL_TOKEN=<dstack secret 注入>
  ```
- **方案②:compose 双服务(干净)**:同一 dstack app 的 compose 里放两个 service(gateway、executor),共享一个挂载卷放 UDS;两者都在该 CVM 的 attested 边界内。需要把这套 compose 也纳入 attested `compose_hash` 并钉死镜像 digest。

> 无论哪种:**executor 与网关必须在同一 CVM(同一 attested 边界)**;executor→sub2api 是出 enclave 的**控制面**调用,只带元数据,必须 HTTPS + token。

### 5.3 上游配置(凭证进 TEE)
网关的 `upstreams.json`(经 dstack 加密 secrets 注入,或 admin API 启动后灌):
```json
[
  { "name":"claude","provider":"openai-compatible","base_url":"https://<上游域名>",
    "models":{"sonnet-4-6":"claude-sonnet-4-6"},"bearer_token":"<上游key,dstack secret>" }
]
```
- routeId(`claude:sonnet-4-6`)必须与 sub2api route_map 的 `route_id` 一致。
- 想要**端到端机密**(连模型方也看不到):把 provider 换成机密 provider(`tinfoil`/`phala-direct`/`near-ai`/`chutes`/`aci-dcap`)并按其文档配验证策略;那时 receipt 的 `upstream.verified` 会是 `verified` 且 fail-closed。`openai-compatible`(如 Anthropic 中转)是**非机密路由**(见第 8 节)。

### 5.4 source provenance / 可验证性
- `COMMIT_SHA` 必须是你**审计过**的提交;部署后核对 `GET /v1/attestation/report` 里的 `source_provenance` == 你钉的 REPO_URL/COMMIT_SHA。
- compose(launcher pin、gateway config、upstream seed)都进 attested `compose_hash`,改一处身份就变。

---

## 6. 验证(上线后)

```bash
BASE=https://<网关公网域名>; KEY=<一个 team key>; TOK=<control token>
# 1) 网关身份(真 TDX attestation)
curl -sS "$BASE/v1/attestation/report?nonce=$(openssl rand -hex 8)"
# 2) 控制面连通(从网关所在网络;公网应被 allowlist 挡掉)
curl -sS https://api.example.com/api/control/models -H "Authorization: Bearer $TOK"
# 3) 端到端推理(team key)
curl -sS "$BASE/v1/messages" -H "Authorization: Bearer $KEY" -H 'anthropic-version: 2023-06-01' \
  -H 'content-type: application/json' \
  -d '{"model":"claude-sonnet-4-6","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}'
# 4) 回执 + 离线验证
RID=<x-receipt-id>; curl -sS "$BASE/v1/aci/receipts/$RID" -H "Authorization: Bearer $KEY" > receipt.json
curl -sS "$BASE/v1/attestation/report?nonce=N" > report.json
# 在网关仓库执行:cargo run --example verify_aci_artifacts -- --report report.json --receipt receipt.json --nonce N
# 5) 计费:sub2api 侧 usage_logs +1、team billing_subject 余额按量减少
```

---

## 7. 运维

- **密钥轮换**:control token、上游 key 经 dstack secrets 轮换;轮换 control token 要两端同时更新。
- **可用性**:executor 在 sub2api 不可达时对 `/consult/pre` **fail-closed(503)**——sub2api 控制面需 HA、靠近网关、低延迟。
- **监控**:网关 `GET /v1/metrics`(Prometheus);sub2api 自有指标;关注 consult 延迟/错误率、上游错误率。
- **存储**:网关 receipt 目前**内存 + TTL**(`receipt_ttl_seconds`),attested-session 写 `sessions.jsonl`;无 prompt 落盘。durable 透明日志未实现——如需留存合规证据要自行方案。
- **计费对账**:`/consult/post` 是 fire-and-forget(尽力而为),控制面抖动可能丢计费——建议异步对账兜底。
- **升级**:网关升级 = 改 `COMMIT_SHA` 重新 `phala deploy`(身份会变,需重新公告/验证)。

---

## 8. 安全边界(必须向用户讲清)

| 路由 | sub2api/运营方可见? | 上游模型方可见? | fail-closed | receipt attestation |
|---|---|---|---|---|
| 机密 provider(tinfoil/phala/...) | 否 | **否(在 TEE 内)** | 是 | 有 |
| openai-compatible(Anthropic 中转等) | 否 | **是** | 否 | 无 |

- "数据只过 TEE" = **你的中转方(sub2api/非 TEE 机器)拿不到 prompt/响应/上游凭证**。
- 走普通上游时,**模型方仍看到明文**——要连模型方也挡住,必须用机密 provider。
- 保证的兑现需有人**验证 attestation report + receipt**(第 6 步);客户端不会自动验。

---

## 9. 上线检查清单

- [ ] sub2api 在非 TEE 服务器,`/api/control/*` 仅网关可达 + TLS + control token
- [ ] DB 可 `CREATE EXTENSION pgcrypto`;迁移 157/158 已应用;`placeholder_account_id` 已设
- [ ] team 已建;成员 key 带 `team_id`;route_map 的 route_id 与网关 upstreams 对齐
- [ ] 网关以 git-launcher + **审计过的 COMMIT_SHA** 部署到真 dstack CVM
- [ ] executor 与网关在**同一 CVM**;`CONTROL_URL=https://.../api/control`,token 经 dstack secret
- [ ] 上游凭证经 dstack secrets 注入网关(不进 sub2api)
- [ ] `GET /v1/attestation/report` 的 `source_provenance` == 审计 commit
- [ ] 端到端推理 + 回执离线验证通过;计费按 team billing_subject 扣减
- [ ] 向用户标注:机密 provider = 端到端机密;普通上游 = 仅防中转方
```
