#!/usr/bin/env bash
# Fleet health check for all Meridian seats. Probes each seat with a 1-token
# inference request (the ONLY reliable signal — /health's loggedIn is true even
# when the token has expired) and classifies token / proxy / ban status. Emails
# on failures. Exit 1 if any seat needs attention (so cron MAILTO also works).
#
#   ./check-seats.sh [seats.json]
#
# Required env:
#   MERIDIAN_APP_ID   - the Meridian CVM app id (phala cvms get ... --json)
#   MERIDIAN_NODE     - e.g. dstack-pha-prod9.phala.network
#   MERIDIAN_<SEAT>_API_KEY   - per seat, e.g. MERIDIAN_SEAT1_API_KEY (same value
#                               used in the gateway upstream bearer_token)
# Optional email (SMTP; else just prints + exit code for cron MAILTO):
#   ALERT_TO ALERT_FROM SMTP_HOST SMTP_PORT SMTP_USER SMTP_PASS
#   ALERT_ALWAYS=1  -> email even when all healthy (heartbeat)
#
# Cron example (every 15 min, keys sourced from a local env file):
#   */15 * * * * set -a; . /path/seats.env; . /path/alert.env; set +a; \
#                /path/deploy/meridian/check-seats.sh >> /var/log/meridian-check.log 2>&1
set -uo pipefail   # NOT -e: probe every seat even if some fail
DIR="$(cd "$(dirname "$0")" && pwd)"
SEATS="${1:-$DIR/seats.json}"
: "${MERIDIAN_APP_ID:?set MERIDIAN_APP_ID}" "${MERIDIAN_NODE:?set MERIDIAN_NODE}"
command -v jq >/dev/null || { echo "error: jq required"; exit 2; }
[ -f "$SEATS" ] || { echo "error: $SEATS not found"; exit 2; }

PORT_BASE=$(jq -r '.meridian_host_port_base // 3456' "$SEATS")
MODEL=$(jq -r '.models | keys[0]' "$SEATS")
N=$(jq '.seats | length' "$SEATS")
upper() { echo "$1" | tr '[:lower:]-' '[:upper:]_'; }

rows=""; nfail=0
for i in $(seq 0 $((N-1))); do
  name=$(jq -r ".seats[$i].name" "$SEATS"); UP=$(upper "$name"); port=$((PORT_BASE + i))
  keyvar="MERIDIAN_${UP}_API_KEY"; key="${!keyvar:-}"
  url="https://${MERIDIAN_APP_ID}-${port}.${MERIDIAN_NODE}/v1/messages"
  if [ -z "$key" ]; then
    rows+="$(printf '%-10s %-20s %s' "$name" "SKIP" "no $keyvar in env")"$'\n'; continue
  fi
  bf="$(mktemp)"
  code=$(curl -sS -m 30 -o "$bf" -w "%{http_code}" -X POST "$url" \
    -H "Content-Type: application/json" -H "anthropic-version: 2023-06-01" \
    -H "Authorization: Bearer $key" \
    --data "{\"model\":\"$MODEL\",\"max_tokens\":1,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}" 2>/dev/null)
  body="$(cat "$bf" 2>/dev/null)"; rm -f "$bf"
  case "$code" in
    200)              status="OK"; detail="" ;;
    401)              if echo "$body" | grep -qi "expired"; then status="TOKEN_EXPIRED"; detail="claude login $name -> refresh-seat.sh";
                      else status="AUTH_FAIL"; detail="check $keyvar"; fi ;;
    403)              status="BANNED/FLAGGED"; detail="account may be flagged" ;;
    000|5*)           status="PROXY_OR_DOWN"; detail="proxy/container/network" ;;
    *)                status="HTTP_$code"; detail="$(echo "$body" | tr -d '\n' | head -c 80)" ;;
  esac
  rows+="$(printf '%-10s %-20s %s' "$name" "$status" "$detail")"$'\n'
  [ "$code" = "200" ] || nfail=$((nfail + 1))
done

report="Meridian seat health: $((N - nfail))/$N healthy
$(printf '%-10s %-20s %s' SEAT STATUS DETAIL)
$rows"
echo "$report"

# ---- email alert on failures (or heartbeat if ALERT_ALWAYS=1) ----
if { [ "$nfail" -gt 0 ] || [ "${ALERT_ALWAYS:-}" = "1" ]; } && [ -n "${ALERT_TO:-}" ] && [ -n "${SMTP_HOST:-}" ]; then
  subj="[Meridian] $nfail/$N seat(s) need attention"
  [ "$nfail" -eq 0 ] && subj="[Meridian] heartbeat: all $N seats healthy"
  from="${ALERT_FROM:-$ALERT_TO}"; port="${SMTP_PORT:-465}"; scheme="smtps"
  [ "$port" = "587" ] && scheme="smtp"   # 587 = STARTTLS, 465 = implicit TLS
  if printf 'From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s\r\n' "$from" "$ALERT_TO" "$subj" "$report" \
     | curl -sS --url "$scheme://${SMTP_HOST}:${port}" --ssl-reqd \
         --mail-from "$from" --mail-rcpt "$ALERT_TO" \
         --user "${SMTP_USER:-}:${SMTP_PASS:-}" --upload-file - ; then
    echo "[alert] emailed $ALERT_TO"
  else
    echo "[alert] email send FAILED (check SMTP_* env)"
  fi
fi

[ "$nfail" -eq 0 ]
