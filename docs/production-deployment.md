# 生产部署手册:sub2api(控制面)+ private-ai-gateway(TEE 网关)

本手册讲如何把这套**控制面 + TEE 网关**(数据只走 TEE)部署到**生产**。和本地测试(`local-testing-runbook.md`)最大的区别:

| | 本地 | 生产 |
|---|---|---|
| TEE | dstack **simulator**(假 attestation) | **真 dstack CVM**(Phala Cloud 或自建 dstack + KMS) |
| 网关部署 | 直接跑二进制 | git-launcher + 钉死 commit + attestation |
| 密钥 | 明文文件 | dstack 加密 secrets / KMS,仅 enclave 内解密 |
| sub2api↔网关 | 都在本机 localhost | 跨网络:**TLS + control token**,且只对网关放行 |
| 安全保证 | 无(仅功能) | 真实:relying party 可验证 attestation + receipt |

> ⚠️ **现状提醒(务必先读)**:private-ai-gateway 是 `0.1.0` 开发者预览,其自带 `deploy/compose.yaml` 是 **no-middleware 模式**(只跑网关)。**接好 sub2api 控制面的 compose 由本仓库提供**(`deploy/gateway/compose.enclave.yaml`,见 §5.2)。pin 的网关 commit 用**进程内 middleware**(非独立 executor 进程)。整套(网关 enclave A + Meridian enclave B)已在 Phala dev CVM **端到端冒烟通过**(attestation→推理→receipt→计费);durable 透明日志、生产存储等仍是 open items。

---

## 1. 生产拓扑

```
                    公网(TLS)
用户/Claude Code ───────────────► [TEE CVM]  private-ai-gateway(frontend + 进程内 middleware)
                                      │  ▲           │
                                      │  │ consult(仅元数据, HTTPS+token)
                                      │  │           ▼
                                      │  └────► [非 TEE 服务器] sub2api(:控制面/api/control) + Postgres + Redis
                                      ▼
                              上游大模型(Anthropic / 机密 provider ...)
```

- **非 TEE 侧**:sub2api + PostgreSQL + Redis。负责 API key、路由决策(route_map)、计费。**只接触元数据,不接触 prompt。** 也对外提供 sub2api 自身的普通用户代理(个人 key 直接打 sub2api `/v1/*`,不走 TEE)。
- **TEE 侧(dstack CVM)**:private-ai-gateway(含**进程内 middleware**)。**所有 prompt/响应、上游凭证只在这里。** 网关的进程内 middleware 通过 HTTPS 调非 TEE 的 sub2api `/api/control`(只发 key 哈希、model、token 计数)。

> 数据面(prompt)永不离开 TEE 与上游;控制面(元数据)才走到 sub2api。这正是"数据只走 TEE"的安全前提。

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
| **control token** | 两端一致:sub2api 配置 + 网关 `middleware.control_token` | 用 `openssl rand -hex 32` 生成;网关侧经 **dstack sealed env**(`phala -e`/`phala envs`)注入,不明文进 compose 仓库 |
| **上游 API key**(Anthropic 等) | **网关 upstreams.json**(TEE 内),经 dstack secrets | 绝不放到 sub2api;sub2api 看不到上游 key |
| **sub2api DB 密码 / admin token** | sub2api 侧 secret 管理 | — |
| **TLS 证书** | 反代;网关可配 `tls.domain_certificates` 把 SPKI 绑进 attested keyset | 让客户端能把 TLS 绑定到被证明的网关身份 |

网络隔离:
- sub2api 的 `/api/control/*` **只允许网关出口 IP 访问**(防火墙/反代 allowlist)+ 强制 TLS + control token 三重。
- 普通用户面(sub2api 自身 `/v1/*`)与控制面 `/api/control/*` 建议分流/分端口,控制面不对公网开放。

---

## 4. 部署 A:sub2api(控制面,非 TEE)

1. **DB/缓存**:部署 PostgreSQL + Redis;确保 DB 角色能 `CREATE EXTENSION pgcrypto`(迁移 `950` 用 `digest()` 回填 key_hash,依赖 pgcrypto),否则预装 pgcrypto。
2. **配置 `config.yaml`**:
   ```yaml
   server:   { host: 0.0.0.0, port: 8080 }
   database: { host: ..., port: 5432, user: ..., password: ..., dbname: sub2api, sslmode: require }
   redis:    { host: ..., port: 6379 }
   consult:
     control_token: "<强随机,与网关一致>"
     placeholder_account_id: <tee-gateway 账号 id;迁移后查 SELECT id FROM accounts WHERE name='tee-gateway'>
     route_map:
       "claude-*": { route_id: "claude:claude-sonnet-5", format: "openai" }   # 按你的网关上游对齐
   ```
