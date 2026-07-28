/**
 * Browser-side Intel TDX DCAP hardware-quote verification for the /proof page
 * (single-enclave architecture: one sub2api CVM on Phala dstack).
 *
 * This is the hardware root of trust that lets the /proof page mark steps
 * "verified". It wraps `@phala/dcap-qvl` (Phala's pure-JS DCAP verifier) —
 * crypto is NOT reimplemented here. Collateral (Intel PCK chain / TCB / QE
 * identity) is fetched from Phala PCCS directly in the browser.
 *
 * Why not `@phala/dcap-qvl-web`: that wasm build is affected by
 * CVE-2026-22696 / GHSA-796p-j2gh-9m2q (CVSS 9.3) — it fetched the QE Identity
 * collateral but never verified its signature against the Intel-rooted issuer
 * chain, and never enforced MRSIGNER / ISVPRODID / ISVSVN on the QE report. A
 * forged QE Identity could therefore whitelist a rogue Quoting Enclave and sign
 * arbitrary quotes, defeating the whole point of this page. No fixed release
 * exists for the `-web` package; upstream's remedy is this pure-JS package
 * (fixed in 0.3.9), which verifies the QE Identity signature and enforces the
 * QE report policy.
 *
 * Verification chain (verifyEnclaveQuote):
 *   1. `verify()` proves the quote is genuine TDX hardware and reports its TCB
 *      posture (signature chain + collateral, including QE Identity).
 *   2. nonce binding: the quote's 64-byte report_data must be
 *      `nonce(left-justified into [0..32]) || zeros(32)` — the nonce this
 *      verifier generated, proving the quote is fresh, not replayed.
 *   3. measurement policy: the dstack event log is replayed into RTMR3 and
 *      compared to the quote's rt_mr3 (proving the log is authentic); then
 *      compose-hash / os-image-hash / app-id events are checked against the
 *      published reference. compose_hash is additionally cross-checked against
 *      the `mr_config_id` register, which dstack sets to `01 || compose_hash || 0*`.
 *
 * Honesty note: the raw MRTD / RTMR0-2 boot registers have NO published
 * reference value (the published `osImageHash` is dstack's os-image-hash event,
 * not the firmware MRTD), so they are exposed raw and reported as `unverified`
 * rather than faked green — OS-image identity is instead verified transitively
 * via the anchored `os-image-hash` event.
 */
import { getCollateral, verify as dcapVerify } from '@phala/dcap-qvl'
import { sha256, sha384 } from '@noble/hashes/sha2.js'
import { bytesToHex, hexToBytes } from '@noble/hashes/utils.js'
import type { EnclaveReference } from '@/constants/attestation'

/** One granular verification step, rendered as a row in the auditors list. */
export interface Check {
  id: string
  ok: boolean
  detail?: string
  /**
   * True when this step could not be decided rather than having failed — set
   * for measurement comparisons made against a reference we cannot vouch for
   * (mirror or baked-in copy). Such a step never gates the overall verdict:
   * a stale reference must not be reported as a tampered enclave.
   */
  indeterminate?: boolean
}

/** Default Phala PCCS endpoint (verified CORS-clean for browser fetches). */
export const PHALA_PCCS_URL = 'https://pccs.phala.network'

/** TCB statuses accepted as a clean hardware pass by default. */
export const DEFAULT_ACCEPTABLE_TCB_STATUSES = ['UpToDate'] as const

/** Decode hex (with optional `0x` prefix) to bytes. */
export function decodeHex(value: string): Uint8Array {
  const stripped = value.startsWith('0x') || value.startsWith('0X') ? value.slice(2) : value
  return hexToBytes(stripped)
}

// --- dcap-qvl report shapes ---------------------------------------------------
// Mirrors @phala/dcap-qvl 0.6.x: register fields are camelCase `Uint8Array`
// (the wasm build used to hand back snake_case hex strings).

interface DcapTd10 {
  mrTd: Uint8Array
  mrConfigId: Uint8Array
  rtMr0: Uint8Array
  rtMr1: Uint8Array
  rtMr2: Uint8Array
  rtMr3: Uint8Array
  reportData: Uint8Array
}

/** dcap-qvl `Report`: variant tag + payload; `asTd10()` unwraps TD15's `base`. */
interface DcapReport {
  type: 'sgx' | 'td10' | 'td15'
  asTd10: () => DcapTd10 | null
  asSgx: () => { reportData: Uint8Array } | null
}

