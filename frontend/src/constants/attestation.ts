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
 * Baked-in last-resort reference for the production publicai-gateway CVM
 * (node dstack-pha-prod5). This copy can never be authoritative by
 * construction: it lives inside the measured image, so writing the current
 * composeHash here changes the image digest, which changes the composeHash.
 * It is a hint for offline inspection only — a mismatch against it must be
 * reported as undecided, never as a failed enclave. deploy/phala/reference.json
 * is the source of truth.
 */
export const SUB2API_REFERENCE: EnclaveReference = {
  appId: '9467ce766ed63423e86a19d2f36cc9a9926daf27',
  osImage: 'dstack-0.5.9',
  osImageHash: 'bd369a8c2f9edb2b52dad48ac8e0b32dde5f1337c423a506b48d07403a7d8033',
  composeHash: 'a199f53d46141490d730a4c8575e5bcbfb454eb38421bec6e4d1179f898007da',
  sourceRepo: 'https://github.com/FiiLabs/sub2api.git',
  sourceCommit: '4394826be9fba3a12b093504bee494df348ceee5',
  images: [
    {
      name: 'sub2api',
      digest:
        'docker.io/markerdao/sub2api@sha256:83c3f6f88999acca55ac9e72205a39521d76b83df4073aaca98e6fc9ac7d8adc',
    },
    {
      name: 'sub2api-attestor',
      digest:
        'docker.io/markerdao/sub2api-attestor@sha256:f5ddd0c235588f0d799cd5d50cf6476a04e51fbfcba4c17a4190a6f26d8215ae',
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
 *
 * The branch here MUST be the branch the deployed image is built from.
 * It used to point at `proof-solo` while images were built from
 * `feat/two-sided-market`, which meant /proof read its reference from a branch
 * nobody was deploying — the two only stayed in sync because someone remembered
 * to commit the same file twice. A reference that silently describes a
 * different build is worse than no reference at all: it reports a mismatch as
 * a *failed enclave*, i.e. it accuses a healthy deployment of being tampered
 * with. If you move the release branch, move this too.
 */
export const REFERENCE_JSON_URL: string =
  (import.meta.env.VITE_REFERENCE_JSON_URL as string | undefined) ||
  'https://raw.githubusercontent.com/FiiLabs/sub2api/feat/two-sided-market/deploy/phala/reference.json'

/**
 * Mirrors of the same file, tried in order when the authoritative URL is
 * unreachable (raw.githubusercontent.com is blocked or rate-limited in some
 * networks). A CDN caches branch refs, so a mirror may lag the repo — mirrors
 * are therefore treated as non-authoritative: a measurement mismatch against
 * one is reported as undecided rather than as a failed enclave.
 */
export const REFERENCE_JSON_MIRROR_URLS: readonly string[] = (
  (import.meta.env.VITE_REFERENCE_JSON_MIRROR_URLS as string | undefined) ||
  'https://cdn.jsdelivr.net/gh/FiiLabs/sub2api@feat/two-sided-market/deploy/phala/reference.json'
)
  .split(',')
  .map((u) => u.trim())
  .filter(Boolean)
