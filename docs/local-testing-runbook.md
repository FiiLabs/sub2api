# 本地集成测试完整手册:sub2api + private-ai-gateway + Meridian + ProxyLite

**目标**:只看本文档,从零在本地把整套集成跑起来并验证。链路是——你(curl / Claude Code)
用一个 **team key** 发请求 → **private-ai-gateway**(隐私网关)→ **executor**(中间件)
→ **sub2api**(控制面:鉴权/选路/计费,只见元数据)→ **Meridian**(用你的 **Claude 订阅额度**
调 Anthropic,**可选**经 **ProxyLite** 固定出口 IP)→ Anthropic。最终验证:鉴权、路由、
回执(receipt)、计费,以及(可选)固定出口 IP 生效。

> 即使你没接触过这些组件,按本文档从上到下照做即可。全程本地,不需要真 TEE 硬件
> (用 dstack **simulator** 模拟,仅供功能测试,**不提供真实安全**)。

---

## 1. 这是什么 / 各组件与链路

```
你(curl / Claude Code)
   │  Authorization: Bearer <team key>
   ▼
private-ai-gateway(隐私网关 :8086)──► executor(中间件)──► sub2api(:8080 /api/control:鉴权/选路/计费,只见元数据)
   │                                                          
   ▼  按选路命中的上游转发(数据面,不经过 sub2api)
Meridian(meridian-seat1 :3456,用 Claude 订阅登录态)
   │  (可选) gost SOCKS5→HTTP shim ──► ProxyLite 固定出口 IP
   ▼
Anthropic API(用订阅额度扣减)
```

| 组件 | 通俗解释 | 仓库 / 来源 |
|---|---|---|
| **private-ai-gateway** | "隐私网关"。本应跑在 TEE 里,对外提供 OpenAI/Anthropic 兼容接口,转发前验证上游、并给每次请求签一张"回执"。 | `https://github.com/PublicAI01/private-ai-gateway.git`(**dev 分支**) |
| **executor** | 网关自带中间件(在网关仓库 `middleware/executor`),把网关和 sub2api 连起来,并做请求格式转换(OpenAI ↔ Anthropic)。 | (同上,子目录) |
| **sub2api** | "控制面":管理 API key、决定某 key 能用哪些模型/路由、记用量计费。**只接触元数据**(key 哈希、模型名、token 数),不接触 prompt 内容。 | `https://github.com/FiiLabs/sub2api.git`(**tee-control 分支**) |
| **dstack simulator** | 本地模拟 TEE 的密钥服务(KMS/quote),让网关无真 TEE 硬件也能启动。⚠️ 仅功能测试。 | `https://github.com/Dstack-TEE/dstack.git`(simulator 在 `sdk/simulator`) |
| **Meridian** | 把一个 **Claude Max/Pro 订阅账号**桥成 Anthropic 兼容端点(`/v1/messages`)。网关把它当普通 `openai-compatible` 上游。以 Docker 容器运行。 | npm 包 `@rynfar/meridian`;部署单元在 `$SUB2API/deploy/meridian/` |
| **ProxyLite**(可选) | SOCKS5 代理,给某个 seat 提供**固定出口 IP**(防封)。Meridian/Claude SDK 只认 HTTP 代理,故镜像内置 **gost** 作 SOCKS5→HTTP shim。 | 你自备的 ProxyLite(或任意 SOCKS5)端点 |

关键名词:
- **TEE / 控制面 / 数据面**:控制面 = 鉴权/路由/计费(只传元数据,走到 sub2api);数据面 = 真正的 prompt/响应(只经过网关+上游,**不经过 sub2api**)。
- **team key**:带"团队"归属的 API key,设计上**只能走网关(TEE)**;personal key 走 sub2api 自己的代理。本手册测 team key。
- **seat(席位)**:一个 Claude 订阅账号 = 一个 Meridian 实例 = 网关里一条 `meridian-seatN` 上游。要多账号池化就多开 seat(见 §11)。
- **route_map**:sub2api 里的表,把"客户端发来的模型名"映射到"网关的某条上游路由"。
- **receipt(回执)**:网关为每次请求签名的记录,可事后核验。

