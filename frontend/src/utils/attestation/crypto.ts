/**
 * ACI signature-verification primitives — faithful port of the verifier halves
 * of `src/aci/keys.rs` (`verify_keyset_endorsement`, `verify_receipt_signature`,
 * `ethereum_address_from_uncompressed_public_key`). Crypto is delegated to
 * @noble/curves + @noble/hashes; nothing is hand-rolled.
 *
 * Algorithm strings match the Rust constants exactly.
 */
import { sha256 } from '@noble/hashes/sha2.js'
import { keccak_256 } from '@noble/hashes/sha3.js'
import { bytesToHex } from '@noble/hashes/utils.js'
import { secp256k1 } from '@noble/curves/secp256k1.js'
import { ed25519 } from '@noble/curves/ed25519.js'
import { decodeHex } from './jcs'

export const ALGO_ED25519 = 'ed25519'
export const ALGO_ECDSA_SECP256K1 = 'ecdsa-secp256k1'

interface WirePublicKey {
  algo: string
  public_key: string
}

/**
 * Verify a keyset endorsement signature under the identity key
 * (`verify_keyset_endorsement`).
 *
 *  - `ed25519`: 64-byte RFC 8032 signature over `payload`.
 *  - `ecdsa-secp256k1`: 64-byte `r || s` over `sha256(payload)`.
 *
 * NB: @noble's `secp256k1.verify` hashes its message argument with the curve
 * hash (sha256), so we pass the raw `payload` — matching k256's `Verifier`,
 * which likewise hashes the message. `lowS: false` mirrors k256 verification,
 * which does not reject high-S signatures.
 */
export function verifyKeysetEndorsement(
  identity: WirePublicKey,
  payload: Uint8Array,
  signature: Uint8Array,
): boolean {
  try {
    const pub = decodeHex(identity.public_key)
    if (identity.algo === ALGO_ED25519) {
      if (signature.length !== 64 || pub.length !== 32) return false
      return ed25519.verify(signature, payload, pub)
    }
    if (identity.algo === ALGO_ECDSA_SECP256K1) {
      if (signature.length !== 64) return false
      return secp256k1.verify(signature, payload, pub, { lowS: false })
    }
    return false
  } catch {
    return false
  }
}

/**
 * Verify an ACI receipt signature per §9.4 (`verify_receipt_signature`).
 *
 *  - `ed25519`: raw 64-byte RFC 8032 signature over `canonicalBytes`.
 *  - `ecdsa-secp256k1`: EXACTLY 65 bytes `r || s || v` over
 *    `sha256(canonicalBytes)`; `v` must recover the listed receipt public key.
 *    Bare 64-byte `r || s` (JOSE ES256K) is rejected.
 */
export function verifyReceiptSignature(
  receiptKey: WirePublicKey,
  canonicalBytes: Uint8Array,
  signature: Uint8Array,
): boolean {
  try {
    if (receiptKey.algo === ALGO_ED25519) {
      const pub = decodeHex(receiptKey.public_key)
      if (signature.length !== 64 || pub.length !== 32) return false
      return ed25519.verify(signature, canonicalBytes, pub)
    }
    if (receiptKey.algo === ALGO_ECDSA_SECP256K1) {
      if (signature.length !== 65) return false
      let v = signature[64]
      if (v >= 27 && v <= 30) v -= 27
      if (v > 3) return false
      const compact = signature.slice(0, 64)
      const prehash = sha256(canonicalBytes)
      const recovered = secp256k1.Signature.fromBytes(compact)
        .addRecoveryBit(v)
        .recoverPublicKey(prehash)
      const expected = secp256k1.Point.fromHex(receiptKey.public_key)
      // Compare on the uncompressed encoding, matching the Rust VerifyingKey
      // equality regardless of how the listed public key was encoded.
      return recovered.toHex(false).toLowerCase() === expected.toHex(false).toLowerCase()
    }
    return false
  } catch {
    return false
  }
}

/**
 * `0x` + keccak256(uncompressed pubkey without the 0x04 tag)[12..] — mirrors
 * `ethereum_address_from_uncompressed_public_key` (legacy `signing_address`).
 */
export function ethereumAddressFromUncompressedPublicKey(publicKeyHex: string): string {
  const bytes = decodeHex(publicKeyHex)
  let body: Uint8Array
  if (bytes.length === 65 && bytes[0] === 0x04) body = bytes.slice(1)
  else if (bytes.length === 64) body = bytes
  else throw new Error(`secp256k1 public key must be 64 or 65 bytes, got ${bytes.length}`)
  return '0x' + bytesToHex(keccak_256(body)).slice(-40)
}
