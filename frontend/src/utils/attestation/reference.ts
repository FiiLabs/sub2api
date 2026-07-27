/**
 * Runtime loader for the published enclave reference.
 *
 * The authoritative reference lives in the public repo
 * (deploy/phala/reference.json) so that (a) updating pins doesn't require
 * rebuilding the measured image that embeds this frontend — breaking the
 * composeHash circularity — and (b) every change to the reference is public
 * git history. Falls back to the baked-in SUB2API_REFERENCE when the fetch
 * fails (offline, rate-limited, etc.); the UI surfaces which source was used.
 */
import {
  REFERENCE_JSON_URL,
  SUB2API_REFERENCE,
  type EnclaveReference,
} from '@/constants/attestation'

export interface LoadedReference {
  reference: EnclaveReference
  /** Where the reference came from — shown to the user for transparency. */
  source: 'repo' | 'baked-in'
  /** The URL fetched when source === 'repo'. */
  url?: string
}

interface ReferenceJson {
  version: number
  enclave: EnclaveReference
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

export async function loadReference(fetchImpl: typeof fetch = fetch): Promise<LoadedReference> {
  try {
    const res = await fetchImpl(REFERENCE_JSON_URL, { cache: 'no-store' })
    if (res.ok) {
      const body = (await res.json()) as ReferenceJson
      if (body?.version === 1 && isValidReference(body.enclave)) {
        return { reference: body.enclave, source: 'repo', url: REFERENCE_JSON_URL }
      }
    }
  } catch {
    // fall through to the baked-in reference
  }
  return { reference: SUB2API_REFERENCE, source: 'baked-in' }
}
