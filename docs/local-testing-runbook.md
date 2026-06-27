# 本地连接 private-ai-gateway 测试 — 完整手册

本手册带你从零在本地把整套环境跑起来:用一个 API key + curl(或 Claude Code)发请求,经过"隐私网关"转发到上游大模型,并验证鉴权、路由、回执、计费。即使你之前没接触过这些组件,按顺序照做即可。

---

## 1. 这是什么 / 各组件的角色

我们要本地启动 4 个程序,把它们串成一条链路:

```
你(curl / Claude Code)
   └─► private-ai-gateway(网关,:8086)──► executor(中间件)──┬─► sub2api(:8080,做鉴权/路由/计费决策)
                                                              └─► 上游大模型(如 Claude API)
```

| 组件 | 通俗解释 | 仓库 |
|---|---|---|
| **private-ai-gateway** | "隐私网关"。本应运行在 TEE(可信执行环境)里,对外提供 OpenAI/Anthropic 兼容接口,转发前验证上游、并给每次请求签一张"回执"。 | `https://github.com/Dstack-TEE/private-ai-gateway.git` |
| **executor** | 网关自带的中间件进程(在 private-ai-gateway 仓库的 `middleware/executor` 里),负责把网关和 sub2api 连起来,并做请求格式转换(OpenAI ↔ Anthropic)。 | (同上,子目录) |
| **sub2api** | "控制面":管理 API key、决定某个 key 能用哪些模型/路由、记录用量计费。**只接触请求的元数据**(key 的哈希、模型名、token 数),不接触你的 prompt 内容。 | `https://github.com/FiiLabs/sub2api.git` |
| **dstack simulator** | 本地模拟 TEE 的密钥服务(KMS/quote),让网关在**没有真 TEE 硬件**时也能启动。⚠️ 仅供功能测试,不提供真实安全保证。 | `https://github.com/Dstack-TEE/dstack.git`(simulator 在 `sdk/simulator`) |

几个会反复出现的名词:
- **TEE**:可信执行环境,代码在加密隔离区里跑,外部(包括服务器运营者)看不到内存里的数据。本地用 simulator 模拟。
- **控制面 / 数据面**:控制面 = 鉴权/路由/计费(只传元数据);数据面 = 真正的 prompt/响应(只经过网关,不经过 sub2api)。
- **team key**:带"团队"归属的 API key,设计上**只能走网关(TEE)这条路**;普通(personal)key 走 sub2api 自己的代理。本手册测的是 team key。
- **route_map**:sub2api 里的一张表,把"客户端发来的模型名"映射到"网关的某条上游路由"。
- **receipt(回执)**:网关为每次请求签名的记录,可事后核验。

---

## 2. 前置条件(需要先装好的工具)

- `git`
- **Rust**(stable;构建 private-ai-gateway 和 dstack simulator 用)—— 装:`curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh`
- **Go ≥ 1.23**(构建 sub2api 用)
- **Node ≥ 18 + npm**(构建/运行 executor 用)
- **Docker**(本手册用它跑 `psql` 查数据库,免装 psql)
- **PostgreSQL + Redis**(sub2api 的依赖,需在本地可连)
- `openssl`、`curl`(生成 token、发测试请求)

---

## 3. 获取代码 + 设路径变量

把三个仓库 clone 到你喜欢的目录,然后用环境变量记住它们的位置(本手册后续都用这些变量,不写死路径):

```bash
# 选一个工作目录,例如:
mkdir -p ~/work && cd ~/work

git clone https://github.com/Dstack-TEE/private-ai-gateway.git
git clone https://github.com/FiiLabs/sub2api.git
git clone https://github.com/Dstack-TEE/dstack.git

# 设变量(指向你刚 clone 的位置)
export PAG=~/work/private-ai-gateway          # 网关 + executor
export SUB2API=~/work/sub2api                 # 控制面(后端在 $SUB2API/backend)
export DSTACK_SIM=~/work/dstack/sdk/simulator # dstack simulator 目录
```
> 提示:这些 `export` 只在当前终端有效。开新终端要重新 export(或写进 `~/.bashrc`)。

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

---

## 5. 关键概念:三个 model 名(最容易踩坑,务必理解)

一次请求里"模型名"其实有三个,分属不同环节,必须对应起来:

