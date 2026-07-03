# Attestation Verification (Production Reference Values)

This document lets a **third party independently verify** that the ApexOne TEE
gateway is running the exact, auditable code we claim — without trusting us.
It publishes the reference measurements a verifier compares the live attestation
against, plus the step-by-step procedure.

> Scope of the guarantee (be precise):
> - ✅ **Runs on genuine Intel TDX hardware** — the quote chains to Intel's roots.
> - ✅ **Runs a production OS** (`is_dev=false`, no SSH) — confidentiality is enforced.
> - ✅ **Runs the pinned, audited gateway source** (`repo_commit` below).
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
| compose_hash | `4e1e1c2cd4f8265475a4a57e20771967141a8f3c3bc1bba8e24ddd47b2686ec3` |
| source repo | `https://github.com/Dstack-TEE/private-ai-gateway.git` |
| source commit | `975ac50f3074ffdaf18a5e377934a8db99ba64ba` |
| receipt signing_address | `0x6654a3347cb50616b88815fdeac7065dd3f80038` (ecdsa) |
| measured compose | `deploy/gateway/compose.enclave.yaml` (in this repo) |
| launcher image | `docker.io/markerdao/git-launcher-rust@sha256:3fe4ad9b246e131f0445b67bc193648c1247686e4d8b0401d0d73c4ce1cf4c58` |

### Meridian seat (enclave B — non-confidential Claude-subscription bridge)
| Field | Expected value |
|---|---|
| app_id | `bbbc8691946a8575accfa86b8b533ad288d00828` |
| os image | `dstack-0.5.9` — hash `bd369a8c…` (same as gateway) |
| compose_hash | `8f7fc6e1780d142ec661e29ad12b6f15f74da1630e7f1def8e6629d5155af2ad` |
| measured compose | `deploy/meridian/compose.seats.generated.yaml` (generate via `gen-seats.sh`; contains only `${VAR}` refs + the image digest + ports — NO secrets) |
| meridian image | `docker.io/markerdao/meridian-enclave@sha256:aaef9917387c41674cf9dc3e85bd117366229fe8299b7aa555f5014f437646c6` |

> The Meridian route is **non-confidential** (Anthropic sees plaintext); its
> attestation proves the bridge software + egress, not content confidentiality.

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
#    and the app compose_hash to 4e1e1c2c...  (gateway) / 8f7fc6e1... (meridian).

# 4) Recompute compose_hash from the published compose (dstack canonicalizes the
#    docker-compose into app-compose.json and hashes THAT — it is NOT a raw
#    sha256 of the yaml). Use dstack's app-compose hashing tool on
#    deploy/gateway/compose.enclave.yaml and confirm it equals 4e1e1c2c...

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
