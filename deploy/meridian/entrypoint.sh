#!/bin/sh
set -e

# OAuth subscription state provisioning. Two mutually-compatible sources:
#   1. Bind-mount (local dev): compose mounts secrets/seatN/* into place.
#   2. Env injection (dstack/Phala): CLAUDE_CREDENTIALS_JSON / CLAUDE_JSON hold
#      the file *contents*, delivered via dstack ENCRYPTED SECRETS (never via a
#      measured `configs.content`, which would leak the token into compose_hash).
# When the env vars are set we materialize them to the writable container fs so
# Meridian can persist its ~8h token refresh (no :ro mount needed in prod).
if [ -n "${CLAUDE_CREDENTIALS_JSON:-}" ]; then
  echo "[entrypoint] materializing /root/.claude/.credentials.json from env"
  mkdir -p /root/.claude
  printf '%s' "$CLAUDE_CREDENTIALS_JSON" > /root/.claude/.credentials.json
  chmod 600 /root/.claude/.credentials.json
fi
if [ -n "${CLAUDE_JSON:-}" ]; then
  echo "[entrypoint] materializing /root/.claude.json from env"
  printf '%s' "$CLAUDE_JSON" > /root/.claude.json
fi

# Optional anti-ban egress: ProxyLite is a SOCKS5 proxy, but Meridian /
# Claude Code SDK honor HTTP proxy env (HTTPS_PROXY) only. When PROXYLITE_SOCKS5
# is set (socks5://user:pass@host:port), run a local gost SOCKS5->HTTP shim and
# point Meridian at it. Unset = direct egress (enclave's own IP).
if [ -n "$PROXYLITE_SOCKS5" ]; then
  echo "[entrypoint] starting gost SOCKS5->HTTP shim on 127.0.0.1:8118"
  gost -L "http://127.0.0.1:8118" -F "$PROXYLITE_SOCKS5" &
  export HTTP_PROXY="http://127.0.0.1:8118"
  export HTTPS_PROXY="http://127.0.0.1:8118"
  export NO_PROXY="127.0.0.1,localhost"
fi

exec meridian
