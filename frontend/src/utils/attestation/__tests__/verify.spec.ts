/**
 * End-to-end ACI verification tests against Rust ground truth (vectors.json):
 * signature primitives, report binding, receipt verification, and the
 * orchestrator — plus tamper cases that must fail closed.
 */
import { describe, it, expect } from 'vitest'
import { ed25519 } from '@noble/curves/ed25519.js'
import { hexToBytes, bytesToHex } from '@noble/hashes/utils.js'
import {
  verifyKeysetEndorsement,
  verifyReceiptSignature,
  ethereumAddressFromUncompressedPublicKey,
} from '../crypto'
import { canonicalize, decodeHex, keysetEndorsementPayloadCanonical } from '../jcs'
import { validateReportBinding } from '../reportBinding'
import { verifyReceipt, type AciReceipt } from '../receipt'
import { verifyAci } from '../index'
import type { AttestationReport } from '@/api/attestation'
import vectors from './vectors.json'

const report = vectors.report as unknown as AttestationReport
// The report is fresh in [1000, 4102444800); pin the clock inside that window.
const NOW = 2000

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v))
}

describe('signature primitives (crypto.ts)', () => {
  const identity = vectors.report.attestation.workload_keyset.workload_identity.public_key
  const signingKeys = vectors.report.attestation.workload_keyset.receipt_signing_keys

  it('verifies the ecdsa-secp256k1 keyset endorsement (64-byte r||s)', () => {
    const payload = canonicalize(keysetEndorsementPayloadCanonical(vectors.keysetDigest))
    const sig = hexToBytes(vectors.endorsementSigHex)
    expect(verifyKeysetEndorsement(identity, payload, sig)).toBe(true)
  })

  it('rejects a tampered endorsement payload', () => {
    const payload = canonicalize(keysetEndorsementPayloadCanonical(vectors.keysetDigest + '00'))
    const sig = hexToBytes(vectors.endorsementSigHex)
    expect(verifyKeysetEndorsement(identity, payload, sig)).toBe(false)
  })

  it('verifies the secp256k1 receipt signature (65-byte recoverable)', () => {
    const r = vectors.receipts[0]
    const key = signingKeys[0]
    expect(
      verifyReceiptSignature(key, hexToBytes(r.canonicalForSigningHex), hexToBytes(r.receipt.signature.value)),
    ).toBe(true)
  })

  it('rejects a 64-byte (JOSE ES256K) receipt signature for secp256k1', () => {
    const r = vectors.receipts[0]
    const key = signingKeys[0]
    const sig64 = hexToBytes(r.receipt.signature.value).slice(0, 64)
    expect(verifyReceiptSignature(key, hexToBytes(r.canonicalForSigningHex), sig64)).toBe(false)
  })

  it('verifies the ed25519 receipt signature (64-byte)', () => {
    const r = vectors.receipts[1]
    const key = signingKeys[1]
    expect(
      verifyReceiptSignature(key, hexToBytes(r.canonicalForSigningHex), hexToBytes(r.receipt.signature.value)),
    ).toBe(true)
  })

  it('round-trips an ed25519 keyset endorsement', () => {
    const sk = ed25519.utils.randomSecretKey()
    const pub = ed25519.getPublicKey(sk)
    const payload = canonicalize(keysetEndorsementPayloadCanonical(vectors.keysetDigest))
    const sig = ed25519.sign(payload, sk)
    const pk = { algo: 'ed25519', public_key: bytesToHex(pub) }
    expect(verifyKeysetEndorsement(pk, payload, sig)).toBe(true)
    expect(verifyKeysetEndorsement(pk, canonicalize({ x: 1 }), sig)).toBe(false)
  })

  it('derives the legacy ethereum signing address (keccak256)', () => {
    expect(ethereumAddressFromUncompressedPublicKey(vectors.identityPubHex)).toBe(vectors.ethAddress)
  })
})

