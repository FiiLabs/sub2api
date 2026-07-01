#!/usr/bin/env bash
# 一键启动本地测试栈的 private-ai-gateway 侧:dstack simulator + 网关 + executor。
# 前提:① 已 export PAG 和 DSTACK_SIM;② 各程序已构建;③ sub2api + Postgres/Redis 已自行启动。
#
#   export PAG=<private-ai-gateway 仓库根目录>
#   export DSTACK_SIM=<dstack 仓库>/sdk/simulator
#   ./start-local.sh
#
# 可选:也起一个本地 Meridian seat(Claude 订阅路由),需 Docker + 已 claude login:
#   export SUB2API=<sub2api 仓库根目录>
#   MERIDIAN_LOCAL=1 MERIDIAN_COPY_CREDS=1 ./start-local.sh
# 再可选:该 seat 经 ProxyLite 固定出口 IP 出网(防封),额外设:
#   PROXYLITE_SOCKS5="socks5://<user>:<pass>@<host>:<port>" MERIDIAN_LOCAL=1 ./start-local.sh
set -euo pipefail

: "${PAG:?请先 export PAG=<private-ai-gateway 仓库根目录>}"
: "${DSTACK_SIM:?请先 export DSTACK_SIM=<dstack 仓库>/sdk/simulator}"

GW_BIN="$PAG/target/release/private-ai-gateway"
EXEC_JS="$PAG/middleware/executor/build/server.js"
SIM_BIN="$DSTACK_SIM/dstack-simulator"
LOG=/tmp/pag-local-logs
SOCK_DIR=/tmp/pag-stage2
STATE_DIR=/tmp/pag-stage2-state
CFG=/tmp/pag-stage2.config.json
SEED=/tmp/pag-stage2-upstreams.seed.json
TOKEN_FILE=/tmp/pag-control-token.txt
DSTACK_LINK=/tmp/aci-dstack-sock-dev.dstack.sock

# --- Meridian(Claude 订阅路由)可选本地 seat ---
# MERIDIAN_LOCAL=1 时,额外用 docker compose 起一个本地 Meridian seat(发布 127.0.0.1:3456),
# 并把 meridian-seat1 写进网关上游 seed。需:Docker + 一个 Claude 订阅(claude login 产出的凭证)。
# MERIDIAN_COPY_CREDS=1 时,缺凭证会自动从 ~/.claude/ 拷贝到 secrets/seat1/。
MERIDIAN_LOCAL="${MERIDIAN_LOCAL:-0}"
MERIDIAN_COPY_CREDS="${MERIDIAN_COPY_CREDS:-0}"
MERIDIAN_DIR="${SUB2API:-}/deploy/meridian"
MERIDIAN_PORT=3456
MERIDIAN_BEARER=devsecret          # 本地网关调 Meridian 用的 token(= 容器内 MERIDIAN_API_KEY)
MERIDIAN_OVERRIDE=/tmp/pag-meridian.ports.yml   # 为本地测试发布端口 + 设 API key 的 compose 覆盖

# --- 检查构建产物 ---
for f in "$GW_BIN" "$EXEC_JS" "$SIM_BIN"; do
  [ -x "$f" ] || [ -f "$f" ] || { echo "缺少 $f —— 请先按手册第 4 步构建"; exit 1; }
done
command -v node >/dev/null || { echo "未找到 node"; exit 1; }

mkdir -p "$LOG" "$SOCK_DIR" "$STATE_DIR"

# --- control token(没有就生成)---
[ -f "$TOKEN_FILE" ] || openssl rand -hex 32 > "$TOKEN_FILE"
TOK="$(cat "$TOKEN_FILE")"

# --- 网关配置(没有就生成)---
[ -f "$CFG" ] || cat > "$CFG" <<EOF
{
  "bind": "127.0.0.1:8086",
  "state_dir": "$STATE_DIR",
  "upstream_config_seed_path": "$SEED",
  "dstack_endpoint": "unix:$DSTACK_LINK",
  "executor": { "uds_path": "$SOCK_DIR/executor.sock", "backend_uds_path": "$SOCK_DIR/backend.sock" }
}
EOF

# --- 上游 seed(没有就生成模板并提示填写)---
if [ ! -f "$SEED" ]; then
  if [ "$MERIDIAN_LOCAL" = "1" ]; then
    # 同时写入 claude 模板 + 本地 meridian-seat1(指向 docker 发布的 127.0.0.1:3456)
    cat > "$SEED" <<EOF
