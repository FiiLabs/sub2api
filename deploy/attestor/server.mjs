#!/usr/bin/env node
// sub2api attestation sidecar. Exposes over a tiny HTTP endpoint with
// permissive CORS:
//   1. a FRESH, nonce-bound Intel TDX DCAP quote + dstack event log, so a
//      browser can hardware-verify THIS CVM;
//   2. an enclave-held E2EE keypair (derived from the dstack app KMS, never
//      leaves this CVM) whose PUBLIC key hash is bound into the same quote —
//      plus an encrypt/echo endpoint proving possession of the private key.
// It carries NO secrets beyond the derived E2EE key and NO request/response
// content, so it needs no auth and no sealed env.
//
// Contract (must match frontend/src/utils/attestation/{tdxVerify,e2ee}.ts):
//   GET /attestation/quote?nonce=<hex, up to 32 bytes>
//     -> 200 JSON { quote, event_log, vm_config,
//                   e2ee_public_key?: <hex 65-byte uncompressed secp256k1>,
//                   e2ee_algo?: "secp256k1-aes-256-gcm-hkdf-sha256" }
//   report_data binding: 64 bytes = nonce (left-justified into [0..32])
//     || sha256(e2ee_public_key) into [32..64] (zeros when key unavailable).
//   POST /e2ee/echo  JSON { payload: <hex ECIES blob>, reply_pubkey: <hex 65B> }
//     -> 200 JSON { payload: <hex ECIES blob encrypted to reply_pubkey> }
//   ECIES wire: ephemeral_pub(65) || aes_nonce(12) || ciphertext+tag.
//     shared = ECDH(secp256k1); key = HKDF-SHA256(shared_x, info=E2EE_INFO);
//     AES-256-GCM with AAD "v1|req|" (request) / "v1|resp|" (response).

import http from 'node:http'
import { DstackClient } from '@phala/dstack-sdk'
import { secp256k1 } from '@noble/curves/secp256k1'
import { hkdf } from '@noble/hashes/hkdf'
import { sha256 } from '@noble/hashes/sha256'
import { bytesToHex, hexToBytes } from '@noble/hashes/utils'
import { gcm } from '@noble/ciphers/aes'

const HOST = process.env.ATTESTOR_HOST || '0.0.0.0'
const PORT = Number(process.env.ATTESTOR_PORT || 8091)
const DSTACK_SOCKET = process.env.DSTACK_SOCKET || '/var/run/dstack.sock'

const E2EE_ALGO = 'secp256k1-aes-256-gcm-hkdf-sha256'
const E2EE_INFO = new TextEncoder().encode('publicai.e2ee.v1.secp256k1')
const AAD_REQ = new TextEncoder().encode('v1|req|')
const AAD_RESP = new TextEncoder().encode('v1|resp|')

const CORS = {
  'Access-Control-Allow-Origin': '*',
  'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
  'Access-Control-Allow-Headers': '*',
  'Access-Control-Max-Age': '86400',
}

function sendJson(res, status, body) {
  res.writeHead(status, { 'Content-Type': 'application/json', ...CORS })
  res.end(JSON.stringify(body))
}

// --- E2EE key (derived in-enclave from the dstack app KMS; deterministic per
// app, so it survives restarts and redeploys, and is unobtainable outside). ---
let e2eeKeyPromise = null
async function getE2eeKey() {
  if (!e2eeKeyPromise) {
    e2eeKeyPromise = (async () => {
      const client = new DstackClient(DSTACK_SOCKET)
      const resp = await client.getKey('e2ee/v1', 'encryption')
      let d = resp.key.slice(0, 32)
      while (!secp256k1.utils.isValidPrivateKey(d)) d = sha256(d)
      const pub = secp256k1.getPublicKey(d, false) // 65B uncompressed
      return { priv: d, pub, pubHex: bytesToHex(pub) }
    })().catch((e) => {
      e2eeKeyPromise = null // allow retry on the next request
      throw e
    })
  }
  return e2eeKeyPromise
}

// nonce (hex, up to 32 bytes) -> 64-byte report_data:
//   nonce left-justified into [0..32] || sha256(e2ee pubkey) into [32..64]
//   (zeros when the key is unavailable). Throws on malformed nonce (-> 400).
function reportDataFromNonce(nonceHex, pubkeyBytes) {
  const hex = (nonceHex || '').replace(/^0x/i, '')
  if (hex.length === 0) throw new Error('nonce query param is required (hex, up to 32 bytes)')
  if (hex.length % 2 !== 0 || /[^0-9a-fA-F]/.test(hex)) throw new Error('nonce must be valid hex')
  if (hex.length > 64) throw new Error('nonce must be at most 32 bytes (64 hex chars)')
  const reportData = Buffer.alloc(64)
  Buffer.from(hex, 'hex').copy(reportData, 0)
  if (pubkeyBytes) Buffer.from(sha256(pubkeyBytes)).copy(reportData, 32)
  return reportData
}

