/**
 * ECIES client for the enclave E2EE key-possession proof. Mirrors the scheme
 * in deploy/attestor/server.mjs byte-for-byte:
 *   wire = ephemeral_pub(65) || aes_nonce(12) || ciphertext+tag
 *   shared_x = ECDH(secp256k1)[1..33]; key = HKDF-SHA256(shared_x, info=INFO)
 *   AES-256-GCM with AAD "v1|req|" (request) / "v1|resp|" (response).
 * Crypto is delegated to @noble/* — nothing hand-rolled.
 */
import { secp256k1 } from '@noble/curves/secp256k1.js'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { bytesToHex, hexToBytes } from '@noble/hashes/utils.js'
import { gcm } from '@noble/ciphers/aes.js'

export const E2EE_ALGO = 'secp256k1-aes-256-gcm-hkdf-sha256'
const INFO = new TextEncoder().encode('publicai.e2ee.v1.secp256k1')
export const AAD_REQ = new TextEncoder().encode('v1|req|')
export const AAD_RESP = new TextEncoder().encode('v1|resp|')

export function sha256Hex(bytes: Uint8Array): string {
  return bytesToHex(sha256(bytes))
}
export { bytesToHex, hexToBytes }

function deriveAesKey(sharedX: Uint8Array): Uint8Array {
  return hkdf(sha256, sharedX, undefined, INFO, 32)
}

export function generateClientKeypair(): { priv: Uint8Array; pubHex: string } {
  const priv = secp256k1.utils.randomSecretKey()
  return { priv, pubHex: bytesToHex(secp256k1.getPublicKey(priv, false)) }
}

export function eciesEncrypt(peerPubHex: string, plaintext: Uint8Array, aad: Uint8Array): Uint8Array {
  const ephPriv = secp256k1.utils.randomSecretKey()
  const ephPub = secp256k1.getPublicKey(ephPriv, false)
  const sharedX = secp256k1.getSharedSecret(ephPriv, hexToBytes(peerPubHex), false).slice(1, 33)
  const nonce = crypto.getRandomValues(new Uint8Array(12))
  const ct = gcm(deriveAesKey(sharedX), nonce, aad).encrypt(plaintext)
  const out = new Uint8Array(65 + 12 + ct.length)
  out.set(ephPub, 0)
  out.set(nonce, 65)
  out.set(ct, 77)
  return out
}

export function eciesDecrypt(priv: Uint8Array, wire: Uint8Array, aad: Uint8Array): Uint8Array {
  if (wire.length < 65 + 12 + 16) throw new Error('ciphertext too short')
  const sharedX = secp256k1.getSharedSecret(priv, wire.slice(0, 65), false).slice(1, 33)
  return gcm(deriveAesKey(sharedX), wire.slice(65, 77), aad).decrypt(wire.slice(77))
}
