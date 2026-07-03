#!/bin/sh
set -e

# OAuth subscription state provisioning. Two mutually-compatible sources:
#   1. Bind-mount (local dev): compose mounts secrets/seatN/* into place.
#   2. Env injection (dstack/Phala): CLAUDE_CREDENTIALS_JSON / CLAUDE_JSON hold
#      the file *contents*, delivered via dstack sealed env (never via a measured
#      `configs.content`, which would leak the token into compose_hash).
#
# CREDENTIALS: "newest wins". Meridian auto-refreshes the ~8h OAuth token and
# writes it back to /root/.claude/.credentials.json. In production that path is a
# PERSISTENT VOLUME (see compose.cvm.yaml), so a refreshed token survives restarts.
# On boot we must NOT blindly overwrite that persisted (possibly refreshed) copy
# with the older env copy — but we SHOULD adopt the env copy when it is fresher
# (first boot, or an operator re-injected new creds after the volume went stale).
# So we compare claudeAiOauth.expiresAt and keep whichever is newer.
if [ -n "${CLAUDE_CREDENTIALS_JSON:-}" ]; then
  mkdir -p /root/.claude
  DEST=/root/.claude/.credentials.json
  WRITE_ENV=1
  if [ -f "$DEST" ] && node -e '
      const fs=require("fs");
      const exp=(o)=>{try{return JSON.parse(o).claudeAiOauth.expiresAt||0}catch(e){return 0}};
      const disk=exp(fs.readFileSync(process.argv[1],"utf8"));
      const env=exp(process.env.CLAUDE_CREDENTIALS_JSON);
      process.exit(disk>=env?0:1);   // 0 = disk is newer/equal -> keep disk
    ' "$DEST" 2>/dev/null; then
    WRITE_ENV=0
    echo "[entrypoint] keeping persisted credentials (>= env freshness)"
  fi
  if [ "$WRITE_ENV" = "1" ]; then
    echo "[entrypoint] writing credentials from env (first boot or fresher than disk)"
    printf '%s' "$CLAUDE_CREDENTIALS_JSON" > "$DEST"
    chmod 600 "$DEST"
  fi
fi
# .claude.json is static account metadata (oauthAccount), not refreshed — always
# materialize from env when provided.
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