// --- ECIES helpers (mirror frontend/src/utils/attestation/e2ee.ts) ---------
function deriveAesKey(sharedX) {
  return hkdf(sha256, sharedX, undefined, E2EE_INFO, 32)
}
function eciesDecrypt(priv, wire, aad) {
  if (wire.length < 65 + 12 + 16) throw new Error('ciphertext too short')
  const eph = wire.slice(0, 65)
  const nonce = wire.slice(65, 77)
  const ct = wire.slice(77)
  const sharedX = secp256k1.getSharedSecret(priv, eph, false).slice(1, 33)
  return gcm(deriveAesKey(sharedX), nonce, aad).decrypt(ct)
}
function eciesEncrypt(peerPub, plaintext, aad) {
  const ephPriv = secp256k1.utils.randomPrivateKey()
  const ephPub = secp256k1.getPublicKey(ephPriv, false)
  const sharedX = secp256k1.getSharedSecret(ephPriv, peerPub, false).slice(1, 33)
  const nonce = crypto.getRandomValues(new Uint8Array(12))
  const ct = gcm(deriveAesKey(sharedX), nonce, aad).encrypt(plaintext)
  const out = new Uint8Array(65 + 12 + ct.length)
  out.set(ephPub, 0)
  out.set(nonce, 65)
  out.set(ct, 77)
  return out
}

function readBody(req, limit = 64 * 1024) {
  return new Promise((resolve, reject) => {
    let size = 0
    const chunks = []
    req.on('data', (c) => {
      size += c.length
      if (size > limit) reject(new Error('body too large'))
      else chunks.push(c)
    })
    req.on('end', () => resolve(Buffer.concat(chunks)))
    req.on('error', reject)
  })
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host || 'localhost'}`)

  if (req.method === 'OPTIONS') {
    res.writeHead(204, CORS)
    res.end()
    return
  }
  // Liveness (does NOT touch the socket, so it works outside a CVM too).
  if (req.method === 'GET' && url.pathname === '/health') {
    sendJson(res, 200, { ok: true, socket: DSTACK_SOCKET })
    return
  }

  // Proof of private-key possession: decrypt inside the enclave, echo back
  // encrypted to the caller's key. No content is logged or stored.
  if (req.method === 'POST' && url.pathname === '/e2ee/echo') {
    try {
      const body = JSON.parse((await readBody(req)).toString('utf8'))
      const wire = hexToBytes(String(body.payload || '').replace(/^0x/i, ''))
      const replyPub = hexToBytes(String(body.reply_pubkey || '').replace(/^0x/i, ''))
      if (replyPub.length !== 65) throw new Error('reply_pubkey must be a 65-byte uncompressed secp256k1 key')
      const { priv } = await getE2eeKey()
      const plaintext = eciesDecrypt(priv, wire, AAD_REQ)
      const reply = new TextEncoder().encode(
        JSON.stringify({ echo: new TextDecoder().decode(plaintext), enclave: 'publicai-gateway-attestor' }),
      )
      sendJson(res, 200, { payload: bytesToHex(eciesEncrypt(replyPub, reply, AAD_RESP)) })
    } catch (e) {
      sendJson(res, 400, { error: String(e?.message || e) })
    }
    return
  }

  if (req.method !== 'GET' || url.pathname !== '/attestation/quote') {
    sendJson(res, 404, { error: 'not found; use GET /attestation/quote?nonce=<hex>' })
    return
  }

  // E2EE key is best-effort: quotes still work if KMS derivation fails.
  let e2ee = null
  try {
    e2ee = await getE2eeKey()
  } catch {
    e2ee = null
  }

  let reportData
  try {
    reportData = reportDataFromNonce(url.searchParams.get('nonce'), e2ee?.pub)
  } catch (e) {
    sendJson(res, 400, { error: String(e?.message || e) })
    return
  }

  try {
    const client = new DstackClient(DSTACK_SOCKET)
    const q = await client.getQuote(reportData)
    sendJson(res, 200, {
      quote: String(q.quote).replace(/^0x/i, ''),
      event_log: q.event_log,
      vm_config: q.vm_config,
      ...(e2ee ? { e2ee_public_key: e2ee.pubHex, e2ee_algo: E2EE_ALGO } : {}),
    })
  } catch (e) {
    sendJson(res, 500, {
      error: 'failed to obtain TDX quote from the dstack guest-agent',
      detail: String(e?.message || e),
      socket: DSTACK_SOCKET,
    })
  }
})

server.listen(PORT, HOST, () => {
  console.log(`[attestor] listening on http://${HOST}:${PORT} (dstack socket: ${DSTACK_SOCKET})`)
})
