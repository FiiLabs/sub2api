# Attestation Verification (Production Reference Values)

This document lets a **third party independently verify** that the ApexOne TEE
gateway is running the exact, auditable code we claim — without trusting us.
It publishes the reference measurements a verifier compares the live attestation
against, plus the step-by-step procedure.

> Scope of the guarantee (be precise):
> - ✅ **Runs on genuine Intel TDX hardware** — the quote chains to Intel's roots.
> - ✅ **Runs a production OS** (`is_dev=false`, no SSH) — confidentiality is enforced.
> - ✅ **Runs the pinned, audited gateway source** (`repo_commit` below).
> - ✅ **Browser-level E2EE to the attested key** (hop-1): the gateway advertises a
>   dedicated encryption key in its attestation-bound keyset, so a browser can
>   encrypt directly to the enclave (no trust in TLS/ingress). See §5.
> - ⚠️ Residual (does NOT invalidate the above, but disclose it): the gateway
>   software is a `0.1.0` developer preview (attestation `vendor=private-ai-gateway-dev`);
>   the in-TEE Rust toolchain is bootstrapped via apt+rustup (a dev-grade trust
>   surface, see private-ai-gateway `deploy/README.md` "Toolchain Posture").

---

## 1. Reference values (production, dstack-0.5.9)

Fetch the live report and compare against these. Update this table whenever
`COMMIT_SHA`, an image digest, or the compose changes (any of which changes
`compose_hash`).

### Gateway (enclave A — confidential path, serves api.apex1.us)
| Field | Expected value |
|---|---|
| endpoint | `https://api.apex1.us` (and `https://<app-id>-8086.dstack-pha-prod5.phala.network`) |
| app_id | `d65b6fbc8df7a1a52a55807a4f5bd2c7dc67b983` |
| os image | `dstack-0.5.9` — hash `bd369a8c2f9edb2b52dad48ac8e0b32dde5f1337c423a506b48d07403a7d8033` |
| compose_hash | `746302d09218f21a1c860b4d8a08d8715a4a2b4206ce871d9f6dba6ef3cf65c2` |
| source repo | `https://github.com/Dstack-TEE/private-ai-gateway.git` |
| source commit | `975ac50f3074ffdaf18a5e377934a8db99ba64ba` |
| receipt signing_address | `0x6654a3347cb50616b88815fdeac7065dd3f80038` (ecdsa) |
| measured compose | `deploy/gateway/compose.enclave.yaml` (in this repo) |
| launcher image | `docker.io/markerdao/git-launcher-rust@sha256:3dc0e8be4050e2cf10d2e618f103873a1331f5f5adb8f9452f4378696cc36611` |

