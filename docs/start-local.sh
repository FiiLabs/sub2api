#!/usr/bin/env bash
# 一键启动本地测试栈的 private-ai-gateway 侧:dstack simulator + 网关 + executor。
# 前提:① 已 export PAG 和 DSTACK_SIM;② 各程序已构建;③ sub2api + Postgres/Redis 已自行启动。
#
#   export PAG=<private-ai-gateway 仓库根目录>
#   export DSTACK_SIM=<dstack 仓库>/sdk/simulator
#   ./start-local.sh
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