3. **启动**(首启自动跑迁移 `950` 建 key_hash 列并回填、`951` seed `tee-gateway` 占位账号):`./sub2api`(用进程管理器 systemd/k8s 常驻)。
4. **反代 + TLS**:对外 `https://api.example.com`;把 `/api/control/*` 限制为仅网关可达。
5. **API key**:创建普通 API key 即可,任何有效 key 都能经网关(TEE)走 consult。key 创建时自动写入 `key_hash`(SHA256),网关 middleware 用它经 `/consult/pre` 查找并鉴权。route_map 的 `route_id` 必须与网关 upstreams 的 `<name>:<model>` 一致。

> 路由真相来源是 `consult.route_map` → 网关 upstreams;`tee-gateway` 账号仅作计费占位(TEE 用量的 usage_logs 挂它名下),不参与鉴权,也不需要建任何 group 指向它。

---

## 5. 部署 B:private-ai-gateway(含进程内 middleware,TEE CVM)

### 5.1 基线:网关本身(no-middleware,仓库现成)
仓库 `deploy/` 用 git-launcher 一键部署(`deploy/README.md`、`deploy/compose.yaml`):钉死 `REPO_URL`+`COMMIT_SHA`,在 TEE 内 `cargo build` 并运行,从 dstack KMS 取密钥。Phala 上:
```bash
cd deploy
PRIVATE_AI_GATEWAY_REPO_COMMIT=<审计过的完整40位commit> \
PRIVATE_AI_GATEWAY_ADMIN_TOKEN=<长随机> \
phala deploy -n private-ai-gateway -c compose.yaml
```
但这只是网关(no-middleware),**还没开进程内 middleware、也没接 sub2api**;下一节的 compose 才接上。

### 5.2 接上 sub2api:进程内 middleware(用 `deploy/gateway/compose.enclave.yaml`)
本仓库在 **`deploy/gateway/compose.enclave.yaml`** 提供了**接好 sub2api 控制面**的网关部署单元。pin 的网关 commit 把 control-plane middleware **跑在网关进程内**(Rust):静态配置里给一段 `middleware`,网关就直接调 `middleware.control_url` 咨询 sub2api——**没有独立 executor 进程、没有 UDS**(那是更早的 out-of-process 设计)。一个审计过的 `COMMIT_SHA` 覆盖网关 + middleware 全部源码,verifier 只核对一处。上游(Meridian seat)不内联在此,而是独立 CVM + 起后热加载(见 §10)——网关 attested 身份不随 seat 变动。

部署(Phala;敏感值走 dstack sealed env,不进仓库):
```bash
cd deploy/gateway
phala deploy -n private-ai-gateway -c compose.enclave.yaml -e <sealed.env>
# 更新现有 CVM:加 --cvm-id <app_id>
```
`<sealed.env>` 逐行 `KEY=VALUE`,含:
- `PRIVATE_AI_GATEWAY_ADMIN_TOKEN`(admin API,`openssl rand -hex 32`)
- `PRIVATE_AI_GATEWAY_CONTROL_TOKEN`(= sub2api `consult.control_token`)
- `SUB2API_CONTROL_URL=https://<sub2api域名>/api/control`

要点(踩过的坑,均已写进 compose 注释):
- **`COMMIT_SHA` 必须写死进 `gateway-pin`(会被测进 `compose_hash`)**——Phala 的 `${VAR}` 是 **CVM 内运行时**解析,若把 commit 放 env 就不被测量、source-pin 失效。密钥则相反:**必须走 sealed env**,不进 compose_hash。
- 网关静态配置用 `middleware:{ control_url, control_token }` 段开启进程内 middleware;`control_url` 指 `https://<域名>/api/control`。
- **launcher 需含 C 编译器**:stock `dstacktee/git-launcher` 无 `cc`,网关 `cargo build` 会 `linker cc not found`。用 `deploy/gateway/launcher/`(= git-launcher + build-essential)自建镜像并按 digest 钉入 compose。
- launcher 镜像**按 digest** 钉入 `compose_hash`;`gateway-pin` 的 `REPO_URL` 指向你**审计的**仓库。
- 上游种子 `gateway-upstreams` 内联你的 provider/Meridian 上游(见 §5.3 / §10),或启动后 `PUT /v1/admin/upstreams`。

