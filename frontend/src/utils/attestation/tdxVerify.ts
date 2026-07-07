/**
 * Item 2b — browser-side Intel TDX DCAP hardware-quote verification.
 *
 * This is the hardware root of trust that lets the /proof page mark steps
 * "verified". It wraps `@phala/dcap-qvl-web` (the SAME audited `dcap-qvl` Rust
 * core the gateway uses server-side, compiled to wasm) — crypto is NOT
 * reimplemented here. Collateral (Intel PCK chain / TCB / QE identity) is
 * fetched from Phala PCCS directly in the browser.
 *
 * The verification policy mirrors private-ai-gateway @975ac50
 * `src/aci/verifier/dcap.rs` + `dstack.rs`:
 *   1. `js_verify` proves the quote is genuine TDX hardware and reports its TCB
 *      posture (this is the audited signature-chain + collateral check).
 *   2. report_data binding: the quote's 64-byte report_data must carry the ACI
 *      digest (`reportDataDigestHex`, from item 2a) in bytes [0..32] and zeros
 *      in [32..64] — this ties the ACI keyset to genuine hardware.
 *   3. measurement policy: the dstack event log is replayed into RTMR3 and
 *      compared to the quote's rt_mr3 (proving the log is authentic); each
 *      imr==3 event's digest is recomputed with dstack's algorithm (binding
 *      each event payload into RTMR3); then compose-hash / os-image-hash /
 *      app-id events are checked against the published reference. compose_hash
 *      is additionally cross-checked against the `mr_config_id` register, which
 *      dstack sets to `01 || compose_hash || 0*`.
 *
 * Honesty note: the raw MRTD / RTMR0-2 boot registers have NO published
 * reference value (the published `osImageHash` is dstack's OS-image-hash event,
 * not the firmware MRTD), so they are exposed raw and reported as
 * `unverified` rather than faked green — OS-image identity is instead verified
 * transitively via the anchored `os-image-hash` event.
 *
 * This module is standalone; wiring into ProofView / index.ts is done by the
 * caller. It imports `reportDataDigestHex` only as a value (hex string).
 */
import { sha384 } from '@noble/hashes/sha2.js'
import type { AttestationReport } from '@/api/attestation'
import { GATEWAY_REFERENCE, MERIDIAN_REFERENCE, type EnclaveReference } from '@/constants/attestation'
import { bytesToHex, decodeHex } from './jcs'
import type { Check } from './reportBinding'

/** Default Phala PCCS endpoint (verified CORS-clean for browser fetches). */
export const PHALA_PCCS_URL = 'https://pccs.phala.network'

/** TCB statuses accepted as a clean hardware pass by default. */
export const DEFAULT_ACCEPTABLE_TCB_STATUSES = ['UpToDate'] as const

// --- wasm module typing (from @phala/dcap-qvl-web 0.3.3 .d.ts) ---------------

type WasmInitInput = BufferSource | WebAssembly.Module | string | URL | Request | Response
interface DcapQvlWasm {
  default: (arg?: { module_or_path: WasmInitInput } | WasmInitInput) => Promise<unknown>
  js_get_collateral: (pccsUrl: string, rawQuote: Uint8Array) => Promise<unknown>
  js_verify: (rawQuote: Uint8Array, collateral: unknown, now: bigint) => VerifyResult
}

/** Raw shape returned by `js_verify` (mirrors dcap-qvl `VerifiedReport`). */
export interface VerifyResult {
  status: string
  advisory_ids: string[]
  report: RawReportEnum
}

/** dcap-qvl `Report` enum, tagged by variant name. TD15 nests TD10 under `base`. */
interface RawTd10 {
  mr_td: string
  mr_config_id: string
  rt_mr0: string
  rt_mr1: string
  rt_mr2: string
  rt_mr3: string
  report_data: string
  [k: string]: unknown
}
interface RawReportEnum {
  TD10?: RawTd10
  TD15?: { base: RawTd10; [k: string]: unknown }
  SgxEnclave?: { report_data: string; [k: string]: unknown }
}

// --- lazy wasm init ----------------------------------------------------------