| 名字 | 在哪里设置 | 例子 |
|---|---|---|
| **① 客户端 model**(= sub2api `route_map` 的 key) | 你发请求的 `model` 字段 + sub2api 配置 | `claude-sonnet-4-6` |
| **② routeId** = `<网关upstream名>:<公开模型id>` | route_map 的 `route_id`,要和网关 upstreams 对上 | `claude:sonnet-4-6` |
| **③ 上游 model**(= 网关 upstreams 里 `models` 的 value) | 网关的上游配置 | `claude-sonnet-4-6` |

流程:你发 **①** → sub2api 用 route_map 把 ① 映射成 **②** → 网关按 ② 找到上游,并把"公开模型id"改写成 **③** 发给上游。三者对不上就会报 404 或路由失败。

---

## 6. 一次性准备(token / team key / 占位 account)

### 6.1 control token(executor 和 sub2api 共用同一个)
```bash
openssl rand -hex 32 | tee /tmp/pag-control-token.txt
```

### 6.2 team key
在 sub2api 里创建一个**带 team 的 API key**(通过 sub2api 的后台/接口创建;新建的 key 会自动写入用于查找的哈希)。记下这个 key 明文,测试时作为 `Authorization: Bearer` 用。

### 6.3 占位 account id
sub2api **首次启动时会自动跑数据库迁移**,其中会建一个名为 `tee-gateway` 的占位账号(供计费用)。查它的 id:
```bash
# <db用户>/<db密码>/<dbname> 取自 sub2api 的 backend/config.yaml 的 database 段
docker run --rm --network host -e PGPASSWORD=<db密码> postgres:16-alpine \
  psql -h 127.0.0.1 -U <db用户> -d <dbname> -tAc \
  "SELECT id FROM accounts WHERE name='tee-gateway';"
```
记下这个 id,填进下面的 `placeholder_account_id`(填 0 也能跑通推理,只是不记计费)。

---

## 7. 配置文件

### 7.1 sub2api:在 `$SUB2API/backend/config.yaml` 里加 `consult` 段
```yaml
consult:
  control_token: "<与 /tmp/pag-control-token.txt 里的值一致>"
  placeholder_account_id: <第 6.3 步查到的 id>     # 0 = 不计费(数据面仍通)
  route_map:
    # key = 客户端发的模型名;route_id = <网关upstream名>:<公开模型id>;format = openai 或 anthropic
    claude-sonnet-4-6: { route_id: "claude:sonnet-4-6", format: "openai" }
    claude-opus-4-6:   { route_id: "claude:opus-4-6",   format: "openai" }
    # 通配:覆盖同一系列的多个别名(匹配优先级:精确 key > 最长前缀 xxx-* > 兜底 *)
    "claude-sonnet*":  { route_id: "claude:sonnet-4-6", format: "openai" }
    "claude-opus*":    { route_id: "claude:opus-4-6",   format: "openai" }
```
> `/api/control/models`(模型目录)只列**具体 key**,不列通配/兜底 key。每条带 `id/object/created/owned_by`。

### 7.2 网关:新建 `/tmp/pag-stage2.config.json`
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

### 7.3 网关上游:新建 `/tmp/pag-stage2-upstreams.seed.json`
```json
[
  {
    "name": "claude",
    "provider": "openai-compatible",
    "base_url": "https://你的上游域名",
    "models": { "sonnet-4-6": "claude-sonnet-4-6", "opus-4-6": "claude-opus-4-6" },
    "bearer_token": "<上游 API key>"
  }
]
```
> - `base_url` **只写到域名,不要带 `/v1`**(网关会自动补 `/v1/chat/completions` 等)。
> - 这里的 `name`(claude)+ `models` 的 key(sonnet-4-6)拼成 routeId `claude:sonnet-4-6`,要和 7.1 的 `route_id` 完全一致。
> - 改了这个文件后,要 `rm -f /tmp/pag-stage2-state/upstreams.json` 再重启网关才会重新加载(seed 只在网关首次、active 配置为空时生效)。

### 7.4 dstack simulator 的 socket 软链
网关配置里写的是 `/tmp/aci-dstack-sock-dev.dstack.sock`,把它指向 simulator 真实生成的 socket:
```bash
ln -sf "$DSTACK_SIM/dstack.sock" /tmp/aci-dstack-sock-dev.dstack.sock
```
(simulator 启动后会在 `$DSTACK_SIM/dstack.sock` 创建该文件。)

