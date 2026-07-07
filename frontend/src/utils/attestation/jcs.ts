/**
 * Canonical JSON (JCS subset) + digest helpers for ACI attestation.
 *
 * FAITHFUL port of `src/aci/canonical.rs` and the `to_canonical_value()`
 * projections in `src/aci/types.rs` from private-ai-gateway @975ac50 (the sole
 * source of truth). This is NOT a general RFC 8785 implementation; it is the
 * exact subset ACI wire shapes need:
 *
 *   - String / integer (i64/u64/bigint) / bool / null,
 *   - arrays (declared order),
 *   - objects (keys emitted in UTF-16 code-unit order).
 *
 * Floats are rejected (RFC 8785 §3.2.2.3 number canonicalisation would require
 * ECMAScript-262 serialisation; ACI defines only integer numerics). Byte-exact
 * output is critical: a one-byte difference in the canonical bytes makes every
 * signature/digest check fail.
 */
import { sha256 } from '@noble/hashes/sha2.js'
import { bytesToHex, hexToBytes } from '@noble/hashes/utils.js'

/** A value accepted by {@link canonicalize}. Mirrors the ACI value space. */
export type CanonicalValue =
  | string
  | number
  | bigint
  | boolean
  | null
  | CanonicalValue[]
  | { [key: string]: CanonicalValue }

/** Thrown when a value cannot be canonicalised (matches CanonicalError). */
export class CanonicalError extends Error {}

/**
 * Serialise a JSON string exactly like `write_string` in canonical.rs
 * (RFC 8785 §3.2.2.2): `"`, `\` and the short escapes `\b \t \n \f \r`;
 * other control chars < 0x20 as lowercase `\u00xx`; every other code point
 * (including DEL 0x7f and all non-ASCII) verbatim as its UTF-8 bytes.
 */
function writeString(out: string[], s: string): void {
  out.push('"')
  // for..of iterates by Unicode code point, matching Rust's `s.chars()`.
  for (const ch of s) {
    const cp = ch.codePointAt(0) as number
    if (ch === '"') out.push('\\"')
    else if (ch === '\\') out.push('\\\\')
    else if (cp === 0x08) out.push('\\b')
    else if (cp === 0x09) out.push('\\t')
    else if (cp === 0x0a) out.push('\\n')
    else if (cp === 0x0c) out.push('\\f')
    else if (cp === 0x0d) out.push('\\r')
    else if (cp < 0x20) out.push('\\u' + cp.toString(16).padStart(4, '0'))
    else out.push(ch)
  }
  out.push('"')
}

function writeNumber(out: string[], n: number | bigint): void {
  if (typeof n === 'bigint') {
    out.push(n.toString())
    return
  }
  if (!Number.isInteger(n)) {
    throw new CanonicalError(
      'JCS: float / non-integer numeric is not allowed in the ACI value space',
    )
  }
  // Integers within IEEE-754 safe range serialise identically to Rust i64/u64.
  out.push(String(n))
}

function writeValue(out: string[], value: CanonicalValue): void {
  if (value === null) {
    out.push('null')
    return
  }
  switch (typeof value) {
    case 'boolean':
      out.push(value ? 'true' : 'false')
      return
    case 'number':
    case 'bigint':
      writeNumber(out, value)
      return
    case 'string':
      writeString(out, value)
      return
    case 'object':
      break
    default:
      throw new CanonicalError(`JCS: unsupported value type ${typeof value}`)
  }
  if (Array.isArray(value)) {
    out.push('[')
    value.forEach((item, i) => {
      if (i > 0) out.push(',')
      writeValue(out, item)
    })
    out.push(']')
    return
  }
  // Object: sort keys by UTF-16 code units. JS's default string comparison
  // (`<`) is lexicographic on UTF-16 code units, matching `utf16_compare`.
  const keys = Object.keys(value).sort()
  out.push('{')
  keys.forEach((key, i) => {
    const child = value[key]
    if (child === undefined) {
      // ACI projections never emit `undefined`; surface it rather than hide it.
      throw new CanonicalError(`JCS: object key ${JSON.stringify(key)} has undefined value`)
    }
    if (i > 0) out.push(',')
    writeString(out, key)
    out.push(':')
    writeValue(out, child)
  })
  out.push('}')
}