[
  {
    "name": "claude",
    "provider": "openai-compatible",
    "base_url": "https://YOUR-UPSTREAM-DOMAIN",
    "models": { "sonnet-4-6": "claude-sonnet-4-6", "opus-4-6": "claude-opus-4-6" },
    "bearer_token": "YOUR-UPSTREAM-KEY"
  },
  {
    "name": "meridian-seat1",
    "provider": "openai-compatible",
    "base_url": "http://127.0.0.1:$MERIDIAN_PORT",
    "path": "/v1/messages",
    "models": { "claude-sonnet-4-6": "claude-sonnet-4-6", "claude-opus-4-6": "claude-opus-4-6" },
    "bearer_token": "$MERIDIAN_BEARER"
  }
]
EOF
    echo "⚠️  已生成上游模板 $SEED(含 meridian-seat1)—— claude 那条按需填/删;填好后 rm -f $STATE_DIR/upstreams.json 再重跑"
  else
    cat > "$SEED" <<'EOF'
[
  {
    "name": "claude",
    "provider": "openai-compatible",
    "base_url": "https://YOUR-UPSTREAM-DOMAIN",
    "models": { "sonnet-4-6": "claude-sonnet-4-6", "opus-4-6": "claude-opus-4-6" },
    "bearer_token": "YOUR-UPSTREAM-KEY"
  }
]
EOF
    echo "⚠️  已生成上游模板 $SEED —— 请填好上游域名/key 后,执行:rm -f $STATE_DIR/upstreams.json 再重跑本脚本"
  fi
elif [ "$MERIDIAN_LOCAL" = "1" ] && ! grep -q '"meridian-seat1"' "$SEED"; then
  echo "⚠️  seed $SEED 已存在但无 meridian-seat1 —— 请手动加一条(见 deploy/meridian/gateway-upstreams.example.json,"
  echo "    本地 base_url 用 http://127.0.0.1:$MERIDIAN_PORT),然后 rm -f $STATE_DIR/upstreams.json 再重跑。"
fi

# --- dstack socket 软链 ---
ln -sf "$DSTACK_SIM/dstack.sock" "$DSTACK_LINK"

# --- 先停掉本脚本管理的旧进程(不动 sub2api/PG/Redis)---
pkill -f "build/server.js" 2>/dev/null || true
pkill -f "target/release/private-ai-gateway" 2>/dev/null || true
pkill -f "dstack-simulator" 2>/dev/null || true
sleep 1

wait_for() { # 描述 命令... ;轮询最多 ~15s
  local desc=$1; shift
  for _ in $(seq 1 30); do "$@" >/dev/null 2>&1 && { echo "  ✓ $desc"; return 0; }; sleep 0.5; done
  echo "  ✗ 等待超时:$desc(看日志 $LOG/)"; return 1
}

echo "① 启动 dstack simulator ..."
( cd "$DSTACK_SIM" && nohup ./dstack-simulator >"$LOG/simulator.log" 2>&1 & )
wait_for "simulator socket" test -S "$DSTACK_SIM/dstack.sock"

echo "② 启动网关 :8086 ..."
PRIVATE_AI_GATEWAY_CONFIG_PATH="$CFG" nohup "$GW_BIN" >"$LOG/gateway.log" 2>&1 &
wait_for "网关 /health" curl -sf http://127.0.0.1:8086/health

echo "③ 启动 executor ..."
PRIVATE_AI_GATEWAY_EXECUTOR_UDS_PATH="$SOCK_DIR/executor.sock" \
PRIVATE_AI_GATEWAY_BACKEND_UDS_PATH="$SOCK_DIR/backend.sock" \
PRIVATE_AI_GATEWAY_CONTROL_URL="http://127.0.0.1:8080/api/control" \
PRIVATE_AI_GATEWAY_CONTROL_TOKEN="$TOK" \
nohup node "$EXEC_JS" >"$LOG/executor.log" 2>&1 &
wait_for "executor socket" test -S "$SOCK_DIR/executor.sock"

