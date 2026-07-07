/**
 * Browser ECIES client for ACI v2 E2EE — a byte-faithful port of the client
 * halves of private-ai-gateway `src/aci/e2ee.rs` (`encrypt_for_public_key` /
 * `decrypt_with_secret_key`) plus the AAD strings from
 * `src/aggregator/service/e2ee_crypto.rs`. Crypto is delegated to
 * @noble/curves + @noble/hashes + @noble/ciphers; nothing is hand-rolled.
 *
 * Scheme (canonical ACI v2, algo `secp256k1-aes-256-gcm-hkdf-sha256`):
 *   ephemeral secp256k1 ECDH  ->  HKDF-SHA256(info="aci.e2ee.v2.secp256k1")
 *   ->  AES-256-GCM (12-byte nonce, 16-byte tag, structured AAD).
 * The ECDH shared secret is the 32-byte X-coordinate of the shared point,
 * matching k256's `SharedSecret::raw_secret_bytes()`.
 *
 * Wire ciphertext (lowercase hex):
 *   ephemeral_uncompressed_pubkey(65) || aes_gcm_nonce(12) || ciphertext_tag
 *
 * This lets the browser encrypt a payload to the gateway's ATTESTED
 * `e2ee_public_keys[secp256k1-...]` key, so only the attested enclave (holding
 * the dstack-KMS-released private key) can decrypt it — a TLS-independent,
 * browser-checkable hop-1 confidentiality proof.
 */
import { sha256 } from '@noble/hashes/sha2.js'
import { hkdf } from '@noble/hashes/hkdf.js'
import { bytesToHex, hexToBytes, utf8ToBytes, randomBytes } from '@noble/hashes/utils.js'
import { secp256k1 } from '@noble/curves/secp256k1.js'
import { gcm } from '@noble/ciphers/aes.js'
import type { AttestationReport, KeyedPublicKey } from '@/api/attestation'

/** Canonical ACI v2 E2EE algorithm + version, matching the Rust constants. */
export const E2EE_ALGO = 'secp256k1-aes-256-gcm-hkdf-sha256'
export const E2EE_VERSION = '2'

// HKDF context string (`HKDF_INFO` in e2ee.rs). salt is None → RFC 5869 default
// (HashLen zeros); HMAC makes an empty/undefined salt equivalent, so we pass
// undefined below.
const HKDF_INFO = utf8ToBytes('aci.e2ee.v2.secp256k1')
const PUBLIC_KEY_LEN = 65
const NONCE_LEN = 12
const TAG_LEN = 16

function stripHexPrefix(value: string): string {
  return value.startsWith('0x') ? value.slice(2) : value
}

/**
 * Normalize a secp256k1 public key to 65-byte uncompressed lowercase hex,
 * mirroring `normalize_secp256k1_public_key_hex`: accepts a 65-byte `04||X||Y`
 * point or a bare 64-byte `X||Y`, validates it lies on the curve, and errors
 * otherwise.
 */
export function normalizeSecp256k1PublicKeyHex(value: string): string {
  const bytes = hexToBytes(stripHexPrefix(value))
  let full: Uint8Array
  if (bytes.length === PUBLIC_KEY_LEN && bytes[0] === 0x04) {
    full = bytes
  } else if (bytes.length === 64) {
    full = new Uint8Array(PUBLIC_KEY_LEN)
    full[0] = 0x04
    full.set(bytes, 1)
  } else {
    throw new Error(`secp256k1 public key must be 64 or 65 bytes, got ${bytes.length}`)
  }
  const hex = bytesToHex(full)
  // Validate the point (throws on an off-curve key), matching from_sec1_bytes.
  secp256k1.Point.fromHex(hex)
  return hex
}

export interface ClientKeypair {
  /** 32-byte secp256k1 secret scalar; kept in-browser to decrypt the response. */
  privateKey: Uint8Array
  /** 65-byte uncompressed public key, lowercase hex (the `X-Client-Pub-Key`). */
  publicKeyHex: string
}

/** Generate a per-request ephemeral client keypair (the browser's E2EE identity). */
export function generateClientKeypair(): ClientKeypair {
  const privateKey = secp256k1.utils.randomSecretKey()
  const publicKeyHex = bytesToHex(secp256k1.getPublicKey(privateKey, false))
  return { privateKey, publicKeyHex }
}

/** ECDH -> HKDF-SHA256 -> 32-byte AES-256 key (shared secret = 32-byte X coord). */
function deriveAesKey(secretKey: Uint8Array, peerPublicKey: Uint8Array): Uint8Array {
  const sharedX = secp256k1.getSharedSecret(secretKey, peerPublicKey, true).slice(1)
  return hkdf(sha256, sharedX, undefined, HKDF_INFO, 32)
}

