/**
 * Attestation reference values (production) + gateway endpoint config.
 *
 * Mirrors docs/attestation-verification.md. A verifier compares the LIVE
 * attestation report (fetched from the gateway) against these published values.
 * Update this file whenever a compose_hash, source commit, or image digest
 * changes (i.e. whenever docs/attestation-verification.md changes).
 *
 * NOTE: the attestation endpoint is served by the TEE gateway (enclave A) at
 * api.apex1.us — NOT by sub2api. It is a different origin, so the gateway must
 * emit permissive CORS headers for the in-browser fetch to succeed.
 */

export interface EnclaveReference {
  /** dstack app id of the CVM. */
  appId: string
  /** dstack OS image name. */
  osImage: string
  /** Measured OS image hash. */
  osImageHash: string
  /** dstack-canonicalized app-compose hash (NOT a raw yaml sha256). */
  composeHash: string
  /** Public source anchoring the measured code (absent for image-only bridges). */
  sourceRepo?: string
  sourceCommit?: string
  /** ECDSA receipt signing address bound to the attestation (gateway only). */
  receiptSigningAddress?: string
  /** Pinned helper image digests. */
  images?: { name: string; digest: string }[]
  /** Whether this hop is confidential w.r.t. its upstream. */
  confidential: boolean
}

/** Base URL of the TEE gateway that serves /v1/attestation/report. */
export const ATTESTATION_BASE_URL: string =
  (import.meta.env.VITE_ATTESTATION_BASE_URL as string | undefined)?.replace(/\/+$/, '') ||
  'https://api.apex1.us'

// The BARE ACI report (aci/1 report_data = digest||zeros), which the browser
// verifier checks. NOTE: /v1/attestation/report serves the LEGACY-wrapped report
// (report_data = signing-key identity||nonce + injected signing_address), which
// the aci/1 binding check does NOT match — use the aci endpoint here.
export const ATTESTATION_REPORT_PATH = '/v1/aci/attestation'

export const OS_IMAGE = 'dstack-0.5.9'

/** Gateway — enclave A (confidential path, serves api.apex1.us). */
export const GATEWAY_REFERENCE: EnclaveReference = {
  appId: 'd65b6fbc8df7a1a52a55807a4f5bd2c7dc67b983',
  osImage: OS_IMAGE,
  osImageHash: 'bd369a8c2f9edb2b52dad48ac8e0b32dde5f1337c423a506b48d07403a7d8033',
  composeHash: '746302d09218f21a1c860b4d8a08d8715a4a2b4206ce871d9f6dba6ef3cf65c2',
  sourceRepo: 'https://github.com/Dstack-TEE/private-ai-gateway.git',
  sourceCommit: '975ac50f3074ffdaf18a5e377934a8db99ba64ba',
  receiptSigningAddress: '0x6654a3347cb50616b88815fdeac7065dd3f80038',
  images: [
    {
      name: 'git-launcher-rust',
      digest:
        'docker.io/markerdao/git-launcher-rust@sha256:3dc0e8be4050e2cf10d2e618f103873a1331f5f5adb8f9452f4378696cc36611',
    },
  ],
  confidential: true,
}

/** Meridian seat — enclave B (non-confidential Claude-subscription bridge). */
export const MERIDIAN_REFERENCE: EnclaveReference = {
  appId: 'bbbc8691946a8575accfa86b8b533ad288d00828',
  osImage: OS_IMAGE,
  osImageHash: 'bd369a8c2f9edb2b52dad48ac8e0b32dde5f1337c423a506b48d07403a7d8033',
  composeHash: 'df7f25adaccd64ff2bffeefb7bf4448d42c649e35da8fb10e61e6613aed5db29',
  sourceRepo: 'https://github.com/FiiLabs/meridian.git',
  sourceCommit: '5a40947c3b48494ed4080c364908669ec9944822',
  images: [
    {
      name: 'meridian-enclave',
      digest:
        'docker.io/markerdao/meridian-enclave@sha256:1f904222ba6f9cbb433bd65e75371f8d448962ed8f494a32ba747e3e3a063cc7',
    },
  ],
  confidential: false,
}

/**
 * Meridian enclave-B attestation sidecar: a nonce-bound raw TDX quote + dstack
 * event log (deploy/meridian/attestor). Unlike the gateway it exposes NO ACI
 * keyset/receipt — hop-3 verifies genuine hardware + nonce freshness +
 * measurements only. Different origin (the Meridian seat CVM), needs CORS (the
 * sidecar sets `*`). Defaults to the reference seat's app-id on port 8091.
 */
export const MERIDIAN_ATTESTATION_URL: string =
  (import.meta.env.VITE_MERIDIAN_ATTESTATION_URL as string | undefined)?.replace(/\/+$/, '') ||
  `https://${MERIDIAN_REFERENCE.appId}-8091.dstack-pha-prod5.phala.network`

export const MERIDIAN_ATTESTATION_PATH = '/attestation/quote'

/**
 * Path A (E2EE hop-1 browser-level proof). The inference endpoint on the gateway
 * accepts E2EE requests (opt-in via `X-E2EE-*` headers) and CORS-allows `*`, so
 * the browser can POST an ECIES-encrypted prompt cross-origin. The default demo
 * model is deployment-specific — the UI lets the user edit it, and it can be
 * overridden at build time via `VITE_E2EE_DEMO_MODEL`.
 */
export const INFERENCE_CHAT_PATH = '/v1/chat/completions'

export const E2EE_DEMO_MODEL: string =
  (import.meta.env.VITE_E2EE_DEMO_MODEL as string | undefined) || 'claude-sonnet-5'

/** Keep the demo cheap: the round-trip only needs to prove the enclave decrypted. */
export const E2EE_DEMO_MAX_TOKENS = 16