# --- ④(可选)启动本地 Meridian seat ---
if [ "$MERIDIAN_LOCAL" = "1" ]; then
  echo "④ 启动本地 Meridian seat(docker)..."
  command -v docker >/dev/null || { echo "  ✗ 未找到 docker,MERIDIAN_LOCAL 需要 Docker"; exit 1; }
  : "${SUB2API:?MERIDIAN_LOCAL=1 需先 export SUB2API=<sub2api 仓库根目录>}"
  [ -f "$MERIDIAN_DIR/compose.yaml" ] || { echo "  ✗ 找不到 $MERIDIAN_DIR/compose.yaml"; exit 1; }

  # 订阅凭证:secrets/seat1/ 需有 .credentials.json 与 .claude.json(claude login 产出)
  CREDS="$MERIDIAN_DIR/secrets/seat1"
  if [ ! -f "$CREDS/.credentials.json" ] || [ ! -f "$CREDS/.claude.json" ]; then
    if [ "$MERIDIAN_COPY_CREDS" = "1" ] && [ -f "$HOME/.claude/.credentials.json" ] && [ -f "$HOME/.claude.json" ]; then
      mkdir -p "$CREDS"
      cp "$HOME/.claude/.credentials.json" "$CREDS/"
      cp "$HOME/.claude.json"              "$CREDS/"
      echo "  ✓ 已从 ~/.claude 拷贝订阅凭证到 $CREDS"
    else
      echo "  ✗ 缺少订阅凭证 $CREDS/{.credentials.json,.claude.json}"
      echo "    先 claude login,再设 MERIDIAN_COPY_CREDS=1 重跑,或手动拷贝(见 deploy/meridian/README.md)"
      exit 1
    fi
  fi

  # Dockerfile 无条件 COPY gost —— 缺则先拉取到构建上下文
  [ -f "$MERIDIAN_DIR/gost" ] || { echo "  · 拉取 gost ..."; ( cd "$MERIDIAN_DIR" && ./fetch-gost.sh ); }

  # compose 覆盖:本地发布端口 + 设 API key(= 网关 seed 里 meridian-seat1 的 bearer_token);
  # 若设了 PROXYLITE_SOCKS5,一并透传给容器(entrypoint.sh 会起 gost SOCKS5→HTTP shim,固定出口 IP)。
  {
    echo "services:"
    echo "  meridian-seat1:"
    echo "    ports:"
    echo "      - \"127.0.0.1:$MERIDIAN_PORT:3456\""
    echo "    environment:"
    echo "      MERIDIAN_API_KEY: \"$MERIDIAN_BEARER\""
    [ -n "${PROXYLITE_SOCKS5:-}" ] && echo "      PROXYLITE_SOCKS5: \"$PROXYLITE_SOCKS5\""
  } > "$MERIDIAN_OVERRIDE"

  ( cd "$MERIDIAN_DIR" && docker compose -f compose.yaml -f "$MERIDIAN_OVERRIDE" up -d --build ) \
    || { echo "  ✗ docker compose 启动失败(构建沙箱无 DNS 时,见 deploy/meridian/README.md 的 --network=host)"; exit 1; }
  wait_for "Meridian /health" curl -sf "http://127.0.0.1:$MERIDIAN_PORT/health"
fi

cat <<EOF

✅ private-ai-gateway 侧已启动。日志目录:$LOG/
   - 网关:        http://127.0.0.1:8086
   - control token(填到 sub2api config.yaml 的 consult.control_token):
       $TOK

提醒:
  • 确保 sub2api(:8080)已运行,且 config.yaml 的 consult.control_token 与上面一致、
    route_map 的 route_id 与 $SEED 里的 <name>:<模型key> 对应。
  • 验证:  curl -sS http://127.0.0.1:8086/v1/models
  • 停止:  ./stop-local.sh
EOF

if [ "$MERIDIAN_LOCAL" = "1" ]; then
  cat <<EOF

Meridian(Claude 订阅路由)已一并启动:
   - Meridian seat1: http://127.0.0.1:$MERIDIAN_PORT   (健康检查: /health)
   - 已在网关 seed 注册 meridian-seat1(bearer_token=$MERIDIAN_BEARER)
  • 在 sub2api config.yaml 的 consult.route_map 加 Claude→Meridian(见 deploy/config.example.yaml):
      claude-opus-4-6:   { route_id: "meridian-seat1:claude-opus-4-6",   format: "anthropic" }
      claude-sonnet-4-6: { route_id: "meridian-seat1:claude-sonnet-4-6", format: "anthropic" }
  • 测试(team key):
      curl -sS http://127.0.0.1:8086/v1/messages -H "Authorization: Bearer <team key>" \\
        -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \\
        -d '{"model":"claude-opus-4-6","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'
EOF
fi