let wasmPromise: Promise<DcapQvlWasm> | null = null
let wasmInputOverride: BufferSource | WebAssembly.Module | undefined

/**
 * Test-only hook: pre-seed the wasm bytes so init can run under Node/vitest
 * (where the `?url` asset resolves to a filesystem path that `fetch` can't
 * load). Never called by production code — the browser path uses `?url`.
 */
export function __setWasmInputForTests(input: BufferSource | WebAssembly.Module | undefined): void {
  wasmInputOverride = input
  wasmPromise = null
}

async function loadWasm(): Promise<DcapQvlWasm> {
  if (!wasmPromise) {
    wasmPromise = (async () => {
      const mod = (await import('@phala/dcap-qvl-web')) as unknown as DcapQvlWasm
      const input =
        wasmInputOverride ??
        (await import('@phala/dcap-qvl-web/dcap-qvl-web_bg.wasm?url')).default
      await mod.default({ module_or_path: input })
      return mod
    })()
  }
  return wasmPromise
}

// --- register extraction -----------------------------------------------------

export type ReportKind = 'TD10' | 'TD15' | 'SgxEnclave'

/** Normalized quote registers (hex, no `0x`), independent of TD10/TD15 nesting. */
export interface TdxRegisters {
  kind: ReportKind
  reportDataHex: string
  mrTdHex?: string
  mrConfigIdHex?: string
  rtMr0Hex?: string
  rtMr1Hex?: string
  rtMr2Hex?: string
  rtMr3Hex?: string
}

export function extractRegisters(report: RawReportEnum): TdxRegisters {
  const td = report.TD10 ?? report.TD15?.base
  if (td) {
    return {
      kind: report.TD15 ? 'TD15' : 'TD10',
      reportDataHex: normHex(td.report_data),
      mrTdHex: normHex(td.mr_td),
      mrConfigIdHex: normHex(td.mr_config_id),
      rtMr0Hex: normHex(td.rt_mr0),
      rtMr1Hex: normHex(td.rt_mr1),
      rtMr2Hex: normHex(td.rt_mr2),
      rtMr3Hex: normHex(td.rt_mr3),
    }
  }
  if (report.SgxEnclave) {
    return { kind: 'SgxEnclave', reportDataHex: normHex(report.SgxEnclave.report_data) }
  }
  throw new Error('unrecognized dcap-qvl report variant (expected TD10/TD15/SgxEnclave)')
}

function normHex(v: unknown): string {
  return typeof v === 'string' ? v.replace(/^0x/i, '').toLowerCase() : ''
}

// --- top-level quote verify --------------------------------------------------

export interface VerifyTdxQuoteOptions {
  /** PCCS base URL for collateral. Defaults to Phala PCCS. */
  pccsUrl?: string
  /** Verifier clock (seconds since epoch). Defaults to now. */
  nowSecs?: number
}

export interface VerifyTdxQuoteResult {
  /** True iff the quote is genuine TDX hardware (signature chain + collateral). */
  verified: boolean
  /** TCB posture, e.g. "UpToDate", "OutOfDate", "ConfigurationNeeded". */
  tcbStatus: string
  advisoryIds: string[]
  /** Normalized quote registers. */
  report: TdxRegisters
  /** Raw `js_verify` result. */
  raw: VerifyResult
}

/**
 * Verify a raw TDX DCAP quote against Intel collateral fetched from PCCS.
 *
 * Reaching a result means the quote's signature chain verified against the
 * Intel PCK/TCB/QE collateral (i.e. genuine hardware); `js_verify` throws
 * otherwise. `tcbStatus` conveys the TCB level separately.
 */
export async function verifyTdxQuote(
  quoteHex: string,
  opts: VerifyTdxQuoteOptions = {},
): Promise<VerifyTdxQuoteResult> {
  const wasm = await loadWasm()
  const quote = decodeHex(quoteHex)
  const pccsUrl = opts.pccsUrl ?? PHALA_PCCS_URL
  const collateral = await wasm.js_get_collateral(pccsUrl, quote)
  const nowSecs = opts.nowSecs ?? Math.floor(Date.now() / 1000)
  const raw = wasm.js_verify(quote, collateral, BigInt(nowSecs))
  return {
    verified: true,
    tcbStatus: raw.status,
    advisoryIds: raw.advisory_ids ?? [],
    report: extractRegisters(raw.report),
    raw,
  }
}

