# Meridian enclave (Claude subscription bridge)

Runs [Meridian](https://github.com/rynfar/meridian) as a **separate TEE enclave**
that turns a Claude Max/Pro subscription into an Anthropic-compatible endpoint.
The core gateway (enclave A) treats each Meridian instance as a non-confidential
`openai-compatible` upstream (`path: /v1/messages`); no gateway source changes are
required. See `../../docs/apexone-architecture.md` for the full architecture,
`../../docs/local-testing-runbook.md` for a local walk-through, and
`../../docs/production-deployment.md` for the enclave deployment.

## Seat model

One **seat** = one Claude subscription account + one Meridian instance + (optional)
one ProxyLite static egress IP + one gateway upstream (`meridian-seatN`).
`HTTPS_PROXY` is process-level, so **per-seat fixed IP requires one Meridian
instance per seat** (not one multi-profile instance).

## OAuth provisioning (required)

Meridian uses the Claude Code subscription OAuth token, stored by `claude login` in:

- `~/.claude/.credentials.json`  (contains `claudeAiOauth`: access + refresh token)
- `~/.claude.json`               (contains `oauthAccount` metadata)

Provision these into each seat's container at `/root/.claude/.credentials.json`
and `/root/.claude.json`. For local testing, `compose.yaml` bind-mounts
`./secrets/seatN/`. For dstack, inject via **encrypted secrets / KMS** so they only
decrypt inside the enclave. The OAuth token auto-refreshes (~8h); the mounted
`.credentials.json` must be writable if you want the refresh to persist across
restarts (drop `:ro` and use a writable volume in production).

## ProxyLite (optional anti-ban)

**ProxyLite is a SOCKS5 proxy.** Meridian / the Claude Code SDK honor only HTTP
proxy env (`HTTPS_PROXY`), not SOCKS5, so the image bundles **gost** as a
SOCKS5→HTTP shim. Set `PROXYLITE_SOCKS5` on a seat and `entrypoint.sh` starts
`gost -L http://127.0.0.1:8118 -F <PROXYLITE_SOCKS5>` and points Meridian at it:

```yaml
PROXYLITE_SOCKS5: "socks5://<user>:<pass>@<host>:<port>"
```

Unset = direct egress (enclave's own IP). Use static/long-acting IPs and keep one
IP per seat stable across renewals (never high-frequency rotation).

## Attestation sidecar (public hardware verification)

`attestor/` is a tiny Node HTTP server (its own image
`docker.io/markerdao/meridian-attestor`, run as the `meridian-attestor` service
on **port 8091**) that exposes a **fresh, nonce-bound Intel TDX DCAP quote +
dstack event log** over public HTTPS **with CORS**, so a browser can
hardware-verify this Meridian CVM (enclave B) the same way it verifies the
gateway (enclave A). It exists because Meridian itself (`:3456`) has no
attestation endpoint and dstack's public `:8090` Info page exposes measurements
but **no quote and no CORS**. It holds **no secrets** and carries **no
prompt/response content** — only public verification material — so it needs no
auth and no sealed env (one attestor per CVM covers all seats, since they share
one dstack identity).

**Endpoint (public, no auth):**

```
GET https://<app-id>-8091.dstack-pha-prod5.phala.network/attestation/quote?nonce=<hex, up to 32 bytes>
```

Response `200` (`Access-Control-Allow-Origin: *`, preflight `OPTIONS` handled):

```json
{
  "quote": "<hex raw TDX DCAP quote>",
  "event_log": "<JSON string of dstack events {imr,event_type,digest,event,event_payload}[]>",
  "vm_config": "<optional string>"
}
```

The quote's 64-byte `report_data` is `nonceBytes` (left-justified into a 32-byte
field) `|| zeros(32)`; the browser checks `report_data[0..32]` equals the nonce
it sent (freshness). Uses `@phala/dstack-sdk` `DstackClient(/var/run/dstack.sock)
.getQuote(reportData)`, which sends the 64-byte `report_data` **raw** (no
hashing) and returns `{ quote, event_log, vm_config }`.

**Build, push, pin, redeploy:**

```bash
docker build -t docker.io/markerdao/meridian-attestor:latest deploy/meridian/attestor
docker push docker.io/markerdao/meridian-attestor:latest
docker inspect --format='{{index .RepoDigests 0}}' docker.io/markerdao/meridian-attestor:latest
# -> paste that @sha256:... digest into compose.cvm.yaml AND gen-seats.sh
#    (ATTESTOR_IMAGE), replacing REPLACE_AFTER_BUILD_AND_PUSH, then re-run
#    ./gen-seats.sh and redeploy the CVM.
```

**Curl test (against a live CVM):**

```bash
NONCE=$(openssl rand -hex 16)
curl -s "https://<app-id>-8091.dstack-pha-prod5.phala.network/attestation/quote?nonce=$NONCE" | jq 'keys'
# -> ["event_log","quote","vm_config"]
```

> ⚠️ **compose_hash changes.** Adding this sidecar alters the Meridian CVM's
> measured compose, so its `compose_hash` moves away from the current
> `3bc8be00…5796`. After you rebuild/pin/redeploy, read the NEW `compose_hash`
> from the quote's RTMR3 `compose-hash` event (or dstack app-compose tooling) and
> update **both** `docs/attestation-verification.md` and
> `frontend/src/constants/attestation.ts` (`MERIDIAN_REFERENCE.composeHash`) to
> match, or in-browser verification of enclave B will fail the compose-hash check.

## Build & run (local)

```bash
docker compose -f deploy/meridian/compose.yaml build
```

> Note: if your Docker build sandbox has no container DNS/egress, add
> `--network=host` (build only): `docker build --network=host -t meridian-enclave:dev deploy/meridian`.

```bash
mkdir -p deploy/meridian/secrets/seat1
cp ~/.claude/.credentials.json deploy/meridian/secrets/seat1/
cp ~/.claude.json              deploy/meridian/secrets/seat1/
docker compose -f deploy/meridian/compose.yaml up -d
# health:
curl -s http://<seat1>:3456/health   # expect {"auth":{"loggedIn":true},...}
```

Then add the upstream to the gateway `upstreams.json` (see
`gateway-upstreams.example.json`, entry `meridian-seat1`) and the route to sub2api
`consult.route_map` (see `../config.example.yaml`).

---

## Phase 0 spike results (2026-07-01, verified locally)

| Spike | Question | Result |
|---|---|---|
| 0.1 | Meridian bridges Claude subscription → `/v1/messages` | ✅ PASS — returned a proper Anthropic Messages response with `usage`. |
| 0.2 | Agentic tool loops work through the route (`maxTurns=2`?) | ✅ PASS — API `tool_use` returned with client tool schema; **real `claude -p` completed a multi-step (list dir → read file) task through Meridian**. Client-driven loops are NOT blocked by `maxTurns`. |
| 0.3 (mechanism) | Does Meridian / Claude Code SDK honor `HTTPS_PROXY`? | ✅ PASS — with `HTTPS_PROXY` pointed at a local logging CONNECT proxy, the proxy logged `CONNECT api.anthropic.com:443`, proving egress is routed through the proxy. |
| 0.3 (real ProxyLite) | End-to-end via the purchased static IP | ✅ PASS — ProxyLite turned out to be **SOCKS5** (`socks5://<user>:<pass>@<static-ip>:<port>`), with a sticky exit IP. Via a gost SOCKS5→HTTP shim, a full Meridian `/v1/messages` call succeeded and the shim logged `CONNECT api.anthropic.com:443 via SOCKS5` — Anthropic traffic egressed from the static IP. |

### Note on the earlier 403 anomaly
An earlier request routed through a *direct* (datacenter-IP) HTTP proxy returned
`403 Request not allowed`. Routing through the ProxyLite **residential static IP**
did NOT reproduce it — the request succeeded cleanly. Consistent with the anti-ban
rationale (residential egress is treated better than datacenter egress).