> **隐私边界(先知道)**:Meridian 走的是**非机密路由**——数据对 sub2api/中转方不可见(只过网关),
> 但 **Anthropic 能看到明文**,回执 `upstream.verified` 无 attestation、不 fail-closed。
> 要"连模型方也看不到"需换机密 provider(tinfoil/phala-direct/near-ai/chutes/aci-dcap),见 §13。

---

## 2. 前置条件(先装好)

- `git`、`openssl`、`curl`、`jq`
- **Rust**(stable;构建网关 + dstack simulator)—— `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh`
- **Go ≥ 1.23**(构建 sub2api)
- **Node ≥ 18 + npm**(构建/运行 executor)
- **Docker**(跑 Meridian 容器;也用来跑 `psql` 查库,免装 psql)
- **PostgreSQL + Redis**(sub2api 的依赖,本地可连)
- **一个 Claude 订阅账号(Max/Pro)+ Claude Code CLI**(`claude`)—— 用于 `claude login` 产出订阅登录态,供 Meridian 使用
- **(可选)一个 ProxyLite / SOCKS5 端点**:形如 `socks5://<user>:<pass>@<host>:<port>`,用于固定出口 IP(§10)

---

## 3. 获取代码 + 设路径变量

```bash
mkdir -p ~/work && cd ~/work

# 网关 + executor(PublicAI01 的 dev 分支)
git clone -b dev https://github.com/PublicAI01/private-ai-gateway.git
# 控制面(tee-control 分支)
git clone -b tee-control https://github.com/FiiLabs/sub2api.git
# dstack(取其 simulator)
git clone https://github.com/Dstack-TEE/dstack.git

# 设变量(本文档后续都用这些,不写死路径)
export PAG=~/work/private-ai-gateway          # 网关 + executor
export SUB2API=~/work/sub2api                 # 控制面(后端在 $SUB2API/backend;部署件在 $SUB2API/deploy)
export DSTACK_SIM=~/work/dstack/sdk/simulator # dstack simulator 目录
```
> 这些 `export` 只在当前终端有效。开新终端要重新 export(或写进 `~/.bashrc`)。

---

## 4. 构建各程序

```bash
# 网关(Rust)
cd "$PAG" && cargo build --release --bin private-ai-gateway

# executor(Node/TypeScript)
cd "$PAG/middleware/executor" && npm install && npm run build

# sub2api(Go)
cd "$SUB2API/backend" && go build -o sub2api ./cmd/server

# dstack simulator(Rust)
cd "$DSTACK_SIM" && ./build.sh    # 生成 ./dstack-simulator
```
> Meridian 不用本地构建源码——它以 Docker 镜像运行,§7/§8 会用 `docker compose ... up --build` 自动构建。

---

## 5. 关键概念:三个 model 名(最容易踩坑,务必理解)

一次请求里"模型名"有三个,分属不同环节,必须对应起来(以本手册的 Meridian 路由为例):

| 名字 | 在哪里设置 | 本手册例子 |
|---|---|---|
| **① 客户端 model**(= sub2api `route_map` 的 key) | 你发请求的 `model` 字段 + sub2api 配置 | `claude-opus-4-6` |
| **② routeId** = `<网关upstream名>:<公开模型id>` | route_map 的 `route_id`(或 `seats` 自动拼),要和网关 upstreams 对上 | `meridian-seat1:claude-opus-4-6` |
| **③ 上游 model**(= 网关 upstreams 里 `models` 的 value) | 网关的上游配置 | `claude-opus-4-6` |

流程:你发 **①** → sub2api 用 route_map 映射成 **②** → 网关按 ② 找到上游(`meridian-seat1`),
把"公开模型id"改写成 **③** 发给 Meridian。三者对不上就报 404 或路由失败。

> Meridian 路由里,为了少踩坑,建议让 ①②③ 的模型名一致(都用 `claude-opus-4-6`),
> 即 upstream 的 `models` 写 `{"claude-opus-4-6": "claude-opus-4-6"}`。

---

## 6. 一次性准备(token / team key / 占位 account / 订阅登录态)

