#!/usr/bin/env bash
# Hot-update ONE seat WITHOUT restarting it (no downtime), via Meridian's authed
# /admin endpoints. Replaces refresh-seat.sh's "update sealed env + phala cvms
# restart" (which blips ALL seats) for the common cases: new login credentials,
# and swapping/removing the ProxyLite egress proxy.
#
#   ./update-seat.sh <seat> creds
#       push secrets/<seat>/.credentials.json (+ .claude.json if present) LIVE.
#       Next SDK session uses the new account; in-flight sessions finish on old.
#   ./update-seat.sh <seat> proxy socks5://user:pass@host:port
#       route this seat's egress through a new ProxyLite account (hot).
#   ./update-seat.sh <seat> proxy off        # (also: none | direct)
#       remove the proxy — this seat's egress goes DIRECT (enclave IP).
#   ./update-seat.sh <seat> proxy status     # show current egress state
#
# Requires env: MERIDIAN_APP_ID, MERIDIAN_NODE (e.g. dstack-pha-prod5.phala.network).
# Admin token: MERIDIAN_<SEAT>_ADMIN_TOKEN from the environment, else read from
# ./seats.env. The seat must have MERIDIAN_ADMIN_TOKEN set (else /admin is 401).
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
seat="${1:?usage: update-seat.sh <seat> creds | proxy <socks5://…|off|status>}"
action="${2:?usage: update-seat.sh <seat> creds | proxy <socks5://…|off|status>}"
: "${MERIDIAN_APP_ID:?set MERIDIAN_APP_ID}"
: "${MERIDIAN_NODE:?set MERIDIAN_NODE e.g. dstack-pha-prod5.phala.network}"
command -v jq >/dev/null || { echo "error: jq required"; exit 1; }
command -v curl >/dev/null || { echo "error: curl required"; exit 1; }
UP=$(echo "$seat" | tr '[:lower:]-' '[:upper:]_')

# admin token: env var wins, else parse seats.env
tokvar="MERIDIAN_${UP}_ADMIN_TOKEN"
tok="${!tokvar:-}"
if [ -z "$tok" ] && [ -f "$DIR/seats.env" ]; then
  tok=$(grep -E "^${tokvar}=" "$DIR/seats.env" | head -1 | cut -d= -f2-)
fi
[ -n "$tok" ] || { echo "error: no $tokvar (set in env or seats.env)"; exit 1; }

# seat public URL: port = host_port_base + index within seats.json
idx=$(jq -r --arg n "$seat" '.seats | map(.name) | index($n)' "$DIR/seats.json")
[ "$idx" != "null" ] || { echo "error: seat '$seat' not found in seats.json"; exit 1; }
base=$(jq -r '.meridian_host_port_base // 3456' "$DIR/seats.json")
port=$((base + idx))
url="https://${MERIDIAN_APP_ID}-${port}.${MERIDIAN_NODE}"

post() { # path json-body
  curl -fsS -X POST "$url$1" -H "Authorization: Bearer $tok" \
    -H "Content-Type: application/json" -d "$2" | jq .
}

case "$action" in
  creds)
    cred="$DIR/secrets/$seat/.credentials.json"; cj="$DIR/secrets/$seat/.claude.json"
    [ -f "$cred" ] || { echo "error: missing $cred — first run: claude login (as $seat), then copy it here"; exit 1; }
    exp=$(jq -r '.claudeAiOauth.expiresAt // 0' "$cred")
    now_ms=$(( $(date +%s) * 1000 ))
    [ "$exp" -gt "$now_ms" ] 2>/dev/null && echo ">>> token fresh (~$(( (exp-now_ms)/3600000 ))h left)" \
      || echo ">>> WARNING: token looks expired — did you 'claude login' first?"
    if [ -f "$cj" ]; then
      payload=$(jq -n --slurpfile c "$cred" --slurpfile j "$cj" '{credentials:$c[0], claudeJson:$j[0]}')
    else
      payload=$(jq -n --slurpfile c "$cred" '{credentials:$c[0]}')
    fi
    echo ">>> POST $url/admin/credentials (hot, no restart) ..."
    post /admin/credentials "$payload"
    ;;
  proxy)
    val="${3:?usage: update-seat.sh $seat proxy <socks5://…|off|status>}"
    case "$val" in
      status)
        echo ">>> GET $url/admin/proxy ..."
        curl -fsS "$url/admin/proxy" -H "Authorization: Bearer $tok" | jq . ;;
      off|none|direct)
        echo ">>> removing proxy → direct egress (hot) ..."
        post /admin/proxy '{"socks5":null}' ;;
      *)
        case "$val" in socks5://*|socks5h://*) : ;; *) echo "error: proxy must be socks5://… | off | status"; exit 1;; esac
        echo ">>> setting proxy (hot) ..."
        post /admin/proxy "$(jq -n --arg s "$val" '{socks5:$s}')" ;;
    esac
    ;;
  *) echo "error: unknown action '$action' (creds | proxy)"; exit 1 ;;
esac
echo ">>> done — no restart, no downtime."