> ✅ 该 compose 已在 dev CVM **端到端冒烟通过**(attestation 核对 commit → opus/sonnet 推理 200 → 签名 receipt 的 `route.selected` 命中 → 计费落 usage_logs 挂 tee-gateway 账号)。
> ⚠️ **数据面强制走网关**:生产应把 sub2api 自身 `/v1/*` 数据面禁用(直接打返回 `410 DATA_PLANE_DISABLED`),迫使所有推理经 TEE 网关——否则存在绕过 TEE 的明文路径。客户端 base URL 指网关端点,不是 sub2api 域名。

### 5.3 上游配置(凭证进 TEE)
网关的 `upstreams.json`(经 dstack 加密 secrets 注入,或 admin API 启动后灌):
```json
[
  { "name":"claude","provider":"openai-compatible","base_url":"https://<上游域名>",
    "models":{"claude-sonnet-5":"claude-sonnet-5"},"bearer_token":"<上游key,dstack secret>" }
]
```
- routeId(`claude:claude-sonnet-5`)必须与 sub2api route_map 的 `route_id` 一致。
- 想要**端到端机密**(连模型方也看不到):把 provider 换成机密 provider(`tinfoil`/`phala-direct`/`near-ai`/`chutes`/`aci-dcap`)并按其文档配验证策略;那时 receipt 的 `upstream.verified` 会是 `verified` 且 fail-closed。`openai-compatible`(如 Anthropic 中转)是**非机密路由**(见第 8 节)。

### 5.4 source provenance / 可验证性
- `COMMIT_SHA` 必须是你**审计过**的提交;部署后核对 `GET /v1/attestation/report` 里的 `source_provenance` == 你钉的 REPO_URL/COMMIT_SHA。
- compose(launcher pin、gateway config、upstream seed)都进 attested `compose_hash`,改一处身份就变。

### 5.5 用户接入地址(自定义域名,TLS 在 CVM 内终止)
用户拿到 key 后填进 Claude Code 的 `ANTHROPIC_BASE_URL` **必须指向 TEE 网关,不是 sub2api 域名**——sub2api 数据面已禁用(§5.2),打它返回 `410 DATA_PLANE_DISABLED`。**切勿在 sub2api 上反代 `/v1/*` 到网关**:那会让非 TEE 服务器终止 TLS、看到明文 prompt,毁掉 TEE 保证。正确做法是给网关一个干净子域名,直达 TEE:

1. **sub2api 侧**:Admin→设置→"API 端点地址"(`api_base_url`)填 `https://api.<域名>`(用户"使用密钥"里就显示这个)。注意该字段也被微信 OAuth 回调复用,若启用微信登录需拆独立设置项。
2. **网关侧**:`compose.enclave.yaml` 内已含 `dstack-ingress` 服务(见 https://docs.phala.com/phala-cloud/networking/setup-custom-domain):用 Cloudflare "Edit zone DNS" token **自动建 DNS + Certbot DNS-01 签 Let's Encrypt 证书 + 设 CAA**,443 **在 CVM 内终止 TLS** 后转发到 `private-ai-gateway:8086`——明文不出 enclave。`DOMAIN`/`GATEWAY_DOMAIN`(=`_.<该CVM的dstack网关域>`,如 `_.dstack-pha-prod5.phala.network`)在 compose 内;`CLOUDFLARE_API_TOKEN`(secret)与 `CERTBOT_EMAIL` 走 **sealed env**。
3. 部署后 dstack-ingress 自动完成 DNS+证书(约 2–5min,DNS-01 每次等 120s 传播);验证 `curl https://api.<域名>/v1/models` 返回模型目录、`/v1/attestation/report` 可达。旧 `<app-id>-8086.<node>.phala.network` 端点并存不受影响。
> 已验证:`api.apex1.us` 经此上线,Let's Encrypt 证书有效,端到端推理 200、attestation 可经新域名核对。

---

## 6. 验证(上线后)

```bash
BASE=https://<网关公网域名>; KEY=<一个 API key>; TOK=<control token>
# 1) 网关身份(真 TDX attestation)
curl -sS "$BASE/v1/attestation/report?nonce=$(openssl rand -hex 8)"
# 2) 控制面连通(从网关所在网络;公网应被 allowlist 挡掉)
curl -sS https://api.example.com/api/control/models -H "Authorization: Bearer $TOK"
# 3) 端到端推理(API key)
curl -sS "$BASE/v1/messages" -H "Authorization: Bearer $KEY" -H 'anthropic-version: 2023-06-01' \
  -H 'content-type: application/json' \
  -d '{"model":"claude-sonnet-5","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}'
# 4) 回执 + 离线验证
RID=<x-receipt-id>; curl -sS "$BASE/v1/aci/receipts/$RID" -H "Authorization: Bearer $KEY" > receipt.json
curl -sS "$BASE/v1/attestation/report?nonce=N" > report.json
# 在网关仓库执行:cargo run --example verify_aci_artifacts -- --report report.json --receipt receipt.json --nonce N
# 5) 计费:sub2api 侧 usage_logs +1(挂 tee-gateway 占位账号)、对应 user 个人余额按量减少
```

---

## 7. 运维

- **密钥轮换**:control token、上游 key 经 dstack secrets 轮换;轮换 control token 要两端同时更新。
- **可用性**:网关 middleware 在 sub2api 不可达时对 `/consult/pre` **fail-closed(503)**——sub2api 控制面需 HA、靠近网关、低延迟。
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
- [ ] DB 可 `CREATE EXTENSION pgcrypto`;迁移 `950`/`951` 已应用;`placeholder_account_id` 已设
- [ ] API key 已建;route_map 的 route_id 与网关 upstreams 对齐
- [ ] 网关以 git-launcher + **审计过的 COMMIT_SHA** 部署到真 dstack CVM
- [ ] 网关 `middleware.control_url=https://.../api/control`,`control_token` 经 dstack sealed env;`COMMIT_SHA` 写死进 `gateway-pin`
- [ ] 上游凭证经 dstack secrets 注入网关(不进 sub2api)
- [ ] `GET /v1/attestation/report` 的 `source_provenance` == 审计 commit
- [ ] 端到端推理 + 回执离线验证通过;计费落 usage_logs(tee-gateway 账号)+ 对应 user 个人余额扣减
- [ ] 向用户标注:机密 provider = 端到端机密;普通上游 = 仅防中转方

---

## 10. 附:部署 Meridian enclave(Claude 订阅路由,enclave B)

在上面的网关(enclave A)+ sub2api 之外,再加一条**用 Claude 订阅额度**的路由:
每个订阅账号跑一个独立的 **Meridian** 实例,把订阅桥成 Anthropic 兼容端点
(`/v1/messages`);网关把它当**非机密 `openai-compatible` 上游**注册,**零源码改动**。
部署单元/镜像/脚本都在 `deploy/meridian/`(见其 `README.md`);架构见 `docs/apexone-architecture.md`。

**拓扑:独立 enclave B**(`deploy/meridian/compose.cvm.yaml` + `deploy/gateway/compose.enclave.yaml`,已在 Phala dev CVM 端到端冒烟通过)。每个 seat 是独立 Phala CVM,暴露 phala 公网端点(ingress TLS);网关经该端点 + `MERIDIAN_API_KEY` Bearer 把它注册为上游。这样:
- **网关 attested 身份稳定**——加/换 seat 靠 `PUT /v1/admin/upstreams` 热加载,不改网关 `compose_hash`。
- **故障/升级隔离**——动 Meridian 不影响网关。
- 代价:网关→Meridian 那一跳带明文 prompt、走网络,靠 ingress TLS + Bearer 保护(Anthropic 本就见明文,非机密路由,见 §8)。
> **同一订阅账号只能跑一个 Meridian 实例**:两个实例共享账号会互相轮换/失效 refresh token。

### 10.1 拓扑与 seat 模型
```
[TEE CVM · enclave A] 网关(含进程内 middleware) ──(内网)──► [TEE CVM · enclave B] meridian-seat1 ──► Anthropic(订阅额度)
                                          └────────► [TEE CVM · enclave B'] meridian-seat2 ──► Anthropic
        │ consult(仅元数据, HTTPS+token)
        ▼
[非 TEE] sub2api(route_map: seats 轮换+failover)+ PG + Redis
```
- **一个 seat = 一个订阅账号 = 一个 Meridian 实例 = 网关一条 `meridian-seatN` 上游**。
- `HTTPS_PROXY` 是**进程级**的,所以「每 seat 固定出口 IP」必须**一 seat 一实例**(不能一个实例多账号)。
- Meridian 只承载 Claude 路由;非 Claude 模型仍走各自(机密/普通)上游,互不影响。

### 10.2 镜像
用 `deploy/meridian/Dockerfile`(`node:22-slim` + `@rynfar/meridian`,内置 gost 作 SOCKS5→HTTP shim)。
构建前先把 gost 拉进构建上下文:`cd deploy/meridian && ./fetch-gost.sh`(gost 二进制 gitignore,不入库)。
把镜像 digest 钉进该 CVM 的 attested `compose_hash`。

### 10.3 OAuth 订阅凭证(经 dstack sealed env 注入)
Meridian 用 Claude Code 订阅登录态。**生产经 dstack sealed env 注入文件内容**(不用 bind-mount 明文):
- `MERIDIAN_SEAT1_CREDENTIALS` = `secrets/seat1/.credentials.json` 内容(`claudeAiOauth`:access+refresh token)
- `MERIDIAN_SEAT1_CLAUDE_JSON` = `secrets/seat1/.claude.json` 内容——**只保留 `oauthAccount`**,`entrypoint.sh` 把二者落到容器内**可写**路径(`/root/.claude/*`),让 ~8h token 刷新持久化。

准备/刷新凭证(去掉本机隐私,只取 `oauthAccount`):
```bash
claude login                                  # 以该订阅账号登录(浏览器 OAuth)
cp ~/.claude/.credentials.json deploy/meridian/secrets/seat1/.credentials.json
jq '{oauthAccount}' ~/.claude.json > deploy/meridian/secrets/seat1/.claude.json
```
> `.claude.json` 原文含 `projects`/`machineID`/`userID` 等大量本机隐私,**务必只取 `oauthAccount`**。

**token 自动刷新 + 跨重启持久化(已实现)**:Meridian 运行中每 ~8h 自动刷新 OAuth token,写回 `/root/.claude/.credentials.json`。`compose.cvm.yaml` 把该路径挂在**持久卷 `meridian-claude`**(dstack CVM = TEE 加密存储),`entrypoint.sh` 按 `expiresAt` **"新者胜"**:卷里凭证 ≥ env 就保留卷、否则用 env。于是:
- 持续运行 + 重启/重部 → 刷新出的新 token 存活,seat **无人值守长期运行**(不再"重启回退旧凭证→401")。
- 运维强制换号/救活:`claude login` 后重新注入更新的凭证,因其 `expiresAt` 更新会被采用。
> 注意:refresh token 用一次会**轮换**,所以本地 secrets 拷贝、以及 sealed env 里的那份,会随实例刷新而变旧——正常运行靠持久卷续命,不依赖它们;只有卷丢失/长期停机过期才需 `claude login` 重注入。**同一账号只能跑一个实例**(两个会互相轮换失效)。
> `/health` 的 `loggedIn:true` 只表示凭证文件存在,**不代表 token 有效**;token 过期时推理返回 `401 Claude OAuth token has expired`。

### 10.3b 关闭内置 SDK 遥测(干净出网)
Meridian 打包的 `@anthropic-ai/claude-code` SDK 自带遥测(Statsig 特性开关 + Datadog 日志 + 错误上报),会外发**元数据**(非 prompt 正文)到 Anthropic 遥测设施。对 TEE 出网审计不利(enclave 除了必要的 `api.anthropic.com` 还会连 `datadoghq.com`),且占 ProxyLite 带宽。`compose.cvm.yaml` 已设 env 关闭:
```yaml
CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "1"   # 一把关遥测+错误上报+自动更新
DISABLE_TELEMETRY: "1"
DISABLE_ERROR_REPORTING: "1"
```
> 已验证:关闭后 gost 出网日志只剩 `api.anthropic.com:443`,无 `datadoghq`。功能无损。

### 10.4 ProxyLite 固定出口 IP(可选防封,每 seat 独立)
给某个 seat 设 `PROXYLITE_SOCKS5="socks5://<user>:<pass>@<host>:<port>"`,`entrypoint.sh` 会起
`gost -L http://127.0.0.1:8118 -F <PROXYLITE_SOCKS5>` 并让 Meridian 走它。**不设 = 直连**(enclave 自身 IP)。
> ⚠️ scheme 必须是 `socks5://`,**不要 `socks5h://`**:镜像内置的 gost v3 只认 `socks5://`(域名仍由 SOCKS5 端远端解析),`socks5h://` 会导致请求静默超时(`route(retry=0) unexpected EOF`)。
用**住宅/长效静态 IP**,一 seat 一 IP,跨续期保持不变(切忌高频轮换)。记录 seat↔IP 映射。
> 已验证:gost v3 兼容 `-L/-F` 语法;实测经 ProxyLite 出口 IP 与直连不同、CVM 内 meridian→anthropic 全经 `127.0.0.1:8118`。Meridian/Claude Code SDK 遵循 `HTTPS_PROXY`;经住宅静态 IP 出网未复现数据中心 IP 的 `403 Request not allowed`。
>
> **故障行为 = fail-closed(不自动降级直连)**:ProxyLite 过期/欠费/出 bug 时,gost 转发失败 → 该 seat 请求全失败,**不会**自动改走 CVM 数据中心 IP。这是**防封的正确取舍**(数据中心 IP 直连 Anthropic 曾触发 `403`、有风控风险),但意味着可用性依赖 ProxyLite——**务必续费别断,并加到期告警**。若确需"代理故障自动降级直连",需改 entrypoint 加探活,并接受降级期封号风险。

### 10.5 网关注册上游
每个 seat 一条上游(参考 `deploy/meridian/gateway-upstreams.example.json`):
```json
{
  "name": "meridian-seat1",
  "provider": "openai-compatible",
  "base_url": "https://<meridian-seat1 内网端点>",
  "path": "/v1/messages",
  "models": { "claude-opus-4-8": "claude-opus-4-8", "claude-sonnet-5": "claude-sonnet-5" },
  "bearer_token": "<该 seat 的 MERIDIAN_API_KEY,经 dstack secret>"
}
```
经 `upstreams.json`(dstack secrets 注入)或起后 `PUT /v1/admin/upstreams` 热加载。
`base_url` 只到域名/端点、不带 `/v1`;`path: /v1/messages` 指明走 Anthropic Messages 端点。

### 10.6 sub2api route_map(单/多 seat)
```yaml
consult:
  route_map:
    # 单 seat
    claude-opus-4-8:
      route_id: "meridian-seat1:claude-opus-4-8"
      format: "anthropic"
    # 多 seat:控制面轮换选主 + 其余作有序 failover(网关 middleware 的多候选转发)
    claude-sonnet-5:
      seats: ["meridian-seat1", "meridian-seat2"]
      format: "anthropic"
```
**⚠️ 只用精确名或 `claude-*` 前缀,禁止 `*` catch-all**,否则会把非 Claude/未知模型吞进 Claude 路由。
完整示例见 `deploy/config.example.yaml`。

### 10.7 多 seat 生成器
`deploy/meridian/gen-seats.sh` 从一份 `seats.json`(见 `seats.example.json`:含 `models` + 每 seat 的
`creds_dir`/`proxy`)一键生成:
- `compose.generated.yaml`(一 seat 一容器)、`upstreams.generated.json`(网关上游)、`route_map.generated.yaml`(sub2api)。

加 seat = 改 `seats.json` 重跑,再:`docker compose -f compose.generated.yaml up -d` → 上游经
`PUT /v1/admin/upstreams` 热加载 → route_map 合并进 config(viper watch 热加载),**全程零重启**。

### 10.8 安全边界(务必向用户讲清)
Meridian 路由是**非机密路由**(等同第 8 节表中的 `openai-compatible` 行):

| 路由 | sub2api/运营方可见? | 上游模型方可见? | fail-closed | receipt attestation |
|---|---|---|---|---|
| Claude(订阅,经 Meridian) | 否(数据只过 enclave) | **是(Anthropic 见明文)** | 否 | 无 |

即:你的中转方拿不到 prompt/订阅凭证,但 **Anthropic 能看到明文**;要连模型方也挡住需换机密 provider。
订阅凭证的机密性靠「Meridian 在 TEE enclave 内 + 凭证经 dstack secrets 注入」保证。

### 10.9 独立 enclave B 部署实操(生产,已冒烟)
每个 seat 一个独立 Phala CVM,用 `deploy/meridian/compose.cvm.yaml`;网关用 `deploy/gateway/compose.enclave.yaml`(空 seed)。部署顺序:先 seat,拿到端点,再热加载到网关。

```bash
# 1) 部署 Meridian seat 独立 CVM(sealed env 传凭证/代理/API key)
cd deploy/meridian
MERIDIAN_API_KEY=mrdn-$(openssl rand -hex 24)      # 记下:网关 upstream bearer_token 用相同值
cat > /tmp/seat.env <<EOF
MERIDIAN_API_KEY=$MERIDIAN_API_KEY
MERIDIAN_SEAT1_PROXY=socks5://<user>:<pass>@<host>:<port>
MERIDIAN_SEAT1_CREDENTIALS=$(jq -c . secrets/seat1/.credentials.json)
MERIDIAN_SEAT1_CLAUDE_JSON=$(jq -c . secrets/seat1/.claude.json)
EOF
phala deploy -n meridian-seat1 -c compose.cvm.yaml -e /tmp/seat.env && shred -u /tmp/seat.env
# 取 CVM 的公网 :3456 端点 E(phala cvms get <app_id> --json → endpoints[].app)
# 校验:curl $E/health → loggedIn:true;无 key 打 /v1/messages 应 401(鉴权生效)

# 2) 网关(enclave A)用 gateway-only compose(空 seed → 身份稳定)
cd ../gateway
phala deploy --cvm-id <网关app_id> -c compose.enclave.yaml -e <网关sealed.env>

# 3) 热加载该 seat 到网关(以后加/换 seat 都只做这步,网关不重部、身份不变)
curl -X PUT https://<网关>/v1/admin/upstreams -H "Authorization: Bearer <ADMIN_TOKEN>" \
  -d "[{\"name\":\"meridian-seat1\",\"provider\":\"openai-compatible\",\"base_url\":\"$E\",
       \"path\":\"/v1/messages\",\"models\":{\"claude-opus-4-8\":\"claude-opus-4-8\",\"claude-sonnet-5\":\"claude-sonnet-5\"},
       \"bearer_token\":\"$MERIDIAN_API_KEY\"}]"
```
> ⚠️ 网关 `gateway-state` 卷会**持久化 upstreams.json**:空 seed 只在首次启动(无既存 state)生效;复用旧 CVM 时旧上游会残留,直到 `PUT` 覆盖。全新 enclave A 部署则空 seed 即真空,等 `PUT`。
> 校验:receipt 的 `upstream.verified.url_origin` == 该 seat 的公网端点;seat 容器 gost 日志有 `api.anthropic.com` 经 `127.0.0.1:8118`(走 ProxyLite)。

### 10.9b 多账号:一个 CVM 跑 N 个 seat(省成本,推荐)
"一账号一 CVM"成本随账号线性涨。因**每 Meridian 空闲仅 ~60MB**、真正吃资源的是并发(默认 `MERIDIAN_MAX_CONCURRENT=10`/seat),可把 **N 个 seat 容器塞进一个 CVM**。每 seat 仍有:独立端口、独立 `MERIDIAN_<SEAT>_API_KEY`、独立 ProxyLite 静态 IP、独立持久卷——**每账号独立出口 IP 不打折**。网关把每 seat 当独立 `base_url` 上游注册(沿用现有路由,零改动)。

**选型**:单 CVM 目标 30 账号用 **`tdx.xlarge`(8vCPU/16GB)**(空闲 ~2GB + ~14GB 并发余量)。账号少时先用小规格,`phala cvms resize <cvm> tdx.xlarge` 按需长大。

用 `deploy/meridian/gen-seats.sh` 从一份 `seats.json` 一键生成:
```bash
cd deploy/meridian
# seats.json 每账号一条: {"name":"seatN","creds_dir":"secrets/seatN","proxy":"socks5://<seatN 静态IP>"}
./gen-seats.sh
#   -> compose.seats.generated.yaml  (N 容器/单 CVM,含持久卷/关遥测/鉴权/proxy)
#   -> route_map.generated.yaml       (consult.route_map: 各模型 -> 全 seat 轮换+failover)
#   -> seats.env.template             (要填的 sealed env 变量名)
#   -> register-upstreams.generated.sh(把全部 seat PUT 到网关)

# 按 seats.env.template 填 seats.env(每 seat 的 API key/proxy/creds),然后:
phala deploy -n meridian-seats -c compose.seats.generated.yaml -e seats.env
# 取该 CVM 的 app_id 与 node 域(phala cvms get），注册全部上游:
GATEWAY_URL=https://<网关> GATEWAY_ADMIN_TOKEN=<admin> \
MERIDIAN_APP_ID=<meridian-cvm app_id> MERIDIAN_NODE=dstack-pha-prodX.phala.network \
MERIDIAN_SEAT1_API_KEY=... MERIDIAN_SEAT2_API_KEY=... bash register-upstreams.generated.sh
# 把 route_map.generated.yaml 并进 sub2api config.yaml(viper 热加载)
```
**加账号** = seats.json 加一条 → `./gen-seats.sh` → seats.env 补该 seat → 重部这个 CVM(接近容量先 resize)→ 跑 register 脚本 → route_map 热加载。**网关始终不动**。
> ⚠️ 迁移/重部会按 `expiresAt` "新者胜"从持久卷或 env 落地凭证;首次从单-seat `compose.cvm.yaml`(卷 `meridian-claude`)切到多-seat(卷 `meridian-<seat>-claude`)时,seat 会从 sealed env 重新落地凭证——**迁移前对相关账号 `claude login` 刷新 secrets** 再注入,避免用到旧的已轮换 token。
> ⚠️ 单 CVM 的**爆炸半径**:重部/重启会同时影响该 CVM 上所有 seat。账号很多时可分 2–3 个 CVM 降低影响。

### 10.9c 车队维护:监控 + 一键刷新(30 账号必备)
**判断账号是否要重登不能看 `/health`**(它只查文件存在,token 过期仍报 `loggedIn:true`)——必须**逐 seat 打真实推理探测**。工具在 `deploy/meridian/`:

- **`check-seats.sh`**:读 seats.json,逐 seat 打 1-token 请求并分类:`OK` / `TOKEN_EXPIRED`(该账号需 `claude login`)/ `AUTH_FAIL`(key 配错)/ `BANNED/FLAGGED`(403)/ `PROXY_OR_DOWN`(超时/5xx)。打印状态表 + 有异常时**发邮件**;任一异常退出 1(cron MAILTO 也能用)。
  ```bash
  # 需 env: MERIDIAN_APP_ID / MERIDIAN_NODE / 各 MERIDIAN_<SEAT>_API_KEY
  # 邮件(可选): ALERT_TO ALERT_FROM SMTP_HOST SMTP_PORT(465=SSL/587=STARTTLS) SMTP_USER SMTP_PASS
  # cron 每 15 分钟:
  */15 * * * * set -a; . /path/seats.env; . /path/alert.env; set +a; \
               /path/deploy/meridian/check-seats.sh >> /var/log/meridian-check.log 2>&1
  ```
- **`refresh-seat.sh <seat> <meridian-cvm-id>`**:某 seat 报 `TOKEN_EXPIRED` 时,对该账号 `claude login` + 覆盖 `secrets/<seat>/` 后,一条命令完成"更新该 seat sealed env → 重启",entrypoint "新者胜"自动采用新凭证。

**关键认知**:token 过期**不是急事**——route_map `seats:[...]` 会 failover 绕过坏 seat,只是少一份配额,整体服务不断。监控告诉你修哪个,你从容处理。降低断链频率可用 `claude setup-token`(长效 token)。

### 10.10 Meridian 上线检查清单
- [ ] 每个订阅账号一个独立 Meridian 实例(enclave B);镜像 digest 进 attested `compose_hash`
- [ ] 公网暴露必设 `MERIDIAN_API_KEY`(无 key 应 401);网关 upstream `bearer_token` = 同值
- [ ] `.credentials.json`/`.claude.json` 经 **dstack sealed env** 注入,容器内可写以持久化刷新;`.claude.json` 只留 `oauthAccount`
- [ ] (可选)每 seat 的 `PROXYLITE_SOCKS5` 指向各自**住宅静态 IP**,seat↔IP 映射已记录
- [ ] 每 seat 一条 `meridian-seatN` 上游(`openai-compatible` + `path:/v1/messages`)已注册,`base_url` 不带 `/v1`
- [ ] route_map 用精确名或 `claude-*`(**无 `*` catch-all**);多 seat 用 `seats: [...]`
- [ ] 端到端:`claude -p` 多步任务跑通;回执 `route.selected` 为 `meridian-seatN:...` 且**无 attestation**;计费落库
- [ ] 已向用户标注:此路由 **Anthropic 见明文**、非端到端机密