### 6.1 control token(executor 和 sub2api 共用同一个)
```bash
openssl rand -hex 32 | tee /tmp/pag-control-token.txt
```
> 用一键脚本时(§8),缺这个文件会自动生成并打印出来。

### 6.2 team key
在 sub2api 里创建一个**带 team 的 API key**(通过 sub2api 后台/接口;新建的 key 会自动写入用于查找的哈希)。
记下明文,测试时作 `Authorization: Bearer` 用。

### 6.3 占位 account id(计费用)
sub2api **首次启动会自动跑迁移**,建一个名为 `tee-gateway` 的占位账号。查其 id:
```bash
# <db用户>/<db密码>/<dbname> 取自 $SUB2API/backend/config.yaml 的 database 段
docker run --rm --network host -e PGPASSWORD=<db密码> postgres:16-alpine \
  psql -h 127.0.0.1 -U <db用户> -d <dbname> -tAc \
  "SELECT id FROM accounts WHERE name='tee-gateway';"
```
记下 id,填进 §7.1 的 `placeholder_account_id`(填 0 也能跑通推理,只是不记计费)。

### 6.4 Claude 订阅登录态(给 Meridian 用)
用 Claude Code CLI 登录你的订阅账号,产出登录态文件:
```bash
claude login    # 交互式登录一个 Claude Max/Pro 订阅账号
# 成功后本机会有:
#   ~/.claude/.credentials.json  (claudeAiOauth:access + refresh token)
#   ~/.claude.json               (oauthAccount 元数据)
```
> 这两个文件会被注入 Meridian 容器(§7.4);OAuth token 约 8h 自动刷新。

---

## 7. 配置文件

### 7.1 sub2api:在 `$SUB2API/backend/config.yaml` 加 `consult` 段(把 Claude 模型指到 Meridian)
```yaml
consult:
  control_token: "<与 /tmp/pag-control-token.txt 里的值一致>"
  placeholder_account_id: <第 6.3 步查到的 id>     # 0 = 不计费(推理仍通)
  route_map:
    # key = 客户端发的模型名;route_id = <网关upstream名>:<公开模型id>;format = anthropic(Meridian 走 Anthropic 语义)
    # ⚠️ Meridian(Claude 订阅)路由只用精确名或 claude-* 前缀,禁用 "*" catch-all,否则会把非 Claude 模型也吞进来。
    claude-opus-4-6:
      route_id: "meridian-seat1:claude-opus-4-6"
      format: "anthropic"
    claude-sonnet-4-6:
      route_id: "meridian-seat1:claude-sonnet-4-6"
      format: "anthropic"
```
> 完整示例(含多 seat)见 `$SUB2API/deploy/config.example.yaml`。
> `/api/control/models`(模型目录)只列**具体 key**,不列通配/兜底 key。

### 7.2 网关主配置:新建 `/tmp/pag-stage2.config.json`
```json
{
  "bind": "127.0.0.1:8086",
  "state_dir": "/tmp/pag-stage2-state",
  "upstream_config_seed_path": "/tmp/pag-stage2-upstreams.seed.json",
  "dstack_endpoint": "unix:/tmp/aci-dstack-sock-dev.dstack.sock",
  "executor": {
    "uds_path": "/tmp/pag-stage2/executor.sock",
    "backend_uds_path": "/tmp/pag-stage2/backend.sock"
  }
}
```
准备目录:`mkdir -p /tmp/pag-stage2 /tmp/pag-stage2-state`

> 用一键脚本(§8)时,§7.2/§7.3/§7.5 会自动生成,你只需做 §7.1(sub2api)和 §7.4(凭证)。