/** Result of `verify()` (dcap-qvl `VerifiedReport`). */
export interface VerifyResult {
  status: string
  advisory_ids: string[]
  report: DcapReport
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

export function extractRegisters(report: DcapReport): TdxRegisters {
  const td = report.type === 'td10' || report.type === 'td15' ? report.asTd10() : null
  if (td) {
    return {
      kind: report.type === 'td15' ? 'TD15' : 'TD10',
      reportDataHex: toHex(td.reportData),
      mrTdHex: toHex(td.mrTd),
      mrConfigIdHex: toHex(td.mrConfigId),
      rtMr0Hex: toHex(td.rtMr0),
      rtMr1Hex: toHex(td.rtMr1),
      rtMr2Hex: toHex(td.rtMr2),
      rtMr3Hex: toHex(td.rtMr3),
    }
  }
  const sgx = report.type === 'sgx' ? report.asSgx() : null
  if (sgx) {
    return { kind: 'SgxEnclave', reportDataHex: toHex(sgx.reportData) }
  }
  throw new Error('unrecognized dcap-qvl report variant (expected td10/td15/sgx)')
}

/**
 * Register bytes -> lowercase hex. dcap-qvl hands back `Uint8Array` (Buffer is
 * a subclass, so both work); anything else means the field moved and we must
 * not silently pass an empty string off as a measurement.
 */
function toHex(v: unknown): string {
  if (v instanceof Uint8Array) return bytesToHex(v)
  throw new Error('dcap-qvl register field is not a Uint8Array (upstream shape changed?)')
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
  /** Raw `verify()` result. */
  raw: VerifyResult
}

/**
 * Verify a raw TDX DCAP quote against Intel collateral fetched from PCCS.
 *
 * Reaching a result means the quote's signature chain verified against the
 * Intel PCK/TCB/QE collateral (i.e. genuine hardware); `verify()` throws
 * otherwise. `tcbStatus` conveys the TCB level separately.
 */
export async function verifyTdxQuote(
  quoteHex: string,
  opts: VerifyTdxQuoteOptions = {},
): Promise<VerifyTdxQuoteResult> {
  const quote = decodeHex(quoteHex)
  const pccsUrl = opts.pccsUrl ?? PHALA_PCCS_URL
  const collateral = await getCollateral(pccsUrl, quote)
  const nowSecs = opts.nowSecs ?? Math.floor(Date.now() / 1000)
  const raw = dcapVerify(quote, collateral, nowSecs) as unknown as VerifyResult
  return {
    verified: true,
    tcbStatus: raw.status,
    advisoryIds: raw.advisory_ids ?? [],
    report: extractRegisters(raw.report),
    raw,
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
 * (confirmed empirically against live dstack quotes).
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

// --- enclave quote verification ------------------------------------------------
// The attestor sidecar (deploy/attestor) exposes a nonce-bound raw TDX quote +
// dstack event log. Verification = genuine hardware + nonce freshness +
// measurements against the published reference.

export interface EnclaveQuoteInput {
  quote: string
  event_log: unknown
  vm_config?: unknown
  /** Enclave-held E2EE public key (hex, 65B uncompressed secp256k1); its
   * sha256 is bound into report_data[32..64] when present. */
  e2ee_public_key?: string
  e2ee_algo?: string
}

export interface VerifyEnclaveQuoteOptions extends VerifyTdxQuoteOptions {
  /** Published reference to compare measurements against (required). */
  reference: EnclaveReference
  /**
   * Whether `reference` came from the authoritative source (the public repo).
   * Defaults to true. When false the reference may legitimately lag the running
   * deployment — a CDN mirror caches, and the baked-in copy can never be current
   * by construction (editing it changes the image, which changes composeHash).
   * Mismatches are then reported as *indeterminate*, not as failures.
   */
  referenceAuthoritative?: boolean
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
  reference?: EnclaveReference
}

export interface VerifyEnclaveQuoteResult {
  /** True iff every gating check passed (non-gating/informational excluded). */
  ok: boolean
  /**
   * True when the hardware itself verified but at least one measurement could
   * not be compared against an authoritative reference. Neither a pass nor a
   * fail — the caller must render it as "could not complete".
   */
  indeterminate: boolean
  checks: (Check & { gating?: boolean })[]
  measurements: QuoteBindingMeasurements
}

/**
 * Freshness binding: the quote's 64-byte report_data must be
 * `nonce(left-justified into [0..32]) || zeros(32)` — i.e. it starts with exactly
 * the nonce the caller sent and is zero elsewhere. Mirrors the sidecar's binding.
 */
export function checkNonceBinding(
  quoteReportDataHex: string,
  nonceHex: string,
  expectedTailHex?: string,
): { ok: boolean; detail: string } {
  const rd = normHex(quoteReportDataHex)
  const nonce = normHex(nonceHex)
  if (rd.length !== 128) {
    return { ok: false, detail: `quote report_data must be 64 bytes, got ${rd.length / 2}` }
  }
  if (nonce.length === 0 || nonce.length > 64) {
    return { ok: false, detail: `nonce must be 1..32 bytes, got ${nonce.length / 2}` }
  }
  const tail = expectedTailHex ? normHex(expectedTailHex) : '0'.repeat(64)
  const expected = (nonce + '0'.repeat(64)).slice(0, 64) + tail
  const ok = rd === expected
  return {
    ok,
    detail: ok
      ? `report_data[0..32]=nonce, [32..64]=${expectedTailHex ? 'sha256(e2ee pubkey)' : '0'}`
      : `expected=${expected} actual=${rd}`,
  }
}

/**
 * Compare the measurements read out of the quote/event log against the
 * published reference.
 *
 * `authoritative` says whether the reference is the live copy from the public
 * repo. When it is not (a CDN mirror, or the copy baked into this page — which
 * can never be current, since writing it changes the image it lives in), the
 * reference is *allowed* to lag the enclave. A mismatch then means "we could
 * not check", not "the enclave is not what it claims": such a check is marked
 * indeterminate and made non-gating, so a stale reference can never be
 * reported to the user as a tampered enclave.
 */
export function compareToReference(
  measurements: QuoteBindingMeasurements,
  reference: EnclaveReference,
  authoritative: boolean,
): (Check & { gating?: boolean })[] {
  const compare = (id: string, ok: boolean, detail: string): Check & { gating?: boolean } =>
    ok || authoritative
      ? { id, ok, detail, gating: true }
      : {
          id,
          ok: false,
          indeterminate: true,
          gating: false,
          detail: `${detail} — reference is not the authoritative copy, so this mismatch is undecided, not a failure`,
        }

  return [
    compare(
      'measurement_app_id',
      measurements.appIdFromEventLog === reference.appId.toLowerCase(),
      `event=${measurements.appIdFromEventLog} ref=${reference.appId}`,
    ),
    compare(
      'measurement_compose_hash_eventlog',
      measurements.composeHashFromEventLog === reference.composeHash.toLowerCase(),
      `event=${measurements.composeHashFromEventLog} ref=${reference.composeHash}`,
    ),
    compare(
      'measurement_compose_hash_mrconfigid',
      measurements.composeHashFromConfigId === reference.composeHash.toLowerCase(),
      `mr_config_id[1..33]=${measurements.composeHashFromConfigId} ref=${reference.composeHash}`,
    ),
    compare(
      'measurement_os_image_hash',
      measurements.osImageHashFromEventLog === reference.osImageHash.toLowerCase(),
      `event=${measurements.osImageHashFromEventLog} ref=${reference.osImageHash}`,
    ),
  ]
}

/**
 * Verify the sub2api CVM's attestation (nonce-bound TDX quote + dstack event
 * log): genuine TDX hardware + TCB + nonce freshness + measurements against the
 * published reference. Returns granular per-check results for the UI.
 */
export async function verifyEnclaveQuote(
  input: EnclaveQuoteInput,
  nonceHex: string,
  opts: VerifyEnclaveQuoteOptions,
): Promise<VerifyEnclaveQuoteResult> {
  const checks: (Check & { gating?: boolean })[] = []
  const measurements: QuoteBindingMeasurements = {}
  const push = (id: string, ok: boolean, detail?: string, gating = true) =>
    checks.push({ id, ok, detail, gating })

  if (!input.quote || typeof input.quote !== 'string') {
    push('quote_present', false, 'no quote in attestor response')
    return { ok: false, indeterminate: false, checks, measurements }
  }

  // 1. Genuine hardware + TCB.
  let quote: VerifyTdxQuoteResult
  try {
    quote = await verifyTdxQuote(input.quote, opts)
  } catch (e) {
    push('quote_genuine', false, `dcap-qvl verify failed: ${String(e)}`)
    return { ok: false, indeterminate: false, checks, measurements }
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

  // 2. Nonce freshness (+ E2EE key binding when the attestor advertises one:
  // report_data[32..64] must be sha256 of the advertised public key, tying
  // the enclave-held encryption key to this hardware quote).
  const pubHex =
    typeof input.e2ee_public_key === 'string' && /^[0-9a-f]{130}$/i.test(input.e2ee_public_key)
      ? input.e2ee_public_key.toLowerCase()
      : undefined
  const pubHashHex = pubHex ? bytesToHex(sha256(hexToBytes(pubHex))) : undefined
  const nb = checkNonceBinding(reg.reportDataHex, nonceHex, pubHashHex)
  push('nonce_binding', nb.ok, nb.detail)
  if (pubHex) {
    push(
      'e2ee_key_binding',
      reg.reportDataHex.slice(64) === pubHashHex,
      `sha256(pubkey)=${pubHashHex} report_data[32..64]=${reg.reportDataHex.slice(64)}`,
    )
  }

  // 3. Measurement policy.
  let events: DstackEvent[] = []
  try {
    events = parseEventLog(input)
    if (reg.kind === 'SgxEnclave') {
      push('measurement_rtmr3_replay', false, 'SGX enclave has no RTMR3')
    } else {
      // The dstack `getQuote` event_log carries EMPTY `digest` fields, so
      // recompute each imr==3 event's digest from its (event_type, event,
      // event_payload) — the same algorithm dstack extends — fold those into
      // RTMR3, and match the quote's rt_mr3. This binds every event payload
      // (compose-hash, os-image-hash, app-id, …) directly to the attested
      // hardware register.
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
  // compose_hash also lives in mr_config_id: `01 || compose_hash || 0*`.
  if (reg.mrConfigIdHex && reg.mrConfigIdHex.length >= 66) {
    measurements.composeHashFromConfigId = reg.mrConfigIdHex.slice(2, 66)
  }

  const reference = opts.reference
  measurements.reference = reference
  checks.push(
    ...compareToReference(measurements, reference, opts.referenceAuthoritative !== false),
  )
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
  const indeterminate = checks.some((c) => c.indeterminate === true)
  return { ok, indeterminate, checks, measurements }
}