describe('validateReportBinding', () => {
  it('accepts the reference report (all checks pass)', () => {
    const res = validateReportBinding(report, vectors.nonce, { nowSecs: NOW })
    expect(res.ok).toBe(true)
    expect(res.checks.every((c) => c.ok)).toBe(true)
    expect(res.workloadId).toBe(vectors.workloadId)
    expect(res.keysetDigest).toBe(vectors.keysetDigest)
  })

  it('exposes reportDataDigestHex (2b hand-off) = quote report_data[0..32]', () => {
    const res = validateReportBinding(report, vectors.nonce, { nowSecs: NOW })
    expect(res.reportDataDigestHex).toBe(vectors.reportDataHex)
    // The 64-byte quote report_data begins with the 32-byte ACI digest.
    const quoteReportData = report.attestation!.evidence!.quote_report_data as string
    expect(quoteReportData.slice(0, 64)).toBe(res.reportDataDigestHex)
  })

  it('fails report_data on a wrong nonce', () => {
    const res = validateReportBinding(report, 'wrong-nonce', { nowSecs: NOW })
    expect(res.ok).toBe(false)
    expect(res.checks.find((c) => c.id === 'report_data')!.ok).toBe(false)
  })

  it('fails freshness outside the validity window', () => {
    const early = validateReportBinding(report, vectors.nonce, { nowSecs: 500 })
    expect(early.checks.find((c) => c.id === 'freshness')!.ok).toBe(false)
    const late = validateReportBinding(report, vectors.nonce, { nowSecs: 5_000_000_000 })
    expect(late.checks.find((c) => c.id === 'freshness')!.ok).toBe(false)
  })

  it('tolerates a small clock skew below fetched_at (window [1000, …), ±300s)', () => {
    // 200s before fetched_at is within tolerance → freshness still passes, no skew flag.
    const res = validateReportBinding(report, vectors.nonce, { nowSecs: 800 })
    expect(res.checks.find((c) => c.id === 'freshness')!.ok).toBe(true)
    expect(res.clockSkew).toBeUndefined()
  })

  it('reports a clock skew (not a hard fail) when only freshness is out of tolerance', () => {
    // 500s before fetched_at (=1000) exceeds the ±300s tolerance. Every other check
    // still passes, so this is the visitor's device clock, not the TEE.
    const res = validateReportBinding(report, vectors.nonce, { nowSecs: 500 })
    expect(res.checks.find((c) => c.id === 'freshness')!.ok).toBe(false)
    const failing = res.checks.filter((c) => !c.ok)
    expect(failing.map((c) => c.id)).toEqual(['freshness'])
    expect(res.clockSkew).toBeDefined()
    expect(res.clockSkew!.offsetSeconds).toBe(-500) // now(500) - fetched_at(1000)
    expect(res.clockSkew!.fetchedAt).toBe(1000)
  })

  it('fails workload_keyset_digest if the keyset is tampered', () => {
    const bad = clone(report)
    bad.attestation!.workload_keyset!.keyset_epoch = { version: 2, not_after: 4102444800 } as never
    const res = validateReportBinding(bad, vectors.nonce, { nowSecs: NOW })
    expect(res.ok).toBe(false)
    expect(res.checks.find((c) => c.id === 'workload_keyset_digest')!.ok).toBe(false)
  })

  it('fails the endorsement signature if it is tampered', () => {
    const bad = clone(report)
    const sig = hexToBytes(vectors.endorsementSigHex)
    sig[0] ^= 0xff
    bad.attestation!.keyset_endorsement!.value = bytesToHex(sig)
    const res = validateReportBinding(bad, vectors.nonce, { nowSecs: NOW })
    expect(res.checks.find((c) => c.id === 'keyset_endorsement_signature')!.ok).toBe(false)
  })

  it('rejects an unsupported api_version', () => {
    const bad = clone(report)
    bad.api_version = 'aci/2'
    const res = validateReportBinding(bad, vectors.nonce, { nowSecs: NOW })
    expect(res.ok).toBe(false)
    expect(res.checks.find((c) => c.id === 'api_version')!.ok).toBe(false)
  })
})

