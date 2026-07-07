import { describe, it, expect } from 'vitest'
import { bytesToHex, hexToBytes, utf8ToBytes } from '@noble/hashes/utils.js'
import {
  E2EE_ALGO,
  aadComponentAmbiguous,
  eciesDecrypt,
  eciesEncrypt,
  generateClientKeypair,
  normalizeSecp256k1PublicKeyHex,
  requestMessagesAad,
  responseChatAad,
  selectE2eeKey,
} from '../e2ee'

const td = new TextDecoder()

describe('ECIES round-trip (byte-faithful to src/aci/e2ee.rs)', () => {
  it('encrypts to a recipient key and the recipient decrypts back', () => {
    const recipient = generateClientKeypair()
    const aad = requestMessagesAad({ model: 'm', nonce: 'abc', timestamp: 1 })
    const plaintext = utf8ToBytes('hello enclave 🌒')
    const wireHex = eciesEncrypt(recipient.publicKeyHex, plaintext, aad)
    const out = eciesDecrypt(recipient.privateKey, wireHex, aad)
    expect(td.decode(out)).toBe('hello enclave 🌒')
  })

  it('wire layout is ephemeral_pub(65) || nonce(12) || ciphertext_tag', () => {
    const recipient = generateClientKeypair()
    const aad = requestMessagesAad({ model: 'm', nonce: 'n', timestamp: 2 })
    const plaintext = utf8ToBytes('12345')
    const wire = hexToBytes(eciesEncrypt(recipient.publicKeyHex, plaintext, aad))
    // 65 (eph pub) + 12 (nonce) + 5 (pt) + 16 (GCM tag)
    expect(wire.length).toBe(65 + 12 + plaintext.length + 16)
    expect(wire[0]).toBe(0x04) // uncompressed ephemeral point
  })

  it('rejects a tampered AAD (GCM auth fails)', () => {
    const recipient = generateClientKeypair()
    const aad = responseChatAad({ model: 'm', responseId: 'id1', nonce: 'n', timestamp: 3 })
    const wireHex = eciesEncrypt(recipient.publicKeyHex, utf8ToBytes('secret'), aad)
    const wrongAad = responseChatAad({ model: 'm', responseId: 'id2', nonce: 'n', timestamp: 3 })
    expect(() => eciesDecrypt(recipient.privateKey, wireHex, wrongAad)).toThrow()
  })

  it('rejects a wrong recipient key', () => {
    const a = generateClientKeypair()
    const b = generateClientKeypair()
    const aad = requestMessagesAad({ model: 'm', nonce: 'n', timestamp: 4 })
    const wireHex = eciesEncrypt(a.publicKeyHex, utf8ToBytes('secret'), aad)
    expect(() => eciesDecrypt(b.privateKey, wireHex, aad)).toThrow()
  })
})

describe('AAD string format (matches e2ee_crypto.rs)', () => {
  it('request AAD for chat messages content', () => {
    const aad = requestMessagesAad({ model: 'gpt', nonce: 'abc', timestamp: 1 })
    expect(td.decode(aad)).toBe(
      `v2|req|algo=${E2EE_ALGO}|model=gpt|m=0|c=-|n=abc|ts=1`,
    )
  })

  it('request AAD with an explicit content index', () => {
    const aad = requestMessagesAad({ model: 'gpt', contentIndex: 2, nonce: 'abc', timestamp: 1 })
    expect(td.decode(aad)).toBe(
      `v2|req|algo=${E2EE_ALGO}|model=gpt|m=0|c=2|n=abc|ts=1`,
    )
  })

  it('response AAD for chat choice content', () => {
    const aad = responseChatAad({ model: 'gpt', responseId: 'resp_1', nonce: 'abc', timestamp: 1 })
    expect(td.decode(aad)).toBe(
      `v2|resp|algo=${E2EE_ALGO}|model=gpt|id=resp_1|choice=0|field=content|n=abc|ts=1`,
    )
  })

  it('flags ambiguous AAD components', () => {
    expect(aadComponentAmbiguous('ok')).toBe(false)
    expect(aadComponentAmbiguous('a|b')).toBe(true)
    expect(aadComponentAmbiguous('a\nb')).toBe(true)
    expect(aadComponentAmbiguous('a\rb')).toBe(true)
  })
})

describe('normalizeSecp256k1PublicKeyHex (matches normalize_secp256k1_public_key_hex)', () => {
  it('normalizes 64-byte and 65-byte forms to the same 65-byte hex', () => {
    const kp = generateClientKeypair()
    const full = kp.publicKeyHex // 65-byte 04||X||Y
    const bare = full.slice(2) // 64-byte X||Y
    expect(normalizeSecp256k1PublicKeyHex(full)).toBe(full)
    expect(normalizeSecp256k1PublicKeyHex(bare)).toBe(full)
    expect(normalizeSecp256k1PublicKeyHex('0x' + bare)).toBe(full)
  })

  it('rejects wrong-length input', () => {
    expect(() => normalizeSecp256k1PublicKeyHex('04aabbcc')).toThrow()
  })
})

describe('selectE2eeKey', () => {
  it('picks the secp256k1 canonical key from e2ee_public_keys', () => {
    const report = {
      attestation: {
        workload_keyset: {
          workload_identity: { public_key: { algo: 'ecdsa-secp256k1', public_key: '04' } },
          e2ee_public_keys: [
            { key_id: 'legacy', algo: 'ed25519', public_key: 'aa' },
            { key_id: 'k1', algo: E2EE_ALGO, public_key: bytesToHex(new Uint8Array(65)) },
          ],
        },
      },
    }
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const key = selectE2eeKey(report as any)
    expect(key?.key_id).toBe('k1')
    expect(key?.algo).toBe(E2EE_ALGO)
  })

  it('returns null when no e2ee key is advertised', () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect(selectE2eeKey({ attestation: { workload_keyset: {} } } as any)).toBeNull()
  })
})