// --- report_data binding -----------------------------------------------------

/** Expected quote report_data for an ACI digest: `digest(32) || zeros(32)`, hex. */
export function expectedAciReportDataHex(reportDataDigestHex: string): string {
  const digest = normHex(reportDataDigestHex)
  if (digest.length !== 64) {
    throw new Error(`reportDataDigestHex must be 32 bytes (64 hex chars), got ${digest.length}`)
  }
  return digest + '00'.repeat(32)
}

/**
 * Check a 64-byte quote report_data binds the ACI digest: bytes [0..32] equal
 * the digest and bytes [32..64] are zero (mirrors `expected_dcap_report_data`).
 */
export function checkReportDataBinding(
  quoteReportDataHex: string,
  reportDataDigestHex: string,
): { ok: boolean; detail: string } {
  const actual = normHex(quoteReportDataHex)
  if (actual.length !== 128) {
    return { ok: false, detail: `quote report_data must be 64 bytes, got ${actual.length / 2}` }
  }
  const expected = expectedAciReportDataHex(reportDataDigestHex)
  const ok = actual === expected
  return {
    ok,
    detail: ok ? `report_data[0..32]=${normHex(reportDataDigestHex)}, [32..64]=0` : `expected=${expected} actual=${actual}`,
  }
}

// --- dstack event log --------------------------------------------------------

interface DstackEvent {
  imr: number
  event_type: number
  digest: string
  event: string
  event_payload: string
}

function parseEventLog(evidence: unknown): DstackEvent[] {
  const raw = (evidence as { event_log?: unknown })?.event_log
  const text = typeof raw === 'string' ? raw : JSON.stringify(raw ?? [])
  const parsed = JSON.parse(text) as DstackEvent[]
  if (!Array.isArray(parsed)) throw new Error('event_log is not an array')
  return parsed
}

/**
 * dstack RTMR event digest:
 *   sha384( u32le(event_type) || ":" || utf8(event) || ":" || event_payload )
 * (confirmed empirically against a live gateway quote's compose-hash event).
 */
export function dstackEventDigestHex(event: DstackEvent): string {
  const typeLe = new Uint8Array(4)
  new DataView(typeLe.buffer).setUint32(0, event.event_type >>> 0, true)
  const colon = new Uint8Array([0x3a])
  const name = new TextEncoder().encode(event.event)
  const payload = decodeHex(event.event_payload)
  return bytesToHex(sha384(concatBytes(typeLe, colon, name, colon, payload)))
}

/**
 * Replay an IMR by folding the recorded event digests, mirroring dstack's
 * `replay_dstack_rtmr`: `mr = sha384(mr || digest)` starting from 48 zero
 * bytes, over events with matching `imr`.
 */
export function replayRtmrHex(events: DstackEvent[], imr: number): string {
  let mr = new Uint8Array(48)
  for (const ev of events.filter((e) => e.imr === imr)) {
    let digest = decodeHex(ev.digest)
    if (digest.length < 48) {
      const padded = new Uint8Array(48)
      padded.set(digest)
      digest = padded
    }
    mr = sha384(concatBytes(mr, digest))
  }
  return bytesToHex(mr)
}

function concatBytes(...arrays: Uint8Array[]): Uint8Array {
  const total = arrays.reduce((n, a) => n + a.length, 0)
  const out = new Uint8Array(total)
  let off = 0
  for (const a of arrays) {
    out.set(a, off)
    off += a.length
  }
  return out
}

// --- report-bound verification -----------------------------------------------

export interface VerifyQuoteBoundOptions extends VerifyTdxQuoteOptions {
  /** Expected enclave reference. Auto-selected by app-id if omitted. */
  reference?: EnclaveReference
  /** TCB statuses treated as a clean pass. Defaults to ["UpToDate"]. */
  acceptableTcbStatuses?: readonly string[]
}