---

## 8. 启动顺序(开 4~5 个终端,每个先 `export` 第 3 步的变量)

```bash
# ① 确保 Postgres / Redis 在跑(你的方式)

# ② sub2api(终端1)—— 首次启动会自动建表/迁移
cd "$SUB2API/backend" && ./sub2api

# ③ dstack simulator(终端2)
cd "$DSTACK_SIM" && ./dstack-simulator

# ④ 网关(终端3)
cd "$PAG"
PRIVATE_AI_GATEWAY_CONFIG_PATH=/tmp/pag-stage2.config.json ./target/release/private-ai-gateway

# ⑤ executor(终端4)
cd "$PAG/middleware/executor"
PRIVATE_AI_GATEWAY_EXECUTOR_UDS_PATH=/tmp/pag-stage2/executor.sock \
PRIVATE_AI_GATEWAY_BACKEND_UDS_PATH=/tmp/pag-stage2/backend.sock \
PRIVATE_AI_GATEWAY_CONTROL_URL=http://127.0.0.1:8080/api/control \
PRIVATE_AI_GATEWAY_CONTROL_TOKEN="$(cat /tmp/pag-control-token.txt)" \
node build/server.js
```
顺序要点:**simulator 要在网关之前起**(网关启动要连它的 socket);**executor 在网关之后起**(两者共享 `/tmp/pag-stage2/` 下的 socket 文件)。
各进程启动成功的标志:sub2api 监听 8080;simulator 打印 "simulator";网关日志出现 `private-ai-gateway listening bind=127.0.0.1:8086`;executor 打印 `executor listening on unix socket ...`。

---

### 8.1 一键启停脚本(可选,省去手动开终端)

手册同目录提供了两个脚本,**只管理 private-ai-gateway 侧的三个进程**(simulator + 网关 + executor);sub2api、Postgres、Redis 仍由你自己启动。

```bash
# 先确保:已 export PAG / DSTACK_SIM(第 3 步)、各程序已构建(第 4 步)、sub2api+PG+Redis 已在跑
cd "$SUB2API/docs"           # 脚本所在目录
./start-local.sh             # 自动:生成 token/配置/软链 → 起 simulator→网关→executor,并等待就绪
./stop-local.sh              # 停掉这三个进程
```
`start-local.sh` 会:
- 缺少 `/tmp/pag-control-token.txt` 时自动生成(并打印出来,供你填进 sub2api 的 `consult.control_token`);
- 缺少网关 config / 上游 seed 时生成模板(seed 是模板时会提示你填上游域名和 key);
- 软链好 dstack socket,按 simulator→网关→executor 顺序启动并做就绪检查;
- 日志写在 `/tmp/pag-local-logs/`。

> 首次用脚本生成了上游 seed 模板后,记得填好上游 `base_url`/`bearer_token`,再 `rm -f /tmp/pag-stage2-state/upstreams.json` 并重跑 `./start-local.sh`。

---

## 9. 验证 / 测试

```bash
KEY=<你的 team key>                       # 第 6.2 步创建的
TOK=$(cat /tmp/pag-control-token.txt)
```

### 9.1 控制面连通(排障用)
```bash
# 直连 sub2api 控制面,应返回模型目录 JSON
curl -sS http://127.0.0.1:8080/api/control/models -H "Authorization: Bearer $TOK"
# 经网关+executor 的目录(客户端无需带 token,executor 内部会附加)
curl -sS http://127.0.0.1:8086/v1/models
```

### 9.2 OpenAI 风格推理
```bash
curl -sS http://127.0.0.1:8086/v1/chat/completions -H "Authorization: Bearer $KEY" \
  -H 'content-type: application/json' \
  -d '{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"你好"}]}'
```

### 9.3 Claude Code 风格(Anthropic 接口,含流式)
```bash
# 非流式
curl -sS http://127.0.0.1:8086/v1/messages -H "Authorization: Bearer $KEY" \
  -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \
  -d '{"model":"claude-sonnet-4-6","max_tokens":200,"messages":[{"role":"user","content":"你好"}]}'
# 流式(应看到 Anthropic 的 SSE 事件:message_start / content_block_delta / ...)
curl -sS -N http://127.0.0.1:8086/v1/messages -H "Authorization: Bearer $KEY" \
  -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \
  -d '{"model":"claude-sonnet-4-6","max_tokens":200,"stream":true,"messages":[{"role":"user","content":"你好"}]}'
```

