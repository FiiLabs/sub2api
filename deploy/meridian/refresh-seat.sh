#!/usr/bin/env bash
# Re-inject one seat's OAuth credentials after a re-login. Use when check-seats.sh
# reports TOKEN_EXPIRED for a seat.
#
# PREFER ./update-seat.sh <seat> creds — it pushes the same credentials LIVE via
# Meridian's authed /admin/credentials endpoint with NO restart and NO downtime.
# Use THIS script when you also want the sealed env updated (so a future cold
# boot of the CVM starts from the new creds), or when the seat's admin surface
# is not enabled (MERIDIAN_<SEAT>_ADMIN_TOKEN unset). Best practice: run
# update-seat.sh first (instant, no blip), then this one to persist to env.
#
#   1) claude login                       # log in as THAT seat's subscription account
#   2) cp ~/.claude/.credentials.json deploy/meridian/secrets/<seat>/.credentials.json
#      jq '{oauthAccount}' ~/.claude.json > deploy/meridian/secrets/<seat>/.claude.json
#   3) ./refresh-seat.sh <seat> <meridian-cvm-id>
#
# Updates that seat's sealed env on the CVM and restarts it. entrypoint's
# "newest wins" logic then adopts the fresher creds (higher expiresAt) from env.
# NOTE: restart blips ALL seats on that CVM briefly (persistent-volume creds survive).
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
seat="${1:?usage: refresh-seat.sh <seat> <meridian-cvm-id>}"
cvm="${2:?usage: refresh-seat.sh <seat> <meridian-cvm-id>}"
UP=$(echo "$seat" | tr '[:lower:]-' '[:upper:]_')
cred="$DIR/secrets/$seat/.credentials.json"
cj="$DIR/secrets/$seat/.claude.json"
command -v jq >/dev/null || { echo "error: jq required"; exit 1; }
[ -f "$cred" ] && [ -f "$cj" ] || {
  echo "error: missing secrets/$seat/ files. First run:"
  echo "  claude login   # as $seat's account"
  echo "  cp ~/.claude/.credentials.json $cred"
  echo "  jq '{oauthAccount}' ~/.claude.json > $cj"
  exit 1; }

# freshness sanity: warn (don't block) if the copied token is already expired
exp=$(jq -r '.claudeAiOauth.expiresAt // 0' "$cred")
python3 - "$exp" <<'PY' || true
import sys,time
e=float(sys.argv[1])/1000
if e>time.time(): print(">>> token expiresAt in %.1fh (fresh)"%((e-time.time())/3600))
else: print(">>> WARNING: token already expired %.1fh ago — did you `claude login` first?"%((time.time()-e)/3600))
PY

env="$(mktemp)"
{
  printf 'MERIDIAN_%s_CREDENTIALS=%s\n' "$UP" "$(jq -c . "$cred")"
  printf 'MERIDIAN_%s_CLAUDE_JSON=%s\n' "$UP" "$(jq -c . "$cj")"
} > "$env"
echo ">>> updating sealed env for $seat on $cvm ..."
phala envs update "$cvm" -e "$env"
shred -u "$env" 2>/dev/null || rm -f "$env"
echo ">>> restarting CVM (all seats on it blip briefly) ..."
phala cvms restart "$cvm"
echo ">>> done. verify with: ./check-seats.sh"
