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

/**
 * Baked-in fallback reference for the production publicai-gateway CVM
 * (node dstack-pha-prod5). Kept in sync with deploy/phala/reference.json —
 * that file wins when reachable, and is updated first after each deploy (a
 * baked value here may lag one generation behind by design).
 */
export const SUB2API_REFERENCE: EnclaveReference = {
  appId: '9467ce766ed63423e86a19d2f36cc9a9926daf27',
  osImage: 'dstack-0.5.9',
  osImageHash: 'bd369a8c2f9edb2b52dad48ac8e0b32dde5f1337c423a506b48d07403a7d8033',
  composeHash: 'cf50a8d6fc95f1765af2c8c498baf04bbc9e4175ff8a4e44563d4c6dfebe1ded',
  sourceRepo: 'https://github.com/FiiLabs/sub2api.git',
  sourceCommit: 'd4b181da492ab600aa15a41e91fffb5b0bf2e384',
  images: [
    {
      name: 'sub2api',
      digest:
        'docker.io/markerdao/sub2api@sha256:b03f6ac4ae50e4fd70ea93b6096def816278944e11cd112a451f62d217b02b25',
    },
    {
      name: 'sub2api-attestor',
      digest:
        'docker.io/markerdao/sub2api-attestor@sha256:8b8affb684c4f676bf15277601dfa9d1a66f02a37a1a3add8ae3280d9cf5d793',
    },
  ],
  confidential: false,
}

/** Phala public-endpoint node hosting the CVM. */
export const ATTESTOR_NODE = 'dstack-pha-prod5.phala.network'

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