### 7.3 网关上游 seed:新建 `/tmp/pag-stage2-upstreams.seed.json`(把 Meridian 注册为上游)
```json
[
  {
    "name": "meridian-seat1",
    "provider": "openai-compatible",
    "base_url": "http://127.0.0.1:3456",
    "path": "/v1/messages",
    "models": { "claude-sonnet-4-6": "claude-sonnet-4-6", "claude-opus-4-6": "claude-opus-4-6" },
    "bearer_token": "devsecret"
  }
]
```
> - 网关是宿主机进程,Meridian 是容器(§8 会把它发布到 `127.0.0.1:3456`),故 `base_url` 用 `http://127.0.0.1:3456`。
> - `path: /v1/messages` 指明走 Anthropic Messages 端点;`base_url` **只写到域名/端点,不带 `/v1`**。
> - `name`(meridian-seat1)+ `models` 的 key 拼成 routeId,要与 §7.1 的 `route_id` 完全一致。
> - `bearer_token` = Meridian 容器的 `MERIDIAN_API_KEY`(本手册用 `devsecret`)。
> - 参考模板:`$SUB2API/deploy/meridian/gateway-upstreams.example.json`。
> - 改了这个文件后,`rm -f /tmp/pag-stage2-state/upstreams.json` 再重启网关才会重载(seed 只在 active 为空时生效)。

### 7.4 Meridian 订阅凭证注入(把 6.4 的登录态放进 seat1)
```bash
mkdir -p "$SUB2API/deploy/meridian/secrets/seat1"
cp ~/.claude/.credentials.json "$SUB2API/deploy/meridian/secrets/seat1/"
cp ~/.claude.json              "$SUB2API/deploy/meridian/secrets/seat1/"
```
> 本地用 bind-mount 即可;生产改用 dstack 加密 secrets(见 `docs/production-deployment.md`)。
> 用一键脚本 + `MERIDIAN_COPY_CREDS=1` 时,此步会自动完成。

### 7.5 dstack simulator 的 socket 软链
```bash
ln -sf "$DSTACK_SIM/dstack.sock" /tmp/aci-dstack-sock-dev.dstack.sock
```
(simulator 启动后会在 `$DSTACK_SIM/dstack.sock` 创建该文件。)

---

## 8. 启动整套栈

先起 **Postgres / Redis**,配好 §7.1 后起 **sub2api**;然后用一键脚本把
**simulator + 网关 + executor + Meridian** 一起拉起来。

### 8.1 起 sub2api(终端 1)
```bash
# 确保 Postgres / Redis 已在跑;$SUB2API/backend/config.yaml 已按 §7.1 配好 consult 段
cd "$SUB2API/backend" && ./sub2api      # 首次启动自动建表/迁移;监听 :8080
```

### 8.2 一键起网关侧 + Meridian(终端 2,推荐)
脚本在 `$SUB2API/docs/`,会自动:生成 token/网关配置/上游 seed(含 `meridian-seat1`)、
软链 dstack socket、按序起 **simulator→网关→executor**,并(开关打开时)用 Docker 起
**Meridian seat1**(发布 `127.0.0.1:3456`)、做健康检查。
```bash
# 先确保:已 export PAG / DSTACK_SIM / SUB2API,各程序已构建(§4),已 claude login(§6.4)
cd "$SUB2API/docs"
MERIDIAN_LOCAL=1 MERIDIAN_COPY_CREDS=1 ./start-local.sh
```
- `MERIDIAN_LOCAL=1`:同时起本地 Meridian seat 并写进网关 seed。
- `MERIDIAN_COPY_CREDS=1`:缺凭证时自动从 `~/.claude` 拷到 `deploy/meridian/secrets/seat1`(= §7.4)。
- 要经 ProxyLite 固定出口 IP,见 §10(加一个环境变量即可)。

脚本还会:缺 control token 时自动生成并打印(填进 §7.1 的 `consult.control_token` 后**重启 sub2api**);
`Dockerfile` 需要的 `gost` 缺失时自动 `fetch-gost.sh`;日志写在 `/tmp/pag-local-logs/`。

> 若脚本提示"seed 已存在但无 meridian-seat1",按提示加好(见 §7.3)并
> `rm -f /tmp/pag-stage2-state/upstreams.json` 后重跑。

**就绪标志**:sub2api 监听 8080;网关日志 `private-ai-gateway listening bind=127.0.0.1:8086`;
executor `executor listening on unix socket ...`;`curl -s http://127.0.0.1:3456/health` 返回
`{"auth":{"loggedIn":true},...}`。

