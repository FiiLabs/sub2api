/**
 * Runtime loader for the published enclave reference.
 *
 * The authoritative reference lives in the public repo
 * (deploy/phala/reference.json) so that (a) updating pins doesn't require
 * rebuilding the measured image that embeds this frontend — breaking the
 * composeHash circularity — and (b) every change to the reference is public
 * git history.
 *
 * Sources are tried in descending order of trust:
 *   1. REFERENCE_JSON_URL       — authoritative, always current
 *   2. REFERENCE_JSON_MIRROR_URLS — CDN copies, may lag the branch ref
 *   3. SUB2API_REFERENCE        — baked into the measured image, always lags
 *
 * Only (1) is authoritative. A measurement mismatch against (2) or (3) means
 * "we could not check", not "the enclave is not what it claims" — the caller
 * passes `authoritative` through to verifyEnclaveQuote so a stale reference
 * can never masquerade as a tampered enclave.
 */
import {
  REFERENCE_JSON_URL,
  REFERENCE_JSON_MIRROR_URLS,
  SUB2API_REFERENCE,
  type EnclaveReference,
} from '@/constants/attestation'

export interface LoadedReference {
  reference: EnclaveReference
  /** Where the reference came from — shown to the user for transparency. */
  source: 'repo' | 'mirror' | 'baked-in'
  /** True only for `source === 'repo'`: this copy is guaranteed current. */
  authoritative: boolean
  /** The URL fetched when source is 'repo' or 'mirror'. */
  url?: string
  /**
   * Attestor base URL published alongside the reference. Published at runtime
   * so moving the service to a new CVM (new app-id / node) only needs a
   * reference.json edit, not an image rebuild. Absent -> caller falls back to
   * the baked-in ATTESTOR_BASE_URL.
   */
  attestorBaseUrl?: string
}

interface ReferenceJson {
  version: number
  enclave: EnclaveReference
  attestor?: { baseUrl?: string; quotePath?: string }
}

function isValidReference(v: unknown): v is EnclaveReference {
  const r = v as EnclaveReference | null
  return (
    !!r &&
    typeof r.appId === 'string' &&
    /^[0-9a-f]{40}$/i.test(r.appId) &&
    typeof r.osImageHash === 'string' &&
    /^[0-9a-f]{64}$/i.test(r.osImageHash) &&
    typeof r.composeHash === 'string' &&
    /^[0-9a-f]{64}$/i.test(r.composeHash)
  )
}

async function fetchFrom(
  url: string,
  source: 'repo' | 'mirror',
  fetchImpl: typeof fetch,
): Promise<LoadedReference | undefined> {
  try {
    const res = await fetchImpl(url, { cache: 'no-store' })
    if (!res.ok) return undefined
    const body = (await res.json()) as ReferenceJson
    if (body?.version !== 1 || !isValidReference(body.enclave)) return undefined
    const rawBase = body.attestor?.baseUrl
    const attestorBaseUrl =
      typeof rawBase === 'string' && /^https:\/\/[^\s]+$/.test(rawBase)
        ? rawBase.replace(/\/+$/, '')
        : undefined
    return {
      reference: body.enclave,
      source,
      authoritative: source === 'repo',
      url,
      attestorBaseUrl,
    }
  } catch {
    return undefined
  }
}

export async function loadReference(fetchImpl: typeof fetch = fetch): Promise<LoadedReference> {
  const primary = await fetchFrom(REFERENCE_JSON_URL, 'repo', fetchImpl)
  if (primary) return primary
  for (const url of REFERENCE_JSON_MIRROR_URLS) {
    const mirrored = await fetchFrom(url, 'mirror', fetchImpl)
    if (mirrored) return mirrored
  }
  return { reference: SUB2API_REFERENCE, source: 'baked-in', authoritative: false }
}