/**
 * Encrypt `plaintext` to a recipient secp256k1 public key, bound to `aad`.
 * Port of `encrypt_for_public_key`. Returns the wire ciphertext as lowercase
 * hex: `ephemeral_pub(65) || nonce(12) || ciphertext_tag`.
 */
export function eciesEncrypt(
  recipientPublicKeyHex: string,
  plaintext: Uint8Array,
  aad: Uint8Array,
): string {
  const recipient = hexToBytes(normalizeSecp256k1PublicKeyHex(recipientPublicKeyHex))
  const ephemeralPriv = secp256k1.utils.randomSecretKey()
  const ephemeralPub = secp256k1.getPublicKey(ephemeralPriv, false)
  const key = deriveAesKey(ephemeralPriv, recipient)
  const nonce = randomBytes(NONCE_LEN)
  const ciphertext = gcm(key, nonce, aad).encrypt(plaintext)

  const out = new Uint8Array(PUBLIC_KEY_LEN + NONCE_LEN + ciphertext.length)
  out.set(ephemeralPub, 0)
  out.set(nonce, PUBLIC_KEY_LEN)
  out.set(ciphertext, PUBLIC_KEY_LEN + NONCE_LEN)
  return bytesToHex(out)
}

/**
 * Decrypt a wire ciphertext with the client secret key, bound to `aad`. Port of
 * `decrypt_with_secret_key`: the ephemeral public key is read from the blob (for
 * a response this is the SERVER's ephemeral key, re-encrypting to our client
 * key). Throws on a bad length or a failed GCM tag / AAD mismatch.
 */
export function eciesDecrypt(
  clientPrivateKey: Uint8Array,
  ciphertextHex: string,
  aad: Uint8Array,
): Uint8Array {
  const blob = hexToBytes(stripHexPrefix(ciphertextHex))
  if (blob.length < PUBLIC_KEY_LEN + NONCE_LEN + TAG_LEN) {
    throw new Error(`E2EE ciphertext too short: got ${blob.length} bytes`)
  }
  const ephemeralPub = blob.slice(0, PUBLIC_KEY_LEN)
  const nonce = blob.slice(PUBLIC_KEY_LEN, PUBLIC_KEY_LEN + NONCE_LEN)
  const ciphertext = blob.slice(PUBLIC_KEY_LEN + NONCE_LEN)
  const key = deriveAesKey(clientPrivateKey, ephemeralPub)
  return gcm(key, nonce, aad).decrypt(ciphertext)
}

/** An AAD component may not contain `|`, `\r`, `\n` (mirrors `aad_component_is_ambiguous`). */
export function aadComponentAmbiguous(value: string): boolean {
  return value.includes('|') || value.includes('\r') || value.includes('\n')
}

/**
 * Request AAD for a chat-completions `messages[].content` string field.
 * Format (`request_aad`): `v2|req|algo=..|model=..|m=<i>|c=<j|->|n=<nonce>|ts=<ts>`.
 */
export function requestMessagesAad(opts: {
  algo?: string
  model: string
  messageIndex?: number
  contentIndex?: number | null
  nonce: string
  timestamp: number
}): Uint8Array {
  const algo = opts.algo ?? E2EE_ALGO
  const m = opts.messageIndex ?? 0
  const c = opts.contentIndex == null ? '-' : String(opts.contentIndex)
  return utf8ToBytes(
    `v2|req|algo=${algo}|model=${opts.model}|m=${m}|c=${c}|n=${opts.nonce}|ts=${opts.timestamp}`,
  )
}

/**
 * Response AAD for a chat `choices[].message.content` field.
 * Format (`response_aad`): `v2|resp|algo=..|model=..|id=<id>|choice=<k>|field=<f>|n=<nonce>|ts=<ts>`.
 * NB: in AciV2 mode the gateway binds the REQUEST model here, not the response's.
 */
export function responseChatAad(opts: {
  algo?: string
  model: string
  responseId: string
  choiceIndex?: number
  fieldName?: string
  nonce: string
  timestamp: number
}): Uint8Array {
  const algo = opts.algo ?? E2EE_ALGO
  const choice = opts.choiceIndex ?? 0
  const field = opts.fieldName ?? 'content'
  return utf8ToBytes(
    `v2|resp|algo=${algo}|model=${opts.model}|id=${opts.responseId}|choice=${choice}|field=${field}|n=${opts.nonce}|ts=${opts.timestamp}`,
  )
}

/**
 * The attested secp256k1 E2EE recipient key (`X-Model-Pub-Key`), read from the
 * report's `e2ee_public_keys`. Returns null if the deployment advertises none.
 */
export function selectE2eeKey(report: AttestationReport): KeyedPublicKey | null {
  const keys = report.attestation?.workload_keyset?.e2ee_public_keys ?? []
  return keys.find((k) => k.algo === E2EE_ALGO) ?? null
}
