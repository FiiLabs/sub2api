#!/bin/sh
set -e

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
