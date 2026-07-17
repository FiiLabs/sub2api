#!/usr/bin/env bash
# Post-v11 leak-fix effectiveness check. Reads each seat's in-memory telemetry
# and reports the metrics that prove the root-cause fix is working on REAL
# traffic. Compare against the v10 baseline measured on 2026-07-17 (404 reqs):
#
#   adapter=claude-code   0%   -> expect a MAJORITY of real Claude Code traffic
#   isResume             25%   -> expect substantially higher
#   msg>=10 taking fresh 74%   -> expect substantially lower
#
# NOTE: telemetry is an in-memory ring buffer — it resets on every container
# restart (incl. 'phala envs update'). Numbers only mean something after a few
# hours of real traffic.
#
#   ./check-leak-metrics.sh [seat1 seat2 ...]
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
APP="${MERIDIAN_APP_ID:-bbbc8691946a8575accfa86b8b533ad288d00828}"
NODE="${MERIDIAN_NODE:-dstack-pha-prod5.phala.network}"
seats=("$@"); [ ${#seats[@]} -eq 0 ] && seats=(seat1 seat2)

for seat in "${seats[@]}"; do
  UP=$(echo "$seat" | tr '[:lower:]-' '[:upper:]_')
  key=$(grep -E "^MERIDIAN_${UP}_API_KEY=" "$DIR/seats.env" 2>/dev/null | head -1 | cut -d= -f2-)
  [ -n "$key" ] || { echo "== $seat: no API key in seats.env, skip"; continue; }
  idx=$(jq -r --arg n "$seat" '.seats | map(.name) | index($n)' "$DIR/seats.json")
  [ "$idx" != "null" ] || { echo "== $seat: not in seats.json, skip"; continue; }
  base=$(jq -r '.meridian_host_port_base // 3456' "$DIR/seats.json")
  url="https://${APP}-$((base + idx)).${NODE}"

  echo "== $seat ($url)"
  curl -sS -m 25 -H "Authorization: Bearer $key" "$url/telemetry/requests?limit=500" 2>/dev/null \
  | node -e '
let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{
  let a; try { a = JSON.parse(s) } catch { console.log("   telemetry unreadable:", s.slice(0,80)); return }
  if (!Array.isArray(a) || a.length === 0) { console.log("   no requests recorded yet (ring buffer empty — restarted, or no traffic)"); return }
  const byA={}, byL={}; let dfr=0,res=0,big=0,bigNew=0;
  for (const e of a) {
    byA[e.adapter]=(byA[e.adapter]||0)+1;
    byL[e.lineageType]=(byL[e.lineageType]||0)+1;
    if (e.hasDeferredTools) dfr++;
    if (e.isResume) res++;
    if (e.messageCount>=10) { big++; if (e.lineageType==="new") bigNew++; }
  }
  const pct=(n,d)=> d? (100*n/d).toFixed(0)+"%" : "n/a";
  console.log("   requests:", a.length);
  console.log("   adapter :", JSON.stringify(byA), "   <- claude-code = the v11 fix engaging");
  console.log("   lineage :", JSON.stringify(byL));
  console.log("   deferredTools (real CC traffic):", dfr+"/"+a.length, pct(dfr,a.length));
  console.log("   isResume:", res+"/"+a.length, pct(res,a.length), "  [v10 baseline 25%]");
  console.log("   msg>=10 taking FRESH:", bigNew+"/"+big, pct(bigNew,big), "  [v10 baseline 74% — lower is better]");
});'
  echo
done