### 9.4 用真实 Claude Code
```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8086
export ANTHROPIC_AUTH_TOKEN=$KEY
claude     # 启动后所有请求都走网关
```
> Claude Code 发出的模型名必须在 route_map 里能匹配(精确或通配),否则报 404 "Unknown model"。

### 9.5 查回执(receipt)
响应头里有 `x-receipt-id`。回执是"owned"的,查询要带同一个 key:
```bash
RID=<响应头里的 x-receipt-id>
curl -sS http://127.0.0.1:8086/v1/aci/receipts/$RID -H "Authorization: Bearer $KEY" | python3 -m json.tool
```
关注事件链:`route.selected`(选中的 routeId)、`upstream.verified`、`response.returned`。

### 9.6 验证计费(需 `placeholder_account_id` ≠ 0)
team key 的费用记到**团队账本(billing_subject)**,不扣个人余额:
```bash
docker run --rm --network host -e PGPASSWORD=<db密码> postgres:16-alpine \
  psql -h 127.0.0.1 -U <db用户> -d <dbname> -tAc \
  "SELECT id,type,balance FROM billing_subjects WHERE type='team';"
# 发一次请求,前后对比该 balance 应减少;usage_logs 表应 +1 行
```

---

## 10. 停止
```bash
pkill -f "build/server.js"                       # executor
pkill -f "target/release/private-ai-gateway"     # 网关
pkill -f dstack-simulator                        # simulator
# sub2api / Postgres / Redis 自行停止
```

---

## 11. 排障对照表(常见问题)

| 现象 | 原因 / 处理 |
|---|---|
| `/api/control/...` 返回**网页 HTML** | consult 接口必须在 `/api/` 前缀下(本项目用 `/api/control`);否则被前端页面拦截。确认 `CONTROL_URL` 含 `/api/control`。 |
| `{"message":"Unknown model: X"}` | 你发的模型名 `X` 不在 route_map 的 key 里。加一条精确 key,或用通配 `前缀-*`。 |
| `/v1/models` 返回空 `data:[]` | route_map 里只有通配 key(`*` 类不进目录)。加**具体 key** 才会列出。 |
| `Invalid URL (POST /v1/v1/...)` | 上游 `base_url` 带了 `/v1`。去掉,只写到域名。 |
| 网关报 `dstack KMS ... No such file` | simulator 没启动,或软链 `/tmp/aci-dstack-sock-dev.dstack.sock` 没指向 simulator 的 socket(见 7.4)。 |
| 改了网关上游 seed 不生效 | 先 `rm -f /tmp/pag-stage2-state/upstreams.json` 再重启网关。 |
| `/consult/post` 不计费、余额不变 | `placeholder_account_id=0`;设为 `tee-gateway` 的 account id 并重启 sub2api。 |
| team key 打 sub2api 自身 `/v1/*` 被 403 `USE_TEE_ENDPOINT` | 设计如此:team key 只能走网关(TEE);personal key 才走 sub2api 自己的代理。 |
| 改了 sub2api 配置/代码不生效 | config.yaml 在启动时读取——改完要**重启 sub2api**;改了 Go 代码要先 `go build -o sub2api ./cmd/server` 再重启。 |

---

## 12. 一个重要认知:隐私边界
- 走 `openai-compatible` 这类普通上游 = **非机密路由**:数据**对 sub2api/中转方不可见**(只经过网关),但**上游本身能看到明文**,回执里的 `upstream.verified` 没有 attestation、也不会 fail-closed。
- 若要"连模型方也看不到"(端到端机密),需换成**机密 provider**(如 tinfoil / phala-direct / near-ai / chutes / aci-dcap),那时 `upstream.verified` 会是 `verified` 且带 attestation、并在验证失败时拒绝转发。
- 另外:本地用的是 dstack **simulator**,只供功能测试,**不提供真实 TEE 安全**;真实安全需部署到带 TEE 的 dstack 环境。
