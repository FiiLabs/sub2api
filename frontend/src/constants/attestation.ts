/**
 * Attestation reference values + attestor endpoint config (single-enclave
 * architecture: the whole service is one sub2api CVM on Phala dstack).
 *
 * The /proof page compares a LIVE nonce-bound TDX quote (fetched from the
 * attestor sidecar in the CVM) against a published reference. The reference has
 * two sources, in order:
 *   1. runtime: deploy/phala/reference.json fetched from the public repo
 *      (see utils/attestation/reference.ts) — authoritative, updateable
 *      without rebuilding this frontend, and its history is auditable in git;
 *   2. fallback: SUB2API_REFERENCE below, baked in at build time.
 *
 * The runtime source exists because this frontend is embedded in the measured
 * sub2api image: a composeHash baked in here can never equal the hash of the
 * compose that pins the very image containing it (circular). The repo-hosted
 * reference.json breaks that cycle.
 */

export interface EnclaveReference {
  /** dstack app id of the CVM. */
  appId: string
  /** dstack OS image name. */
  osImage: string
  /** Measured OS image hash (dstack `os-image-hash` event). */
  osImageHash: string
  /** dstack-canonicalized app-compose hash (NOT a raw yaml sha256). */
  composeHash: string
  /** Public source anchoring the measured code. */
  sourceRepo?: string
  sourceCommit?: string
  /** Pinned image digests running in the CVM (compose `image:` values). */
  images?: { name: string; digest: string }[]
  /** Whether this hop is confidential w.r.t. its upstream. */
  confidential: boolean
}

/** Placeholder for values that are only known after a deploy (see P0-7 docs). */
const UNSET = '0000000000000000000000000000000000000000000000000000000000000000'

/**
 * Baked-in fallback reference for the production sub2api CVM
 * (node dstack-pha-prod9). Kept in sync with deploy/phala/reference.json —
 * that file wins when reachable.
 */
export const SUB2API_REFERENCE: EnclaveReference = {
  appId: 'aee564d9e3e1188f47413de2eaad17a8844276db',
  osImage: 'dstack-0.5.9',
  osImageHash: UNSET, // read from the first live quote after deploy
  composeHash: UNSET, // read back from the CVM RTMR3 after deploy
  sourceRepo: 'https://github.com/FiiLabs/sub2api.git',
  sourceCommit: UNSET.slice(0, 40),
  images: [
    { name: 'sub2api', digest: `docker.io/markerdao/sub2api@sha256:${UNSET}` },
    { name: 'sub2api-attestor', digest: `docker.io/markerdao/sub2api-attestor@sha256:${UNSET}` },
  ],
  confidential: false,
}

/** Phala public-endpoint node hosting the CVM. */
export const ATTESTOR_NODE = 'dstack-pha-prod9.phala.network'

/**
 * Attestation sidecar base URL (deploy/attestor/): serves the nonce-bound raw
 * TDX quote + dstack event log with CORS `*`. Different origin than the page.
 */
export const ATTESTOR_BASE_URL: string =
  (import.meta.env.VITE_ATTESTOR_BASE_URL as string | undefined)?.replace(/\/+$/, '') ||
  `https://${SUB2API_REFERENCE.appId}-8091.${ATTESTOR_NODE}`

export const ATTESTATION_QUOTE_PATH = '/attestation/quote'

/**
 * Authoritative runtime reference, hosted in the public repo so its change
 * history is auditable and updates don't require rebuilding the measured image.
 */
export const REFERENCE_JSON_URL: string =
  (import.meta.env.VITE_REFERENCE_JSON_URL as string | undefined) ||
  'https://raw.githubusercontent.com/FiiLabs/sub2api/proof-solo/deploy/phala/reference.json'