### 8.3 手动起(不想用脚本时,开多个终端)
<details>
<summary>展开手动步骤</summary>

```bash
# 终端 2:dstack simulator(要在网关之前起)
cd "$DSTACK_SIM" && ./dstack-simulator

# 终端 3:Meridian seat1(容器,发布 127.0.0.1:3456;设 MERIDIAN_API_KEY=devsecret)
cd "$SUB2API"
[ -f deploy/meridian/gost ] || ( cd deploy/meridian && ./fetch-gost.sh )   # Dockerfile 需要 gost
docker compose -f deploy/meridian/compose.yaml \
  -f <(printf 'services:\n  meridian-seat1:\n    ports: ["127.0.0.1:3456:3456"]\n    environment:\n      MERIDIAN_API_KEY: "devsecret"\n') \
  up -d --build
curl -s http://127.0.0.1:3456/health     # 期望 loggedIn:true

# 终端 4:网关(要在 simulator 之后)
cd "$PAG"
PRIVATE_AI_GATEWAY_CONFIG_PATH=/tmp/pag-stage2.config.json ./target/release/private-ai-gateway

# 终端 5:executor(要在网关之后,共享 /tmp/pag-stage2/ 下的 socket)
cd "$PAG/middleware/executor"
PRIVATE_AI_GATEWAY_EXECUTOR_UDS_PATH=/tmp/pag-stage2/executor.sock \
PRIVATE_AI_GATEWAY_BACKEND_UDS_PATH=/tmp/pag-stage2/backend.sock \
PRIVATE_AI_GATEWAY_CONTROL_URL=http://127.0.0.1:8080/api/control \
PRIVATE_AI_GATEWAY_CONTROL_TOKEN="$(cat /tmp/pag-control-token.txt)" \
node build/server.js
```
顺序要点:**simulator 在网关之前**(网关启动要连它的 socket);**executor 在网关之后**。
</details>

---

## 9. 验证 / 测试(核心集成)

```bash
KEY=<你的 team key>                       # 第 6.2 步创建的
TOK=$(cat /tmp/pag-control-token.txt)
```

### 9.1 连通性(排障用)
```bash
# 直连 sub2api 控制面,应返回模型目录 JSON(含 claude-opus-4-6 / claude-sonnet-4-6)
curl -sS http://127.0.0.1:8080/api/control/models -H "Authorization: Bearer $TOK"
# 经网关+executor 的目录(客户端无需带 token)
curl -sS http://127.0.0.1:8086/v1/models
# Meridian 自身健康
curl -s http://127.0.0.1:3456/health      # {"auth":{"loggedIn":true},...}
```

### 9.2 走 Meridian 的 Claude 推理(Anthropic 接口,含流式)
```bash
# 非流式
curl -sS http://127.0.0.1:8086/v1/messages -H "Authorization: Bearer $KEY" \
  -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \
  -d '{"model":"claude-opus-4-6","max_tokens":200,"messages":[{"role":"user","content":"用一句话自我介绍"}]}'
# 流式(应看到 Anthropic SSE:message_start / content_block_delta / ...)
curl -sS -N http://127.0.0.1:8086/v1/messages -H "Authorization: Bearer $KEY" \
  -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \
  -d '{"model":"claude-sonnet-4-6","max_tokens":200,"stream":true,"messages":[{"role":"user","content":"你好"}]}'
```

### 9.3 用真实 Claude Code(多步 agentic 任务)
```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8086
export ANTHROPIC_AUTH_TOKEN=$KEY
claude -p "列出当前目录并读取其中一个文件的前5行"   # 已验证可跑通客户端驱动的多轮工具循环
```
> Claude Code 发出的模型名必须能在 route_map 里匹配(精确或 `claude-*`),否则 404 "Unknown model"。

### 9.4 查回执(receipt)
响应头有 `x-receipt-id`;回执是"owned"的,查询要带同一个 key:
```bash
RID=<响应头里的 x-receipt-id>
curl -sS http://127.0.0.1:8086/v1/aci/receipts/$RID -H "Authorization: Bearer $KEY" | python3 -m json.tool
```
关注事件链:`route.selected`(routeId 应为 `meridian-seat1:claude-opus-4-6`)、`upstream.verified`
(Meridian 是非机密路由,**无 attestation**)、`response.returned`。

