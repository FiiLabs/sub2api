/**
 * ACI attestation-report binding validation — pure-TS port of
 * `validate_aci_report_binding` (`src/aci/verifier/report.rs`).
 *
 * Recomputes workload_id, workload_keyset_digest and the nonce-bound
 * report_data, verifies the identity-key keyset endorsement, and checks
 * freshness. It deliberately does NOT verify the vendor DCAP quote — that is
 * item 2b, done separately. The 32-byte ACI `reportDataDigestHex` is exposed as
 * the interface point for that hardware check: it must equal the quote's
 * report_data[0..32].
 */
import type { AttestationReport } from '@/api/attestation'
import {
  attestationStatementCanonical,
  bytesToHex,
  canonicalize,
  decodeHex,
  jcsSha256Hex,
  jcsSha256Raw,
  keysetEndorsementPayloadCanonical,
  publicKeyMaterialCanonical,
  workloadKeysetCanonical,
} from './jcs'
import { verifyKeysetEndorsement } from './crypto'

/** A single named verification step. */
export interface Check {
  id: string
  ok: boolean
  detail?: string
}

export interface ReportBindingResult {
  ok: boolean
  checks: Check[]
  /** hex of the 32-byte ACI report_data digest (no `sha256:` prefix). */
  reportDataDigestHex: string
  workloadId: string
  keysetDigest: string
}

export interface ReportBindingOptions {
  /** Verifier wall-clock, seconds since epoch. Defaults to `Date.now()`. */
  nowSecs?: number
}

const API_VERSION = 'aci/1'

function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false
  return true
}

/**
 * Validate the ACI identity binding inside an attestation report.
 *
 * @param report      the raw attestation report (aci/1)
 * @param nonce       the nonce string exactly as sent to the gateway (the
 *                    hex nonce from `randomNonce()`), or undefined if none
 * @param options     optional verifier clock override
 */
export function validateReportBinding(
  report: AttestationReport,
  nonce?: string,
  options: ReportBindingOptions = {},
): ReportBindingResult {
  const checks: Check[] = []
  const push = (id: string, ok: boolean, detail?: string) => checks.push({ id, ok, detail })

  const attestation = report.attestation
  const keyset = attestation?.workload_keyset
  const identityKey = keyset?.workload_identity?.public_key

  let workloadId = ''
  let keysetDigest = ''
  let reportDataDigestHex = ''
  let expectedReportData: Uint8Array | undefined

  // api_version
  push('api_version', report.api_version === API_VERSION, `api_version=${report.api_version}`)

  if (!attestation || !keyset || !identityKey) {
    push('structure', false, 'report is missing attestation.workload_keyset.workload_identity')
    return { ok: false, checks, reportDataDigestHex, workloadId, keysetDigest }
  }

  // Recompute identity + keyset digests (workload_id / workload_keyset_digest).
  try {
    workloadId = jcsSha256Hex(publicKeyMaterialCanonical(identityKey))
    push('workload_id', workloadId === report.workload_id, `computed=${workloadId}`)
  } catch (e) {
    push('workload_id', false, String(e))
  }
  try {
    keysetDigest = jcsSha256Hex(workloadKeysetCanonical(keyset))
    push('workload_keyset_digest', keysetDigest === report.workload_keyset_digest, `computed=${keysetDigest}`)
  } catch (e) {
    push('workload_keyset_digest', false, String(e))
  }

  // report_data = sha256(JCS(statement{purpose, workload_id, keyset_digest, nonce})).
  try {
    expectedReportData = jcsSha256Raw(
      attestationStatementCanonical(workloadId, keysetDigest, nonce),
    )
    reportDataDigestHex = bytesToHex(expectedReportData)
    const reported = decodeHex(attestation.report_data ?? '')
    const ok = reported.length === 32 && bytesEqual(reported, expectedReportData)
    push('report_data', ok, ok ? undefined : `expected=${reportDataDigestHex}`)
  } catch (e) {
    push('report_data', false, String(e))
  }

  // keyset endorsement: algo must match identity algo, signature over
  // JCS({purpose: aci.keyset.endorsement.v1, workload_keyset_digest}).
  const endorsement = attestation.keyset_endorsement
  push(
    'keyset_endorsement_algo',
    !!endorsement && endorsement.algo === identityKey.algo,
    endorsement ? `endorsement.algo=${endorsement.algo} identity.algo=${identityKey.algo}` : 'no keyset_endorsement',
  )
  try {
    if (!endorsement) throw new Error('no keyset_endorsement')
    const payload = canonicalize(keysetEndorsementPayloadCanonical(keysetDigest))
    const sig = decodeHex(endorsement.value ?? '')
    push('keyset_endorsement_signature', verifyKeysetEndorsement(identityKey, payload, sig))
  } catch (e) {
    push('keyset_endorsement_signature', false, String(e))
  }

  // freshness: fetched_at <= now < stale_after (matches report.rs).
  try {
    const nowSecs = options.nowSecs ?? Math.floor(Date.now() / 1000)
    const fetchedAt = Number(attestation.freshness?.fetched_at)
    const staleAfter = Number(attestation.freshness?.stale_after)
    const ok =
      Number.isFinite(fetchedAt) &&
      Number.isFinite(staleAfter) &&
      nowSecs >= fetchedAt &&
      nowSecs < staleAfter
    push('freshness', ok, `now=${nowSecs} fetched_at=${fetchedAt} stale_after=${staleAfter}`)
  } catch (e) {
    push('freshness', false, String(e))
  }

  const ok = checks.every((c) => c.ok)
  return { ok, checks, reportDataDigestHex, workloadId, keysetDigest }
}
