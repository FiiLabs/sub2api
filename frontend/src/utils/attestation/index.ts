/**
 * ACI attestation verification core (item 2a) — orchestrator.
 *
 * Pure-TypeScript, byte-exact port of the ACI verification logic from
 * private-ai-gateway @975ac50 (`src/aci/*`, `examples/verify_aci_artifacts.rs`).
 * It validates the report identity binding and, optionally, a receipt.
 *
 * IMPORTANT: this core does NOT verify the hardware DCAP quote (item 2b). The
 * result always carries `hardwareQuoteVerified: false` so a caller can never
 * render "attestation verified" from ACI checks alone — the quote binding that
 * proves the report_data actually came from TEE hardware is a separate step.
 * The 32-byte `reportDataDigestHex` is the hand-off point for that check.
 */
import type { AttestationReport } from '@/api/attestation'
import {
  validateReportBinding,
  type Check,
  type ReportBindingResult,
} from './reportBinding'
import { verifyReceipt, type AciReceipt, type ReceiptVerificationResult } from './receipt'

export interface VerifyAciOptions {
  /** Nonce string sent to the gateway (binds report freshness). */
  nonce?: string
  /** Optional receipt to verify against the report. */
  receipt?: AciReceipt
  /** Optional request body to check against the receipt's request.received hash. */
  requestBody?: Uint8Array | string
  /** Optional response body to check against the receipt's response.returned hash. */
  responseBody?: Uint8Array | string
  /** Verifier clock override (seconds since epoch); defaults to now. */
  nowSecs?: number
}

export interface VerifyAciResult {
  /** True iff every ACI-level check passed. NOT hardware-verified — see below. */
  ok: boolean
  /**
   * Always false: DCAP hardware quote verification (item 2b) is not performed
   * by this core. Callers MUST gate any "verified" UI on this being true.
   */
  hardwareQuoteVerified: false
  hardwareQuoteNote: string
  /** 32-byte ACI report_data digest (hex) — must equal the quote's report_data[0..32]. */
  reportDataDigestHex: string
  workloadId: string
  keysetDigest: string
  binding: ReportBindingResult
  receipt: ReceiptVerificationResult | null
  /** Flattened, namespaced per-check results for display. */
  checks: Check[]
}

const HARDWARE_QUOTE_NOTE =
  'ACI identity/receipt binding only. The DCAP hardware quote (item 2b) is NOT ' +
  'verified here: this does not yet prove report_data originated from genuine ' +
  'TEE hardware. Gate any "verified" state on a separate quote check that binds ' +
  'reportDataDigestHex to the quote report_data.'

/**
 * Verify an ACI attestation report (and optional receipt) end to end at the
 * ACI protocol level. Returns per-check results plus the explicit
 * `hardwareQuoteVerified: false` guard.
 */
export function verifyAci(report: AttestationReport, options: VerifyAciOptions = {}): VerifyAciResult {
  const binding = validateReportBinding(report, options.nonce, { nowSecs: options.nowSecs })

  let receiptResult: ReceiptVerificationResult | null = null
  if (options.receipt) {
    receiptResult = verifyReceipt(report, options.receipt, {
      requestBody: options.requestBody,
      responseBody: options.responseBody,
    })
  }

  const checks: Check[] = [
    ...binding.checks.map((c) => ({ ...c, id: `report.${c.id}` })),
    ...(receiptResult ? receiptResult.checks.map((c) => ({ ...c, id: `receipt.${c.id}` })) : []),
  ]

  const ok = binding.ok && (receiptResult ? receiptResult.ok : true)

  return {
    ok,
    hardwareQuoteVerified: false,
    hardwareQuoteNote: HARDWARE_QUOTE_NOTE,
    reportDataDigestHex: binding.reportDataDigestHex,
    workloadId: binding.workloadId,
    keysetDigest: binding.keysetDigest,
    binding,
    receipt: receiptResult,
    checks,
  }
}

export { validateReportBinding, verifyReceipt }
export type { Check, ReportBindingResult, ReceiptVerificationResult, AciReceipt }
export {
  canonicalize,
  sha256Hex,
  jcsSha256Hex,
  jcsSha256Raw,
  canonicalBytesForSigning,
} from './jcs'