### 9.5 验证计费(需 `placeholder_account_id` ≠ 0)
> 本分支(**tee-control**)为纯控制面,**无 billing_subject / 团队账本**。TEE 用量经
> `POST /consult/post` 记到占位账号 `tee-gateway` 下的 `usage_logs`,并按 **user 维度**扣个人余额:
```sql
-- 用量明细(列名:model / input_tokens / output_tokens / total_cost)
SELECT model, input_tokens, output_tokens, total_cost FROM usage_logs
  WHERE account_id IN (SELECT id FROM accounts WHERE name='tee-gateway') ORDER BY id DESC LIMIT 5;
-- 余额扣减(消费后应下降;扣减额 = 上面各条 total_cost 之和)
SELECT id, balance FROM users WHERE id=<user id>;
-- 占位账号存在(status 为 disabled 属正常——占位账号只用于挂 usage_logs,不参与鉴权)
SELECT id, name, status FROM accounts WHERE name='tee-gateway';
```

---

## 10. (可选)ProxyLite 固定出口 IP —— 防封

Meridian / Claude SDK 只认 HTTP 代理,而 ProxyLite 是 **SOCKS5**;Meridian 镜像内置 **gost**
作 SOCKS5→HTTP shim。给 seat 设 `PROXYLITE_SOCKS5` 即启用,该 seat 的 Anthropic 流量就从
ProxyLite 的**固定 IP** 出网(不设 = 直连,用本机/enclave 自身 IP)。

### 10.1 用一键脚本启用(在 §8.2 基础上多加一个变量)
```bash
cd "$SUB2API/docs"
PROXYLITE_SOCKS5="socks5://<user>:<pass>@<host>:<port>" \
MERIDIAN_LOCAL=1 MERIDIAN_COPY_CREDS=1 ./start-local.sh
```
脚本会把 `PROXYLITE_SOCKS5` 透传进 Meridian 容器,`entrypoint.sh` 自动起
`gost -L http://127.0.0.1:8118 -F <PROXYLITE_SOCKS5>` 并让 Meridian 走它。

### 10.2 手动启用(§8.3 的 compose 覆盖里加一行)
```bash
docker compose -f deploy/meridian/compose.yaml \
  -f <(printf 'services:\n  meridian-seat1:\n    ports: ["127.0.0.1:3456:3456"]\n    environment:\n      MERIDIAN_API_KEY: "devsecret"\n      PROXYLITE_SOCKS5: "socks5://<user>:<pass>@<host>:<port>"\n') \
  up -d --build
```

### 10.3 验证出口 IP 确实是 ProxyLite 的固定 IP
```bash
# 容器内经代理看到的出口 IP(应为 ProxyLite 的静态 IP,而非你本机公网 IP)
docker compose -f deploy/meridian/compose.yaml exec meridian-seat1 \
  sh -c 'HTTPS_PROXY=http://127.0.0.1:8118 curl -s https://api.ipify.org'; echo
# 再打一次 §9.2 的请求,确认仍正常返回(即"经固定 IP 调 Anthropic"链路通)
```
> 建议用**住宅/长效静态 IP**,一 seat 一 IP、跨续期保持不变(切忌高频轮换)。
> 曾观察到:经数据中心 IP 出网可能 `403 Request not allowed`,经住宅静态 IP 未复现。

---

## 11. (可选)多 seat:轮换 + failover

