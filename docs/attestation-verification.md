# Attestation verification (single-enclave sub2api)

How to independently verify that the production service is running the
unmodified open-source sub2api build inside a genuine Intel TDX CVM on Phala
dstack. Everything below can be done by a third party; nothing requires
trusting the operator or the server.

Architecture: **one CVM**, four containers — `sub2api` (Go backend + embedded
frontend), `postgres`, `redis`, and the `attestor` sidecar
([`deploy/attestor/`](../deploy/attestor/)) that serves a nonce-bound raw TDX
quote on port 8091. The measured compose is
[`deploy/phala/docker-compose.yml`](../deploy/phala/docker-compose.yml).

## 1. Published reference values

The authoritative reference is
[`deploy/phala/reference.json`](../deploy/phala/reference.json) — the /proof
page fetches it at runtime, and **its full change history is public git
history** (that is deliberate: a reference swap is a visible event, not a
silent rebuild).

| Field | Value |
| --- | --- |
| App ID | `aee564d9e3e1188f47413de2eaad17a8844276db` |
| Node | `dstack-pha-prod9.phala.network` |
| OS image / hash | see reference.json |
| compose_hash | see reference.json (dstack-canonicalized app-compose hash, NOT a raw yaml sha256) |
| Source repo / commit | see reference.json |
| Image digests | see reference.json (linux/amd64 child manifests) |

## 2. Self-verify in 30 seconds

Open `/proof` on the service. The page, **in your browser**:

1. generates a random nonce and fetches
   `https://<app-id>-8091.<node>/attestation/quote?nonce=<nonce>`;
2. verifies the quote's signature chain against Intel collateral from Phala
   PCCS using `@phala/dcap-qvl-web` (wasm build of the audited `dcap-qvl`);
3. checks `report_data == nonce || zeros` (freshness — not a replay);
4. replays the dstack event log into RTMR3 and compares it to the quote
   register (event log is authentic), then compares `compose-hash` (from the
   event log AND from `mr_config_id[1..33]`), `os-image-hash` and `app-id`
   against the published reference.

All green = genuine hardware, running exactly the measured compose, right now.
Evidence is downloadable as JSON for offline re-checking.

## 3. Manual verification (no browser)

```bash
APP=aee564d9e3e1188f47413de2eaad17a8844276db
NODE=dstack-pha-prod9.phala.network
NONCE=$(openssl rand -hex 16)

# Fresh nonce-bound quote + event log
curl -s "https://${APP}-8091.${NODE}/attestation/quote?nonce=${NONCE}" > quote.json

# compose-hash / os-image-hash / app-id as measured into RTMR3
jq -r '.event_log' quote.json | jq -r '.[] | select(.imr==3) | "\(.event): \(.event_payload)"'

# Verify the quote itself with any DCAP verifier, e.g. dcap-qvl's CLI:
#   cargo install dcap-qvl-cli
jq -r '.quote' quote.json | xxd -r -p > quote.bin
dcap-qvl verify quote.bin
```

Cross-checks:

- **compose_hash recomputation**: the measured value is dstack's hash of the
  app-compose (which embeds `deploy/phala/docker-compose.yml` verbatim).
  Recompute with dstack's `app-compose` tooling, or compare against the Phala
  Cloud dashboard / `phala cvms get` for the CVM.
- **Phala Trust Center**: the same app-id and measurements are independently
  displayed at trust.phala.network.

## 4. Digest ↔ source binding (open-source provability)

Both images the compose pins are built by public CI
([`.github/workflows/publish-tee-images.yml`](../.github/workflows/publish-tee-images.yml))
with SLSA provenance (`mode=max`), an SBOM, and a keyless cosign signature.
The signature and provenance attach to the **multi-arch index digest**; the
compose pins the **linux/amd64 child manifest** covered by that index
(`docker buildx imagetools inspect <image>@<index-digest>` shows the mapping).

```bash
# Proves the image was built by THIS repo's GitHub CI (OIDC identity),
# and the provenance names the exact source commit:
cosign verify-attestation --type slsaprovenance \
  --certificate-identity-regexp 'https://github.com/FiiLabs/sub2api/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  docker.io/markerdao/sub2api@sha256:<index-digest>

cosign verify \
  --certificate-identity-regexp 'https://github.com/FiiLabs/sub2api/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  docker.io/markerdao/sub2api@sha256:<index-digest>
```

Same commands for `docker.io/markerdao/sub2api-attestor`. `postgres` and
`redis` are Docker official images, pinned by index digest in the compose.

Chain of trust, end to end:

```
Intel TDX hardware ── signs ──▶ quote (report_data = your nonce)
        quote.rt_mr3 / mr_config_id ══ event-log replay ══▶ compose_hash
        compose_hash ══▶ deploy/phala/docker-compose.yml (this repo, verbatim)
        compose pins image@digest ══ cosign + SLSA provenance ══▶ CI run
        CI run ══ OIDC identity ══▶ github.com/FiiLabs/sub2api @ commit
        commit ══▶ auditable source (this repository)
```

## 5. What is NOT covered (honest scope)

- TLS terminates at the Phala gateway in front of the CVM; there is no
  additional end-to-end encryption layer in this architecture.
- Requests forwarded to upstream model providers are plaintext to those
  providers.
- Raw MRTD / RTMR0-2 have no published reference here; OS identity is anchored
  via the dstack `os-image-hash` event instead (the /proof page marks this
  honestly as informational).

## 6. Operator runbook: updating the pins

1. Run the `publish-tee-images` workflow (tag e.g. `proof-vN`) → note both
   INDEX digests; resolve the amd64 children via `imagetools inspect`.
2. Pin the amd64 child digests in `deploy/phala/docker-compose.yml`.
3. `phala deploy --cvm-id <id> -c deploy/phala/docker-compose.yml -e <sealed env>`.
4. Read back the live compose_hash from the attestor
   (`/attestation/quote` → event log `compose-hash`) and the os-image-hash.
5. Update `deploy/phala/reference.json` (composeHash, osImageHash,
   sourceCommit, image digests) + the baked-in `SUB2API_REFERENCE` fallback in
   `frontend/src/constants/attestation.ts`, commit, push.
6. Open `/proof` — all gating checks green.

Note the deliberate order: the reference is published AFTER deploy (it depends
on the deployed compose), which is why the frontend loads it from the repo at
runtime instead of baking it into the measured image.
