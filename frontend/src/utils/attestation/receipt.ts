/**
 * ACI receipt verification — pure-TS port of the receipt half of
 * `examples/verify_aci_artifacts.rs` (which mirrors §9.4 and
 * `canonical_bytes_for_signing`).
 *
 * Checks: receipt identity matches the report; the signature key_id is in the
 * attested `receipt_signing_keys`; the signature covers
 * `canonical_bytes_for_signing`; and, when the caller supplies bodies,
 * body_hash == sha256(request) and cleartext/wire_hash == sha256(response).
 */
import type { AttestationReport } from '@/api/attestation'
import { canonicalBytesForSigning, decodeHex, sha256Hex } from './jcs'
import { verifyReceiptSignature } from './crypto'
import type { Check } from './reportBinding'

export interface ReceiptSignature {
  algo: string
  key_id: string
  value: string
}

export interface ReceiptEvent {
  seq: number | string
  type: string
  fields?: Record<string, unknown>
  [k: string]: unknown
}

export interface AciReceipt {
  api_version: string
  receipt_id: string
  chat_id?: string | null
  workload_id: string
  workload_keyset_digest: string
  endpoint: string
  method: string
  served_at: number | string
  event_log: ReceiptEvent[]
  signature: ReceiptSignature
}

export interface VerifyReceiptOptions {
  requestBody?: Uint8Array | string
  responseBody?: Uint8Array | string
}

export interface ReceiptVerificationResult {
  ok: boolean
  checks: Check[]
}

const EVENT_REQUEST_RECEIVED = 'request.received'
const EVENT_RESPONSE_RETURNED = 'response.returned'

function toBytes(body: Uint8Array | string): Uint8Array {
  return typeof body === 'string' ? new TextEncoder().encode(body) : body
}

function findEvent(receipt: AciReceipt, type: string): ReceiptEvent | undefined {
  return (receipt.event_log ?? []).find((e) => e.type === type)
}

/** Read a field from an event in either flattened or nested (`fields`) shape. */
function eventField(ev: ReceiptEvent, name: string): unknown {
  if (ev.fields && typeof ev.fields === 'object') return ev.fields[name]
  return ev[name]
}

export function verifyReceipt(
  report: AttestationReport,
  receipt: AciReceipt,
  options: VerifyReceiptOptions = {},
): ReceiptVerificationResult {
  const checks: Check[] = []
  const push = (id: string, ok: boolean, detail?: string) => checks.push({ id, ok, detail })

  // Identity: receipt commits to the same workload as the report.
  const identityMatch =
    receipt.workload_id === report.workload_id &&
    receipt.workload_keyset_digest === report.workload_keyset_digest
  push('identity_match', identityMatch)

  // Signature key_id must be one of the attested receipt signing keys.
  const signingKeys = report.attestation?.workload_keyset?.receipt_signing_keys ?? []
  const receiptKey = signingKeys.find((k) => k.key_id === receipt.signature?.key_id)
  push('signature_key_known', !!receiptKey, `key_id=${receipt.signature?.key_id}`)

  // Signature over canonical_bytes_for_signing (receipt minus signature.value).
  try {
    if (!receiptKey) throw new Error('signature key_id not in attested keyset')
    const canonical = canonicalBytesForSigning(receipt)
    const sig = decodeHex(receipt.signature.value ?? '')
    push('receipt_signature', verifyReceiptSignature(receiptKey, canonical, sig))
  } catch (e) {
    push('receipt_signature', false, String(e))
  }

  // Optional: request body_hash == sha256(request).
  if (options.requestBody !== undefined) {
    try {
      const expected = sha256Hex(toBytes(options.requestBody))
      const ev = findEvent(receipt, EVENT_REQUEST_RECEIVED)
      if (!ev) throw new Error('receipt missing request.received event')
      push('request_body_hash', eventField(ev, 'body_hash') === expected, `expected=${expected}`)
    } catch (e) {
      push('request_body_hash', false, String(e))
    }
  }

  // Optional: response cleartext_hash or wire_hash == sha256(response).
  if (options.responseBody !== undefined) {
    try {
      const expected = sha256Hex(toBytes(options.responseBody))
      const ev = findEvent(receipt, EVENT_RESPONSE_RETURNED)
      if (!ev) throw new Error('receipt missing response.returned event')
      const cleartext = eventField(ev, 'cleartext_hash')
      const wire = eventField(ev, 'wire_hash')
      push('response_body_hash', cleartext === expected || wire === expected, `expected=${expected}`)
    } catch (e) {
      push('response_body_hash', false, String(e))
    }
  }

  return { ok: checks.every((c) => c.ok), checks }
}