### Meridian seat (enclave B — non-confidential Claude-subscription bridge)
| Field | Expected value |
|---|---|
| app_id | `bbbc8691946a8575accfa86b8b533ad288d00828` |
| os image | `dstack-0.5.9` — hash `bd369a8c…` (same as gateway) |
| attestation endpoint | `https://<app-id>-8091.dstack-pha-prod5.phala.network/attestation/quote?nonce=<hex>` (nonce-bound TDX DCAP quote + dstack event log, CORS; served by the `meridian-attestor` sidecar) |
| compose_hash | `4dee71a862c2e402d81982a5ab091dd5c2279c4286f2b5f933b5816c420c4eff` (includes the `meridian-attestor` sidecar; verified against the quote's RTMR3 `compose-hash` event) |
| measured compose | `deploy/meridian/compose.seats.generated.yaml` (generate via `gen-seats.sh`; contains only `${VAR}` refs + the two image digests + ports — NO secrets) |
| meridian image | `docker.io/markerdao/meridian-enclave@sha256:79f828ba66c1dc96816000723c7232c9d1fa289b710d9bdcc559e67cb075cbb8` |
| attestor image | `docker.io/markerdao/meridian-attestor@sha256:7e6d51fe173a2762773a9a80a63ff5eee0303a060bc12551bc1fcb30f25056a1` (CI-built — SLSA provenance + keyless cosign; from `deploy/meridian/attestor/`) |

> The Meridian route is **non-confidential** (Anthropic sees plaintext); its
> attestation proves the bridge software + egress, not content confidentiality.
>
> The `meridian-attestor` sidecar (`deploy/meridian/attestor/`) binds the quote to
> a caller nonce: `report_data = nonceBytes(32) || zeros(32)`, so a browser checks
> `report_data[0..32] == nonce` for freshness. When it is added the CVM's
> `compose_hash` changes — update the row above **and**
> `frontend/src/constants/attestation.ts` (`MERIDIAN_REFERENCE.composeHash`).

---

## 2. Verification procedure

```bash
BASE=https://api.apex1.us
NONCE=$(openssl rand -hex 16)

# 1) Fetch the live attestation report (bind it to your nonce for freshness).
curl -sS "$BASE/v1/attestation/report?nonce=$NONCE" > report.json

# 2) Verify the Intel TDX quote is genuine (proves real TDX hardware, not a fake).
#    Use Intel DCAP / an attestation verifier on report.json.intel_quote,
#    or verify via Phala's Trust Center (see §3) which does DCAP for you.

# 3) Check the measurements against §1:
jq -r '.attestation.source_provenance | {repo_url, repo_commit}' report.json
#    -> repo_commit MUST equal 975ac50f...  (the audited gateway source)
#    Compare the quote's OS measurement to os_image_hash bd369a8c... (dstack-0.5.9),
#    and the app compose_hash to 746302d0...  (gateway) / 4dee71a8... (meridian).

# 4) Recompute compose_hash from the published compose (dstack canonicalizes the
#    docker-compose into app-compose.json and hashes THAT — it is NOT a raw
#    sha256 of the yaml). Use dstack's app-compose hashing tool on
#    deploy/gateway/compose.enclave.yaml and confirm it equals 746302d0...

# 5) Verify a receipt is signed by the attested key and binds to the same report:
#    (fetch a receipt with x-receipt-id, then, in the private-ai-gateway repo)
#    cargo run --example verify_aci_artifacts -- \
#      --report report.json --receipt receipt.json --nonce $NONCE
#    -> confirms the receipt's signing key == the attested signing_address above.

# 6) Verify the pinned image digests correspond to audited source (see §4).
```

A verifier who completes steps 1–6 has proven, with no trust in us: real TDX
hardware + production OS + the exact audited gateway commit + the exact composed
images, and that receipts are signed by that attested enclave.

---

## 3. Publish on Phala Trust Center

List the production CVMs so anyone can fetch + DCAP-verify their attestation
through Phala's infrastructure (currently `listed=false`):

```bash
# Re-deploy (or update) with --listed to publish on the public Trust Center.
phala deploy --cvm-id d65b6fbc8df7a1a52a55807a4f5bd2c7dc67b983 -c deploy/gateway/compose.enclave.yaml -e <sealed.env> --listed
phala deploy --cvm-id bbbc8691946a8575accfa86b8b533ad288d00828 -c deploy/meridian/compose.seats.generated.yaml -e <sealed.env> --listed
```

---

## 4. Image provenance (digest ↔ audited source)

| Image | How its digest is bound to source |
|---|---|
| **gateway (private-ai-gateway)** | ✅ Already covered: built **in-TEE by git-launcher** from `repo_commit` 975ac50f. The commit IS the provenance — no image to reproduce. |
| **git-launcher-rust** | Built from `deploy/gateway/launcher/Dockerfile` (= `dstacktee/git-launcher@4437dce` + build-essential). Needs SLSA provenance and/or a reproducible build. |
| **meridian-enclave** | Built from `deploy/meridian/` (`node:22-slim` + `@rynfar/meridian@1.44.1` + gost). npm makes bit-reproduction hard → use **SLSA provenance** (CI build + cosign keyless signing). |

The two `markerdao/*` images are built + signed by the GitHub Actions workflow
`.github/workflows/publish-tee-images.yml` (SLSA provenance + SBOM + cosign
keyless). Verify with:

```bash
cosign verify-attestation --type slsaprovenance \
  --certificate-identity-regexp 'https://github.com/FiiLabs/sub2api/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  docker.io/markerdao/meridian-enclave@sha256:aaef99...
```

This proves the digest was built by our CI from a specific commit of this repo —
so `deploy/meridian/Dockerfile` + `@rynfar/meridian@1.44.1` is what's inside.

> Optional stronger step (bit-reproducible): `git-launcher-rust` is a trivial
> Dockerfile and can be made bit-for-bit reproducible (pin base by digest, pin
> apt versions, `SOURCE_DATE_EPOCH` + `rewrite-timestamp`) so verifiers rebuild
> and match the digest without trusting the CI. meridian is impractical to
> bit-reproduce (npm) — rely on SLSA provenance there.

---

## 5. E2EE hop-1 browser-level proof (Path A)

hop-1 (your device → gateway) is verifiable two ways. The baseline is an
**audit-level** proof: the TDX quote + `compose_hash` show the measured compose
terminates TLS inside the CVM (`dstack-ingress`), so plaintext never leaves the
enclave. A browser cannot cryptographically bind the live TLS connection from
pure JS, so Path A adds a **TLS-independent, browser-level** proof.

The gateway publishes a dedicated encryption key in its attested keyset —
`workload_keyset.e2ee_public_keys[]`, algo `secp256k1-aes-256-gcm-hkdf-sha256`
(`key_id dstack-kms-e2ee-v1`). Its private half is released by dstack-KMS only to
the attested workload (`evidence.key_custody`). Because the key is inside
`workload_keyset`, it is covered by `workload_keyset_digest`, which is folded into
`report_data` and bound into the TDX quote — all already verified in §2. So a
verifier who trusts the quote transitively trusts this key: **anything encrypted
to it can be decrypted only inside the attested enclave.**

### Scheme (client side, byte-faithful to `private-ai-gateway/src/aci/e2ee.rs`)
ECIES: ephemeral secp256k1 ECDH → HKDF-SHA256 (info `aci.e2ee.v2.secp256k1`) →
AES-256-GCM. Wire ciphertext (lowercase hex):
`ephemeral_uncompressed_pubkey(65) || aes_gcm_nonce(12) || ciphertext_tag`. The
request/response AAD strings match `src/aggregator/service/e2ee_crypto.rs`
(`v2|req|…` / `v2|resp|…`). Opt-in via request headers `X-E2EE-Version: 2`,
`X-Client-Pub-Key`, `X-Model-Pub-Key`, `X-E2EE-Nonce`, `X-E2EE-Timestamp`; the
workload advertises support via `service_capabilities.supported_e2ee_versions`.

### What the `/proof` page does
- **Verification-side (always-on, free):** checks `supported_e2ee_versions` ∋ `2`,
  the secp256k1 key is present + well-formed, and it is covered by the verified
  `workload_keyset_digest`. No data is sent. (`e2ee.capability` / `key_present` /
  `key_attested`; `key_custody` is shown informationally — full `signature_chain`
  verification is a follow-up.)
- **Live round-trip (opt-in):** the browser ECIES-encrypts a prompt to the
  attested key, POSTs it E2EE to `/v1/chat/completions` (the gateway CORS-allows
  `*`), and authenticated-decrypts the E2EE response. Success proves the enclave
  held the key and ran the channel live. The demo prompt is decrypted inside the
  enclave and forwarded to Anthropic in plaintext (hop 4) and consumes tokens —
  this is disclosed in the UI.

Client implementation: `frontend/src/utils/attestation/e2ee.ts` (ECIES + AAD),
`e2eeProof.ts` (both proof levels). Verify it round-trips with a real key/model:

```bash
# In the browser: /proof → "Run a live E2EE round-trip" → paste an API key +
# a valid model id → the response must authenticated-decrypt (green).
```
