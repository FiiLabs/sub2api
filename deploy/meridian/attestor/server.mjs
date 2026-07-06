#!/usr/bin/env node
// Meridian attestation sidecar (enclave B). Exposes a FRESH, nonce-bound Intel
// TDX DCAP quote + dstack event log over a tiny HTTP endpoint with permissive
// CORS, so a browser can hardware-verify THIS Meridian CVM the same way it
// verifies the gateway (enclave A). Rationale for a separate process:
//   * Meridian (@rynfar/meridian, :3456) has NO attestation endpoint.
//   * dstack's public :8090 gateway Info page exposes measurements but NO quote
//     and NO CORS, so a browser can't fetch a nonce-bound quote from it.
// This sidecar fills that gap. It carries NO secrets and NO prompt/response
// content — only PUBLIC hardware-verification material — so it needs no auth and
// no sealed env (keeping the measured compose clean; see the sealed-env
// discipline in ../entrypoint.sh / ../compose.cvm.yaml).
//
// Contract (must match the frontend verifier in
// frontend/src/utils/attestation/tdxVerify.ts):
//   GET /attestation/quote?nonce=<hex, up to 32 bytes>
//     -> 200, header  Access-Control-Allow-Origin: *   (+ CORS preflight OPTIONS)
//     -> JSON { "quote":     "<hex raw TDX DCAP quote>",
//               "event_log": <dstack event log — a JSON string of
//                             {imr,event_type,digest,event,event_payload}[],
//                             SAME shape as the gateway's evidence.event_log>,
//               "vm_config": <optional string> }
//   report_data binding: the quote's 64-byte report_data is
//     nonceBytes (left-justified into a 32-byte field) || zeros(32).
//   The browser checks report_data[0..32] == the nonce it sent (freshness).

import http from 'node:http'
import { DstackClient } from '@phala/dstack-sdk'

const HOST = process.env.ATTESTOR_HOST || '0.0.0.0'
const PORT = Number(process.env.ATTESTOR_PORT || 8091)
// dstack 0.5.9 guest-agent socket (mounted into this container by the compose).
const DSTACK_SOCKET = process.env.DSTACK_SOCKET || '/var/run/dstack.sock'

const CORS = {
  'Access-Control-Allow-Origin': '*',
  'Access-Control-Allow-Methods': 'GET, OPTIONS',
  'Access-Control-Allow-Headers': '*',
  'Access-Control-Max-Age': '86400',
}

function sendJson(res, status, body) {
  res.writeHead(status, { 'Content-Type': 'application/json', ...CORS })
  res.end(JSON.stringify(body))
}

// nonce (hex, up to 32 bytes) -> 64-byte report_data:
//   nonce (left-justified into bytes [0..32]) || zeros -> [len..32] and [32..64] = 0.
// getQuote sends this RAW (no hashing), so the quote's report_data is exactly
// this buffer. Throws on a malformed / oversized nonce (-> HTTP 400).
function reportDataFromNonce(nonceHex) {
  const hex = (nonceHex || '').replace(/^0x/i, '')
  if (hex.length === 0) throw new Error('nonce query param is required (hex, up to 32 bytes)')
  if (hex.length % 2 !== 0 || /[^0-9a-fA-F]/.test(hex)) throw new Error('nonce must be valid hex')
  if (hex.length > 64) throw new Error('nonce must be at most 32 bytes (64 hex chars)')
  const reportData = Buffer.alloc(64) // zero-filled: pads the nonce field and the trailing 32 bytes
  Buffer.from(hex, 'hex').copy(reportData, 0) // left-justify nonce into [0..32]
  return reportData
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host || 'localhost'}`)

  // CORS preflight.
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
  if (req.method !== 'GET' || url.pathname !== '/attestation/quote') {
    sendJson(res, 404, { error: 'not found; use GET /attestation/quote?nonce=<hex>' })
    return
  }

  let reportData
  try {
    reportData = reportDataFromNonce(url.searchParams.get('nonce'))
  } catch (e) {
    sendJson(res, 400, { error: String(e?.message || e) })
    return
  }

  try {
    // Constructing the client validates the guest-agent socket exists; getQuote
    // sends report_data RAW (<=64 bytes) so the quote's report_data == nonce||zeros.
    // event_log is dstack's JSON-string RTMR event log (same shape as the gateway).
    const client = new DstackClient(DSTACK_SOCKET)
    const q = await client.getQuote(reportData)
    sendJson(res, 200, {
      quote: String(q.quote).replace(/^0x/i, ''), // raw hex, no 0x prefix
      event_log: q.event_log, // JSON string of {imr,event_type,digest,event,event_payload}[]
      vm_config: q.vm_config,
    })
  } catch (e) {
    // e.g. running outside a CVM: "Unix socket file /var/run/dstack.sock does not exist".
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