一个 Claude 模型可配多个订阅账号(seat),控制面轮换选主、其余作有序 failover:
```yaml
# $SUB2API/backend/config.yaml 的 consult.route_map
    claude-sonnet-4-6:
      seats: ["meridian-seat1", "meridian-seat2"]   # seats 与 route_id 二选一,同时设则 seats 优先
      format: "anthropic"
```
用 `$SUB2API/deploy/meridian/gen-seats.sh` 从一份 `seats.json`(见同目录 `seats.example.json`:
含 `models` + 每 seat 的 `creds_dir`/可选 `proxy`)一键生成三件套:
```bash
cd "$SUB2API/deploy/meridian"
cp seats.example.json seats.json      # 编辑:每个 seat 一个订阅账号 + (可选)各自 ProxyLite IP
./gen-seats.sh seats.json
#   compose.generated.yaml   —— 一 seat 一容器(各自发布端口:3456, 3457, ...)
#   upstreams.generated.json —— 网关上游(经 PUT /v1/admin/upstreams 热加载)
#   route_map.generated.yaml —— 合并进 sub2api config.yaml(viper watch 热加载)
```
每个 seat 的订阅凭证放各自 `secrets/<seat>/`(同 §7.4)。

**生成后的接线(三处)**:
```bash
cd "$SUB2API/deploy/meridian"
# 1) 起/更新容器(镜像 meridian-enclave:dev;缺则先 docker compose -f compose.yaml build 并 tag)
docker compose -f compose.generated.yaml up -d
curl -s http://127.0.0.1:3456/health; curl -s http://127.0.0.1:3457/health   # 都应 loggedIn:true

# 2) 网关上游:二选一
#    (a) 热加载(推荐,无需重启):网关 config 需先设 admin_token(见下)
curl -X PUT http://127.0.0.1:8086/v1/admin/upstreams \
  -H "Authorization: Bearer <admin_token>" -H 'content-type: application/json' \
  --data @upstreams.generated.json
#    (b) 或替换 seed 后重启网关:
#        cp upstreams.generated.json /tmp/pag-stage2-upstreams.seed.json
#        rm -f /tmp/pag-stage2-state/upstreams.json   # seed 仅在 active 为空时加载
#        重启网关(+executor)

# 3) route_map:把 route_map.generated.yaml 的 consult.route_map 合并进 $SUB2API/backend/config.yaml
#    (viper watch 自动热加载,日志出现 "consult route_map hot-reloaded";无需重启 sub2api)
```
> 验证轮换:连发数请求,查各 receipt 的 `route.selected.target_route_id`,应在 `meridian-seat1/seat2` 间 round-robin。
> 验证 failover:`docker compose -f compose.generated.yaml stop meridian-seat2`,再发请求应全部 200 且 `route.selected` 落到存活的 seat1。

**注意事项(实测踩坑)**:
- **`proxy` 用 `socks5://`,不要 `socks5h://`**:镜像内 gost v3 只认 `socks5://`(域名仍由 SOCKS5 端远端解析);`socks5h://` 会静默超时。
- **`bearer_token` 生成为 `"x"`**:gen-seats 的容器不设 `MERIDIAN_API_KEY`,而 Meridian 未设 key 时不校验入站 bearer,故上游 `bearer_token:"x"` 即可通(要收紧就给容器设 `MERIDIAN_API_KEY` 并让上游 bearer 与之一致)。
- **admin 热加载需 `admin_token`**:§7.2 的默认网关 config **没有** `admin_token`,`PUT /v1/admin/upstreams` 会 404;在 `/tmp/pag-stage2.config.json` 加 `"admin_token":"<一串>"` 重启一次网关即可启用(之后加 seat 才是真·no-restart)。
- **单订阅模拟多 seat**(本地只有 1 个 Claude 订阅 + 1 个静态 IP 时):把同一份 `secrets/seat1` 凭证复制成 `secrets/seat2`,两 seat 共用同一 `proxy` —— 可完整验证轮换/failover/固定出口 IP(生产务必一 seat 一独立账号 + 一独立 IP)。

---

## 12. 停止
```bash
cd "$SUB2API/docs" && ./stop-local.sh    # 停 executor+网关+simulator,并 docker compose down Meridian
# sub2api / Postgres / Redis 自行停止
```
手动停:
```bash
pkill -f "build/server.js"; pkill -f "target/release/private-ai-gateway"; pkill -f dstack-simulator
cd "$SUB2API" && docker compose -f deploy/meridian/compose.yaml down
```

---

