/**
 * Attestation API — fetches the live TDX attestation report from the TEE
 * gateway (enclave A). This is a DIFFERENT origin than sub2api; it hits the
 * gateway directly (see ATTESTATION_BASE_URL) and depends on gateway CORS.
 *
 * The report is self-reported until its TDX quote is cryptographically
 * verified. Browser-local DCAP + receipt verification lives in a separate
 * module (see @/utils/attestation); this file only transports the raw report.
 */
import axios from 'axios'
import JSONBig from 'json-bigint'
import {
  ATTESTATION_BASE_URL,
  ATTESTATION_REPORT_PATH,
  MERIDIAN_ATTESTATION_URL,
  MERIDIAN_ATTESTATION_PATH,
  INFERENCE_CHAT_PATH,
} from '@/constants/attestation'

// Lossless JSON parse: the report carries u64 fields (e.g. keyset_epoch.not_after
// = u64::MAX) that JS's JSON.parse rounds to a different integer, which would
// change the recomputed ACI keyset digest and break every downstream binding
// check. Keep big integers as strings so the canonicalization (item 2a) — whose
// asCanonicalInt() accepts strings — reproduces the gateway's bytes exactly.
const losslessJson = JSONBig({ storeAsString: true })

const attestationClient = axios.create({
  baseURL: ATTESTATION_BASE_URL,
  timeout: 20000,
  headers: { Accept: 'application/json' },
  // Keep the raw text; we parse it losslessly ourselves (see losslessJson).
  responseType: 'text',
  transformResponse: (data) => data,
})

/** A public key entry in the workload keyset. */
export interface KeyedPublicKey {
  key_id?: string
  algo: string
  public_key: string // hex
}

export interface WorkloadKeyset {
  workload_identity: { public_key: { algo: string; public_key: string }; subject?: string | null }
  keyset_epoch?: { version?: string; not_after?: string }
  receipt_signing_keys?: KeyedPublicKey[]
  e2ee_public_keys?: KeyedPublicKey[]
  tls_public_keys?: { spki_sha256: string; domain?: string }[]
}

export interface AttestationEvidence {
  quote: string // hex-encoded raw TDX DCAP quote
  quote_report_data?: string // hex, 64 bytes
  event_log?: unknown
  vm_config?: unknown
  key_custody?: unknown
  downstream_tls_binding?: { domain: string; spki_sha256: string }
}

export interface AttestationBlock {
  vendor?: string
  tee_type?: string
  workload_keyset?: WorkloadKeyset
  report_data?: string // hex, 32-byte ACI digest (padded to 64 in the quote)
  keyset_endorsement?: { algo: string; value: string }
  source_provenance?: {
    repo_url?: string
    repo_commit?: string
    image_digest?: string
    image_provenance?: string
  }
  freshness?: { fetched_at?: string; stale_after?: string }
  evidence?: AttestationEvidence
}

/**
 * Raw attestation report — contract from private-ai-gateway @975ac50 (aci/1).
 * OS/image measurements are NOT top-level fields; they live inside the raw TDX
 * quote (MRTD/RTMRs). The legacy handler additionally wraps `signing_address`,
 * `request_nonce`, and `nvidia_payload`. Kept open with an index signature; we
 * render/verify defensively.
 */
export interface AttestationReport {
  api_version?: string
  workload_id?: string
  workload_keyset_digest?: string
  attestation?: AttestationBlock
  service_capabilities?: { supported_e2ee_versions?: string[] }
  // legacy wrapper fields
  signing_address?: string
  request_nonce?: string
  [k: string]: unknown
}

/** Random hex nonce (default 16 bytes), generated in-browser to bind freshness. */
export function randomNonce(byteLen = 16): string {
  const bytes = new Uint8Array(byteLen)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

export async function fetchAttestationReport(
  nonce: string = randomNonce(),
): Promise<{ nonce: string; report: AttestationReport; raw: string }> {
  const { data } = await attestationClient.get<string>(ATTESTATION_REPORT_PATH, {
    params: { nonce },
  })
  const raw = typeof data === 'string' ? data : JSON.stringify(data)
  const report = losslessJson.parse(raw) as AttestationReport
  return { nonce, report, raw }
}

/** Meridian enclave-B nonce-bound TDX quote + dstack event log (hop-3). */
export interface MeridianQuoteResponse {
  quote: string // hex raw TDX DCAP quote (report_data = nonce||zeros)
  event_log: unknown // dstack event log — a JSON string (or array) of events
  vm_config?: unknown
}

const meridianClient = axios.create({
  baseURL: MERIDIAN_ATTESTATION_URL,
  timeout: 20000,
  headers: { Accept: 'application/json' },
})

/**
 * Fetch a fresh nonce-bound TDX quote from the Meridian enclave-B sidecar. Uses
 * a 32-byte nonce so it fully occupies the quote's report_data[0..32].
 */
export async function fetchMeridianQuote(
  nonce: string = randomNonce(32),
): Promise<{ nonce: string; response: MeridianQuoteResponse }> {
  const { data } = await meridianClient.get<MeridianQuoteResponse>(MERIDIAN_ATTESTATION_PATH, {
    params: { nonce },
  })
  return { nonce, response: data }
}

/** Result of an E2EE inference POST (Path A live round-trip). */
export interface E2eeChatResult {
  status: number
  ok: boolean
  /** Parsed JSON body, or undefined if the response was not JSON. */
  body: unknown
  rawText: string
}

/**
 * POST an E2EE-encrypted inference request directly to the gateway (enclave A),
 * cross-origin. The gateway CORS-allows `*` methods/headers, so the custom
 * `X-E2EE-*` headers + Authorization work from the browser. Uses `fetch` (not
 * the report axios client) since we need arbitrary request headers and only the
 * raw text/JSON back. NB: the encrypted prompt reaches Anthropic in plaintext
 * once decrypted inside the enclave — this is a deliberate, disclosed demo call.
 */
export async function postE2eeChat(
  headers: Record<string, string>,
  body: unknown,
  path: string = INFERENCE_CHAT_PATH,
): Promise<E2eeChatResult> {
  const resp = await fetch(`${ATTESTATION_BASE_URL}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...headers },
    body: JSON.stringify(body),
  })
  const rawText = await resp.text()
  let parsed: unknown
  try {
    parsed = JSON.parse(rawText)
  } catch {
    parsed = undefined
  }
  return { status: resp.status, ok: resp.ok, body: parsed, rawText }
}