export interface QuoteBindingMeasurements {
  reportDataHex?: string
  tcbStatus?: string
  advisoryIds?: string[]
  mrTd?: string
  rtMr0?: string
  rtMr1?: string
  rtMr2?: string
  rtMr3?: string
  mrConfigId?: string
  /** compose_hash read from `mr_config_id` (`01 || compose_hash || 0*`). */
  composeHashFromConfigId?: string
  /** compose_hash read from the RTMR3 `compose-hash` event payload. */
  composeHashFromEventLog?: string
  osImageHashFromEventLog?: string
  appIdFromEventLog?: string
  referenceName?: string
  reference?: EnclaveReference
}

export interface VerifyQuoteBoundResult {
  /** True iff every gating check passed (non-gating/informational excluded). */
  ok: boolean
  checks: (Check & { gating?: boolean })[]
  measurements: QuoteBindingMeasurements
}

/**
 * Verify that the DCAP quote embedded in an attestation report is genuine TDX
 * hardware AND binds the given ACI report_data digest AND matches the published
 * measurement reference. Returns granular per-check results for the UI.
 */
export async function verifyQuoteBoundToReport(
  report: AttestationReport,
  reportDataDigestHex: string,
  opts: VerifyQuoteBoundOptions = {},
): Promise<VerifyQuoteBoundResult> {
  const checks: (Check & { gating?: boolean })[] = []
  const measurements: QuoteBindingMeasurements = {}
  const push = (id: string, ok: boolean, detail?: string, gating = true) =>
    checks.push({ id, ok, detail, gating })

  const evidence = report.attestation?.evidence
  const quoteHex = evidence?.quote
  if (!quoteHex || typeof quoteHex !== 'string') {
    push('quote_present', false, 'report.attestation.evidence.quote is missing')
    return { ok: false, checks, measurements }
  }

  // 1. Genuine-hardware + TCB.
  let quote: VerifyTdxQuoteResult
  try {
    quote = await verifyTdxQuote(quoteHex, opts)
  } catch (e) {
    push('quote_genuine', false, `js_verify failed: ${String(e)}`)
    return { ok: false, checks, measurements }
  }
  push('quote_genuine', quote.verified, `TDX quote signature chain verified against Intel collateral`)

  const acceptable = opts.acceptableTcbStatuses ?? DEFAULT_ACCEPTABLE_TCB_STATUSES
  const tcbOk = acceptable.includes(quote.tcbStatus)
  measurements.tcbStatus = quote.tcbStatus
  measurements.advisoryIds = quote.advisoryIds
  push(
    'tcb_status',
    tcbOk,
    `status=${quote.tcbStatus}${quote.advisoryIds.length ? ` advisories=${quote.advisoryIds.join(',')}` : ''}`,
  )

  const reg = quote.report
  measurements.reportDataHex = reg.reportDataHex
  measurements.mrTd = reg.mrTdHex
  measurements.mrConfigId = reg.mrConfigIdHex
  measurements.rtMr0 = reg.rtMr0Hex
  measurements.rtMr1 = reg.rtMr1Hex
  measurements.rtMr2 = reg.rtMr2Hex
  measurements.rtMr3 = reg.rtMr3Hex

  // 2. tee_type consistency.
  const verifiedTeeType = reg.kind === 'SgxEnclave' ? 'sgx' : 'tdx'
  push(
    'tee_type',
    report.attestation?.tee_type === verifiedTeeType,
    `reported=${report.attestation?.tee_type} verified=${verifiedTeeType}`,
  )

  // 3. evidence.quote_report_data (if present) must match the verified quote.
  const evidenceReportData =
    typeof evidence?.quote_report_data === 'string' ? normHex(evidence.quote_report_data) : undefined
  if (evidenceReportData !== undefined) {
    push(
      'quote_report_data_evidence',
      evidenceReportData === reg.reportDataHex,
      `evidence=${evidenceReportData}`,
    )
  }

  // 4. report_data binding (critical): quote report_data == digest||zeros.
  try {
    const b = checkReportDataBinding(reg.reportDataHex, reportDataDigestHex)
    push('report_data_binding', b.ok, b.detail)
  } catch (e) {
    push('report_data_binding', false, String(e))
  }

  // --- measurement policy --------------------------------------------------
  // Parse + authenticate the dstack event log, then bind named events.
  let events: DstackEvent[] = []
  let eventLogOk = false
  try {
    events = parseEventLog(evidence)
    // Replay RTMR3 and compare to the quote register: proves the log is authentic.
    if (reg.kind === 'SgxEnclave') {
      push('measurement_rtmr3_replay', false, 'SGX enclave has no RTMR3')
    } else {
      const replayed = replayRtmrHex(events, 3)
      eventLogOk = replayed === reg.rtMr3Hex
      push('measurement_rtmr3_replay', eventLogOk, `replayed=${replayed} quote=${reg.rtMr3Hex}`)
    }
    // Bind each imr==3 event payload into RTMR3 by recomputing its digest.
    const imr3 = events.filter((e) => e.imr === 3)
    const mismatched = imr3.filter((e) => dstackEventDigestHex(e) !== normHex(e.digest))
    push(
      'measurement_event_digest_binding',
      mismatched.length === 0,
      mismatched.length === 0
        ? `${imr3.length} RTMR3 events bound`
        : `unbound events: ${mismatched.map((e) => e.event).join(',')}`,
    )
  } catch (e) {
    push('measurement_rtmr3_replay', false, `event_log parse failed: ${String(e)}`)
    push('measurement_event_digest_binding', false, String(e))
  }

  const eventPayload = (name: string): string | undefined => {
    const ev = events.find((e) => e.imr === 3 && e.event === name)
    return ev ? normHex(ev.event_payload) : undefined
  }
  measurements.composeHashFromEventLog = eventPayload('compose-hash')
  measurements.osImageHashFromEventLog = eventPayload('os-image-hash')
  measurements.appIdFromEventLog = eventPayload('app-id')

  // compose_hash also lives in mr_config_id: `01 || compose_hash || 0*`.
  if (reg.mrConfigIdHex && reg.mrConfigIdHex.length >= 66) {
    measurements.composeHashFromConfigId = reg.mrConfigIdHex.slice(2, 66)
  }

  // Reference selection: explicit, else auto-detect by app-id.
  const reference = opts.reference ?? selectReference(measurements.appIdFromEventLog)
  measurements.reference = reference
  measurements.referenceName = reference
    ? reference === GATEWAY_REFERENCE
      ? 'gateway'
      : reference === MERIDIAN_REFERENCE
        ? 'meridian'
        : 'custom'
    : undefined

  if (!reference) {
    push('measurement_reference_known', false, `no reference matches app-id=${measurements.appIdFromEventLog}`)
  } else {
    // app-id
    push(
      'measurement_app_id',
      measurements.appIdFromEventLog === reference.appId.toLowerCase(),
      `event=${measurements.appIdFromEventLog} ref=${reference.appId}`,
    )
    // compose_hash via event log
    push(
      'measurement_compose_hash_eventlog',
      measurements.composeHashFromEventLog === reference.composeHash.toLowerCase(),
      `event=${measurements.composeHashFromEventLog} ref=${reference.composeHash}`,
    )
    // compose_hash via mr_config_id register (independent hardware binding)
    push(
      'measurement_compose_hash_mrconfigid',
      measurements.composeHashFromConfigId === reference.composeHash.toLowerCase(),
      `mr_config_id[1..33]=${measurements.composeHashFromConfigId} ref=${reference.composeHash}`,
    )
    // os_image_hash via event log
    push(
      'measurement_os_image_hash',
      measurements.osImageHashFromEventLog === reference.osImageHash.toLowerCase(),
      `event=${measurements.osImageHashFromEventLog} ref=${reference.osImageHash}`,
    )
  }

  // Raw firmware/boot registers: no published reference exists (osImageHash is
  // the dstack os-image-hash event, not the firmware MRTD). Expose raw and mark
  // explicitly unverified rather than fake a comparison — OS-image identity is
  // covered by measurement_os_image_hash above.
  push(
    'measurement_mrtd_reference',
    false,
    `no published reference for raw MRTD/RTMR0-2; mr_td=${reg.mrTdHex}. ` +
      `OS-image identity is instead anchored via measurement_os_image_hash.`,
    false, // non-gating: informational, does not fail the overall gate
  )

  const ok = checks.filter((c) => c.gating !== false).every((c) => c.ok)
  return { ok, checks, measurements }
}