## 13. 隐私边界(必须理解)
- **Meridian / `openai-compatible` = 非机密路由**:数据对 sub2api/中转方不可见(只过网关),
  但**上游(Anthropic)能看到明文**,回执 `upstream.verified` 无 attestation、不 fail-closed。
  订阅凭证的机密性,靠"Meridian 跑在 TEE enclave 内 + 凭证经 secrets 注入"来保证(本地无真 TEE)。
- 要"连模型方也看不到"(端到端机密):换**机密 provider**(tinfoil / phala-direct / near-ai / chutes / aci-dcap),
  那时 `upstream.verified` 是 `verified` 且带 attestation、验证失败即拒绝转发。
- 本地用 dstack **simulator**,只供功能测试,**不提供真实 TEE 安全**;真实安全需部署到带 TEE 的 dstack(见 `docs/production-deployment.md`)。

---

## 14. 排障对照表

| 现象 | 原因 / 处理 |
|---|---|
| `curl :3456/health` 显示 `loggedIn:false` | `secrets/seat1` 里的订阅凭证缺失/过期。重新 `claude login`,重做 §7.4,重启容器。 |
| 网关调 Meridian 连接失败 | 容器没发布 3456(用 §8.2 脚本或 §8.3 的端口覆盖),或 seed 的 `base_url` 不是 `http://127.0.0.1:3456`。 |
| docker build 失败缺 `gost` | 先 `cd $SUB2API/deploy/meridian && ./fetch-gost.sh`(§8.2 脚本自动处理)。构建沙箱无 DNS 时加 `--network=host`(见 `deploy/meridian/README.md`)。 |
| `{"message":"Unknown model: X"}` | `X` 不在 route_map 的 key 里。加精确 key 或 `claude-*` 前缀。 |
| 非 Claude 模型被吞进 Meridian | route_map 用了 `*` catch-all 指向 meridian。改用精确名或 `claude-*`。 |
| `/v1/models` 返回空 `data:[]` | route_map 里只有通配 key(`*` 类不进目录)。加**具体 key**。 |
| `Invalid URL (POST /v1/v1/...)` | 上游 `base_url` 带了 `/v1`。去掉,只写到域名/端点。 |
| `/api/control/...` 返回**网页 HTML** | consult 接口必须在 `/api/` 前缀下(本项目 `/api/control`);确认 `CONTROL_URL` 含 `/api/control`。 |
| 网关报 `dstack KMS ... No such file` | simulator 没起,或软链 `/tmp/aci-dstack-sock-dev.dstack.sock` 没指向 simulator 的 socket(§7.5)。 |
| 改了网关上游 seed 不生效 | 先 `rm -f /tmp/pag-stage2-state/upstreams.json` 再重启网关。 |
| `/consult/post` 不计费、余额不变 | `placeholder_account_id=0`;设为 `tee-gateway` 的 id 并**重启 sub2api**。 |
| ProxyLite 启用后出口 IP 仍是本机 | `PROXYLITE_SOCKS5` 没传进容器(确认 §10 的覆盖/环境变量),或 SOCKS5 端点不可达;看容器日志里 gost 是否启动。 |
| 经 ProxyLite 的请求全部超时/`route(retry=0) unexpected EOF` | `PROXYLITE_SOCKS5` 用了 **`socks5h://`** scheme。镜像内置的 **gost v3** 只认 **`socks5://`**(域名仍由 SOCKS5 端做远端解析),`socks5h://` 会静默超时。改成 `socks5://` 重启容器。 |
| `/v1/admin/upstreams` 返回 404 `admin ... not enabled` | 网关配置未设 `admin_token`。在 `/tmp/pag-stage2.config.json` 加 `"admin_token":"<随便一串>"` 重启网关,再带 `Authorization: Bearer <admin_token>` 调用(§11 多-seat 热加载需要它)。 |
| team key 打 sub2api 自身 `/v1/*` 被 403 `USE_TEE_ENDPOINT` | 设计如此:team key 只能走网关(TEE);personal key 才走 sub2api 自己的代理。 |
| 改了 sub2api 配置/代码不生效 | config.yaml 启动时读取——改完**重启 sub2api**;改了 Go 代码先 `go build -o sub2api ./cmd/server` 再重启。 |
