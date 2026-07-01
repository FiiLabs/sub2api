#!/usr/bin/env bash
# 停止本地测试栈的 private-ai-gateway 侧(executor + 网关 + dstack simulator)。
# 不会动 sub2api / Postgres / Redis(那些你自己管理)。
set -uo pipefail

stop() { # 描述 模式
  if pkill -f "$2" 2>/dev/null; then echo "  ✓ 已停止 $1"; else echo "  - $1 未在运行"; fi
}

stop "executor"  "build/server.js"
stop "网关"       "target/release/private-ai-gateway"
stop "simulator" "dstack-simulator"

# --- 本地 Meridian seat(若曾用 MERIDIAN_LOCAL=1 起过)---
MERIDIAN_DIR="${SUB2API:-}/deploy/meridian"
MERIDIAN_OVERRIDE=/tmp/pag-meridian.ports.yml
if command -v docker >/dev/null 2>&1; then
  # 多-seat:gen-seats.sh 生成的 compose.generated.yaml(§11)
  if [ -f "$MERIDIAN_DIR/compose.generated.yaml" ]; then
    ( cd "$MERIDIAN_DIR" && docker compose -f compose.generated.yaml down ) >/dev/null 2>&1 \
      && echo "  ✓ 已停止 Meridian 多-seat(compose.generated.yaml)" || echo "  - Meridian 多-seat 未在运行"
  fi
  # 单-seat:start-local.sh 的 MERIDIAN_LOCAL 路径(compose.yaml [+ 端口覆盖])
  if [ -f "$MERIDIAN_DIR/compose.yaml" ]; then
    if [ -f "$MERIDIAN_OVERRIDE" ]; then
      ( cd "$MERIDIAN_DIR" && docker compose -f compose.yaml -f "$MERIDIAN_OVERRIDE" down ) >/dev/null 2>&1 \
        && echo "  ✓ 已停止 Meridian seat" || echo "  - Meridian seat 未在运行"
    else
      ( cd "$MERIDIAN_DIR" && docker compose down ) >/dev/null 2>&1 \
        && echo "  ✓ 已停止 Meridian seat" || echo "  - Meridian seat 未在运行"
    fi
  fi
fi

echo "完成(sub2api / Postgres / Redis 未受影响)。"
