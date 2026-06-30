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

echo "完成(sub2api / Postgres / Redis 未受影响)。"