function selectReference(appIdHex: string | undefined): EnclaveReference | undefined {
  if (!appIdHex) return undefined
  if (appIdHex === GATEWAY_REFERENCE.appId.toLowerCase()) return GATEWAY_REFERENCE
  if (appIdHex === MERIDIAN_REFERENCE.appId.toLowerCase()) return MERIDIAN_REFERENCE
  return undefined
}

// --- Meridian (enclave B) quote verification (hop-3) -------------------------
// Meridian is the @rynfar/meridian bridge, NOT private-ai-gateway: it has no ACI
// keyset/receipt. Its sidecar (deploy/meridian/attestor) exposes a nonce-bound
// raw TDX quote + dstack event log. So hop-3 verifies genuine hardware + nonce
// freshness + measurements (against MERIDIAN_REFERENCE) — the same measurement
// policy as the gateway, with the ACI report_data digest binding replaced by a
// direct nonce binding.

export interface MeridianQuoteInput {
  quote: string
  event_log: unknown
  vm_config?: unknown
}

/**
 * Freshness binding for a Meridian quote: the quote's 64-byte report_data must be
 * `nonce(left-justified into [0..32]) || zeros(32)` — i.e. it starts with exactly
 * the nonce the caller sent and is zero elsewhere. Mirrors the sidecar's binding.
 */