describe('verifyReceipt', () => {
  const requestBody = vectors.requestBodyUtf8
  const responseBody = vectors.responseBodyUtf8

  for (const r of vectors.receipts) {
    it(`accepts the ${r.algo} receipt with matching bodies`, () => {
      const res = verifyReceipt(report, r.receipt as unknown as AciReceipt, { requestBody, responseBody })
      expect(res.ok).toBe(true)
      expect(res.checks.find((c) => c.id === 'request_body_hash')!.ok).toBe(true)
      expect(res.checks.find((c) => c.id === 'response_body_hash')!.ok).toBe(true)
    })
  }

  it('fails request_body_hash on a mismatched request body', () => {
    const res = verifyReceipt(report, vectors.receipts[0].receipt as unknown as AciReceipt, {
      requestBody: 'tampered',
    })
    expect(res.ok).toBe(false)
    expect(res.checks.find((c) => c.id === 'request_body_hash')!.ok).toBe(false)
  })

  it('fails on an unknown signature key_id', () => {
    const bad = clone(vectors.receipts[0].receipt) as unknown as AciReceipt
    bad.signature.key_id = 'not-a-real-key'
    const res = verifyReceipt(report, bad)
    expect(res.checks.find((c) => c.id === 'signature_key_known')!.ok).toBe(false)
  })

  it('fails the signature if the event log is tampered', () => {
    const bad = clone(vectors.receipts[0].receipt) as unknown as AciReceipt
    ;(bad.event_log[0] as Record<string, unknown>).body_hash = 'sha256:' + '00'.repeat(32)
    const res = verifyReceipt(report, bad)
    expect(res.checks.find((c) => c.id === 'receipt_signature')!.ok).toBe(false)
  })

  it('fails identity_match if the receipt names a different workload', () => {
    const bad = clone(vectors.receipts[0].receipt) as unknown as AciReceipt
    bad.workload_id = 'sha256:' + '11'.repeat(32)
    const res = verifyReceipt(report, bad)
    expect(res.checks.find((c) => c.id === 'identity_match')!.ok).toBe(false)
  })
})

describe('verifyAci orchestrator', () => {
  it('passes ACI checks but never claims hardware verification', () => {
    const res = verifyAci(report, {
      nonce: vectors.nonce,
      receipt: vectors.receipts[0].receipt as unknown as AciReceipt,
      requestBody: vectors.requestBodyUtf8,
      responseBody: vectors.responseBodyUtf8,
      nowSecs: NOW,
    })
    expect(res.ok).toBe(true)
    expect(res.hardwareQuoteVerified).toBe(false)
    expect(res.reportDataDigestHex).toBe(vectors.reportDataHex)
    // namespaced checks present for both report and receipt
    expect(res.checks.some((c) => c.id.startsWith('report.'))).toBe(true)
    expect(res.checks.some((c) => c.id.startsWith('receipt.'))).toBe(true)
  })

  it('works without a receipt (binding only)', () => {
    const res = verifyAci(report, { nonce: vectors.nonce, nowSecs: NOW })
    expect(res.ok).toBe(true)
    expect(res.receipt).toBeNull()
    expect(res.hardwareQuoteVerified).toBe(false)
  })

  it('overall ok is false when the receipt fails even if binding passes', () => {
    const badReceipt = clone(vectors.receipts[0].receipt) as unknown as AciReceipt
    badReceipt.signature.value = '00'.repeat(65)
    const res = verifyAci(report, { nonce: vectors.nonce, receipt: badReceipt, nowSecs: NOW })
    expect(res.binding.ok).toBe(true)
    expect(res.ok).toBe(false)
  })
})

// Guard: decodeHex strips 0x, matching Rust decode_hex.
describe('decodeHex', () => {
  it('strips a 0x prefix', () => {
    expect(bytesToHex(decodeHex('0xdeadbeef'))).toBe('deadbeef')
    expect(bytesToHex(decodeHex('deadbeef'))).toBe('deadbeef')
  })
})
