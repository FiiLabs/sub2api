import { describe, expect, it } from 'vitest'
import { AAD_REQ, bytesToHex, eciesDecrypt, eciesEncrypt, generateClientKeypair } from '../e2ee'
import { secp256k1 } from '@noble/curves/secp256k1.js'

describe('e2ee ECIES', () => {
  it('roundtrips and rejects tampering', () => {
    const peer = generateClientKeypair()
    const msg = new TextEncoder().encode('hello enclave')
    const wire = eciesEncrypt(peer.pubHex, msg, AAD_REQ)
    expect(bytesToHex(eciesDecrypt(peer.priv, wire, AAD_REQ))).toBe(bytesToHex(msg))
    const other = secp256k1.utils.randomSecretKey()
    expect(() => eciesDecrypt(other, wire, AAD_REQ)).toThrow()
    const tampered = wire.slice()
    tampered[tampered.length - 1] ^= 1
    expect(() => eciesDecrypt(peer.priv, tampered, AAD_REQ)).toThrow()
  })
})