export function checkNonceBinding(
  quoteReportDataHex: string,
  nonceHex: string,
): { ok: boolean; detail: string } {
  const rd = normHex(quoteReportDataHex)
  const nonce = normHex(nonceHex)
  if (rd.length !== 128) {
    return { ok: false, detail: `quote report_data must be 64 bytes, got ${rd.length / 2}` }
  }
  if (nonce.length === 0 || nonce.length > 64) {
    return { ok: false, detail: `nonce must be 1..32 bytes, got ${nonce.length / 2}` }
  }
  const expected = (nonce + '0'.repeat(128)).slice(0, 128)
  const ok = rd === expected
  return {
    ok,
    detail: ok ? `report_data[0..32]=nonce, [32..64]=0` : `expected=${expected} actual=${rd}`,
  }
}

/**
 * Verify a Meridian enclave-B attestation (nonce-bound TDX quote + dstack event
 * log): genuine TDX hardware + TCB + nonce freshness + measurements against the
 * published Meridian reference. Returns the same granular result shape as the
 * gateway's quote verification.
 */
export async function verifyMeridianQuote(
  input: MeridianQuoteInput,
  nonceHex: string,
  opts: VerifyQuoteBoundOptions = {},
): Promise<VerifyQuoteBoundResult> {
  const checks: (Check & { gating?: boolean })[] = []
  const measurements: QuoteBindingMeasurements = {}
  const push = (id: string, ok: boolean, detail?: string, gating = true) =>
    checks.push({ id, ok, detail, gating })

  if (!input.quote || typeof input.quote !== 'string') {
    push('quote_present', false, 'no quote in Meridian attestation response')
    return { ok: false, checks, measurements }
  }

  // 1. Genuine hardware + TCB.
  let quote: VerifyTdxQuoteResult
  try {
    quote = await verifyTdxQuote(input.quote, opts)
  } catch (e) {
    push('quote_genuine', false, `js_verify failed: ${String(e)}`)
    return { ok: false, checks, measurements }
  }
  push('quote_genuine', quote.verified, 'TDX quote signature chain verified against Intel collateral')

  const acceptable = opts.acceptableTcbStatuses ?? DEFAULT_ACCEPTABLE_TCB_STATUSES
  const tcbOk = acceptable.includes(quote.tcbStatus)
  measurements.tcbStatus = quote.tcbStatus
  measurements.advisoryIds = quote.advisoryIds
  push(
    'tcb_status',
    tcbOk,
    `status=${quote.tcbStatus}${quote.advisoryIds.length ? ` advisories=${quote.advisoryIds.join(',')}` : ''}`,
  )

  const reg = quote.report
  measurements.reportDataHex = reg.reportDataHex
  measurements.mrTd = reg.mrTdHex
  measurements.mrConfigId = reg.mrConfigIdHex
  measurements.rtMr0 = reg.rtMr0Hex
  measurements.rtMr1 = reg.rtMr1Hex
  measurements.rtMr2 = reg.rtMr2Hex
  measurements.rtMr3 = reg.rtMr3Hex

  // 2. Nonce freshness (replaces the ACI report_data digest binding).
  const nb = checkNonceBinding(reg.reportDataHex, nonceHex)
  push('nonce_binding', nb.ok, nb.detail)

  // 3. Measurement policy — same as the gateway, against MERIDIAN_REFERENCE.
  let events: DstackEvent[] = []
  try {
    events = parseEventLog(input)
    if (reg.kind === 'SgxEnclave') {
      push('measurement_rtmr3_replay', false, 'SGX enclave has no RTMR3')
    } else {
      // The dstack `getQuote` event_log carries EMPTY `digest` fields (unlike the
      // gateway's ACI report), so recompute each imr==3 event's digest from its
      // (event_type, event, event_payload) — the same algorithm dstack extends,
      // confirmed byte-exact on the gateway — fold those into RTMR3, and match the
      // quote's rt_mr3. This binds every event payload (compose-hash, os-image-hash,
      // app-id, …) directly to the attested hardware register.
      const imr3 = events.filter((e) => e.imr === 3)
      const withDigests = events.map((e) =>
        e.imr === 3 ? { ...e, digest: dstackEventDigestHex(e) } : e,
      )
      const replayed = replayRtmrHex(withDigests, 3)
      push(
        'measurement_rtmr3_replay',
        replayed === reg.rtMr3Hex,
        `replayed=${replayed} quote=${reg.rtMr3Hex} (${imr3.length} events, digests recomputed from payloads)`,
      )
    }
  } catch (e) {
    push('measurement_rtmr3_replay', false, `event_log parse failed: ${String(e)}`)
  }

  const eventPayload = (name: string): string | undefined => {
    const ev = events.find((e) => e.imr === 3 && e.event === name)
    return ev ? normHex(ev.event_payload) : undefined
  }
  measurements.composeHashFromEventLog = eventPayload('compose-hash')
  measurements.osImageHashFromEventLog = eventPayload('os-image-hash')
  measurements.appIdFromEventLog = eventPayload('app-id')
  if (reg.mrConfigIdHex && reg.mrConfigIdHex.length >= 66) {
    measurements.composeHashFromConfigId = reg.mrConfigIdHex.slice(2, 66)
  }

  const reference = opts.reference ?? selectReference(measurements.appIdFromEventLog) ?? MERIDIAN_REFERENCE
  measurements.reference = reference
  measurements.referenceName = 'meridian'
  push(
    'measurement_app_id',
    measurements.appIdFromEventLog === reference.appId.toLowerCase(),
    `event=${measurements.appIdFromEventLog} ref=${reference.appId}`,
  )
  push(
    'measurement_compose_hash_eventlog',
    measurements.composeHashFromEventLog === reference.composeHash.toLowerCase(),
    `event=${measurements.composeHashFromEventLog} ref=${reference.composeHash}`,
  )
  push(
    'measurement_compose_hash_mrconfigid',
    measurements.composeHashFromConfigId === reference.composeHash.toLowerCase(),
    `mr_config_id[1..33]=${measurements.composeHashFromConfigId} ref=${reference.composeHash}`,
  )
  push(
    'measurement_os_image_hash',
    measurements.osImageHashFromEventLog === reference.osImageHash.toLowerCase(),
    `event=${measurements.osImageHashFromEventLog} ref=${reference.osImageHash}`,
  )
  push(
    'measurement_mrtd_reference',
    false,
    `no published reference for raw MRTD/RTMR0-2; mr_td=${reg.mrTdHex}. ` +
      `OS-image identity is instead anchored via measurement_os_image_hash.`,
    false,
  )

  const ok = checks.filter((c) => c.gating !== false).every((c) => c.ok)
  return { ok, checks, measurements }
}