/** Canonicalise a value to its JCS-subset byte string. */
export function canonicalize(value: CanonicalValue): Uint8Array {
  const parts: string[] = []
  writeValue(parts, value)
  return new TextEncoder().encode(parts.join(''))
}

/** `"sha256:" || hex(sha256(payload))` — mirrors `sha256_hex`. */
export function sha256Hex(payload: Uint8Array): string {
  return 'sha256:' + bytesToHex(sha256(payload))
}

/** `"sha256:" || hex(sha256(JCS(value)))` — mirrors `jcs_sha256_hex`. */
export function jcsSha256Hex(value: CanonicalValue): string {
  return sha256Hex(canonicalize(value))
}

/** Raw 32-byte `sha256(JCS(value))` — mirrors `jcs_sha256_raw`. */
export function jcsSha256Raw(value: CanonicalValue): Uint8Array {
  return sha256(canonicalize(value))
}

export { bytesToHex }

/**
 * Decode hex (with optional `0x` prefix) to bytes. Mirrors `decode_hex` in
 * `src/aci/verifier/mod.rs`, which strips a leading `0x` before hex-decoding.
 */
export function decodeHex(value: string): Uint8Array {
  const stripped = value.startsWith('0x') || value.startsWith('0X') ? value.slice(2) : value
  return hexToBytes(stripped)
}

/**
 * Coerce a wire integer to a canonical numeric. ACI u64/i64 fields arrive as
 * JSON numbers; the loose frontend types also allow strings. Non-integers are
 * rejected (matching `write_number`).
 */
export function asCanonicalInt(v: unknown): number | bigint {
  if (typeof v === 'bigint') return v
  if (typeof v === 'number') {
    if (!Number.isInteger(v)) throw new CanonicalError('JCS: non-integer numeric')
    return v
  }
  if (typeof v === 'string') {
    if (!/^-?\d+$/.test(v)) throw new CanonicalError(`JCS: not an integer string: ${v}`)
    const n = Number(v)
    return Number.isSafeInteger(n) ? n : BigInt(v)
  }
  throw new CanonicalError(`JCS: expected integer, got ${typeof v}`)
}

// ---------------------------------------------------------------------------
// Canonical-value projections — one per `to_canonical_value()` in types.rs.
// These decide which fields are included/omitted and null-vs-absent semantics,
// which determine digest equality. Reproduced EXACTLY from the Rust.
// ---------------------------------------------------------------------------

interface WirePublicKey {
  algo: string
  public_key: string
}
// `key_id` is optional here only to stay structurally compatible with the loose
// `@/api/attestation` types; a conformant ACI keyed key always carries it.
interface WireKeyedPublicKey extends WirePublicKey {
  key_id?: string
}
interface WireTlsSpki {
  spki_sha256: string
  domain?: string | null
}
interface WireWorkloadKeyset {
  workload_identity: { public_key: WirePublicKey; subject?: string | null }
  keyset_epoch?: { version?: unknown; not_after?: unknown }
  receipt_signing_keys?: WireKeyedPublicKey[]
  e2ee_public_keys?: WireKeyedPublicKey[]
  tls_public_keys?: WireTlsSpki[]
}

export const REPORT_DATA_PURPOSE = 'aci.report_data.v1'
export const KEYSET_ENDORSEMENT_PURPOSE = 'aci.keyset.endorsement.v1'

/** `PublicKeyMaterial::to_canonical_value` → `{algo, public_key}`. */
export function publicKeyMaterialCanonical(pk: WirePublicKey): CanonicalValue {
  return { algo: pk.algo, public_key: pk.public_key }
}

/** `KeyedPublicKey::to_canonical_value` → `{key_id, algo, public_key}`. */
export function keyedPublicKeyCanonical(k: WireKeyedPublicKey): CanonicalValue {
  return { key_id: k.key_id ?? '', algo: k.algo, public_key: k.public_key }
}

/** `TlsSpki::to_canonical_value` → `{spki_sha256}` plus `domain` iff present. */
export function tlsSpkiCanonical(t: WireTlsSpki): CanonicalValue {
  const out: { [k: string]: CanonicalValue } = { spki_sha256: t.spki_sha256 }
  if (t.domain !== undefined && t.domain !== null) out.domain = t.domain
  return out
}

/**
 * `WorkloadKeyset::to_canonical_value`. Note: `subject` is null-included (never
 * omitted); the three key arrays are always emitted, empty as `[]`.
 */
export function workloadKeysetCanonical(ks: WireWorkloadKeyset): CanonicalValue {
  const wi = ks.workload_identity
  return {
    workload_identity: {
      public_key: publicKeyMaterialCanonical(wi.public_key),
      subject: wi.subject === undefined ? null : wi.subject,
    },
    keyset_epoch: {
      version: asCanonicalInt(ks.keyset_epoch?.version),
      not_after: asCanonicalInt(ks.keyset_epoch?.not_after),
    },
    receipt_signing_keys: (ks.receipt_signing_keys ?? []).map(keyedPublicKeyCanonical),
    e2ee_public_keys: (ks.e2ee_public_keys ?? []).map(keyedPublicKeyCanonical),
    tls_public_keys: (ks.tls_public_keys ?? []).map(tlsSpkiCanonical),
  }
}

/**
 * `AttestationStatement::to_canonical_value`. `nonce` is null-included when
 * absent (JSON `null`, never the string `"null"`).
 */
export function attestationStatementCanonical(
  workloadId: string,
  workloadKeysetDigest: string,
  nonce: string | null | undefined,
): CanonicalValue {
  return {
    purpose: REPORT_DATA_PURPOSE,
    workload_id: workloadId,
    workload_keyset_digest: workloadKeysetDigest,
    nonce: nonce === undefined ? null : nonce,
  }
}

/** `KeysetEndorsementPayload::to_canonical_value`. */
export function keysetEndorsementPayloadCanonical(workloadKeysetDigest: string): CanonicalValue {
  return {
    purpose: KEYSET_ENDORSEMENT_PURPOSE,
    workload_keyset_digest: workloadKeysetDigest,
  }
}

interface WireReceiptEvent {
  seq: unknown
  type: string
  fields?: Record<string, unknown>
  [k: string]: unknown
}
interface WireReceipt {
  api_version: string
  receipt_id: string
  chat_id?: string | null
  workload_id: string
  workload_keyset_digest: string
  endpoint: string
  method: string
  served_at: unknown
  event_log?: WireReceiptEvent[]
  signature: { algo: string; key_id: string; value?: string }
}

/**
 * `ReceiptEvent::to_canonical_value` — flatten `seq` and `type` into the same
 * object as the type-specific fields. Handles both the flattened wire shape
 * (fields hoisted next to seq/type) and the nested `{seq,type,fields}` shape,
 * mirroring the reference `parse_event` + `to_canonical_value` combination.
 */
export function receiptEventCanonical(ev: WireReceiptEvent): CanonicalValue {
  const obj: { [k: string]: CanonicalValue } = {
    seq: asCanonicalInt(ev.seq),
    type: ev.type,
  }
  if (ev.fields && typeof ev.fields === 'object' && !Array.isArray(ev.fields)) {
    for (const [k, v] of Object.entries(ev.fields)) obj[k] = v as CanonicalValue
  } else {
    for (const [k, v] of Object.entries(ev)) {
      if (k !== 'seq' && k !== 'type') obj[k] = v as CanonicalValue
    }
  }
  return obj
}

/**
 * `Receipt::to_canonical_value(include_signature_value)`. `chat_id` is
 * null-included; the signature object drops `value` when
 * `includeSignatureValue` is false (the bytes the signature itself covers).
 */
export function receiptCanonical(r: WireReceipt, includeSignatureValue: boolean): CanonicalValue {
  const signature: { [k: string]: CanonicalValue } = {
    algo: r.signature.algo,
    key_id: r.signature.key_id,
  }
  if (includeSignatureValue) signature.value = r.signature.value ?? ''
  return {
    api_version: r.api_version,
    receipt_id: r.receipt_id,
    chat_id: r.chat_id === undefined ? null : r.chat_id,
    workload_id: r.workload_id,
    workload_keyset_digest: r.workload_keyset_digest,
    endpoint: r.endpoint,
    method: r.method,
    served_at: asCanonicalInt(r.served_at),
    event_log: (r.event_log ?? []).map(receiptEventCanonical),
    signature,
  }
}

/** Bytes a verifier MUST check the receipt signature over (`canonical_bytes_for_signing`). */
export function canonicalBytesForSigning(r: WireReceipt): Uint8Array {
  return canonicalize(receiptCanonical(r, false))
}
