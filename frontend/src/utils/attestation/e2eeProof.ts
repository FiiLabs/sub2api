/**
 * Path A — E2EE hop-1 browser-level proof.
 *
 * Two levels, both browser-local:
 *
 *  1. verifyE2eeChannel() — always-on, free, verification-side only. Confirms the
 *     gateway advertises an ATTESTED secp256k1 E2EE recipient key. The key lives
 *     inside `workload_keyset`, whose digest is folded into report_data and bound
 *     into the TDX quote (all already verified by the ACI binding + hop-1 quote
 *     checks). So a verifier who trusts the quote already knows: encrypt to this
 *     key and ONLY the attested enclave can decrypt — no data is sent.
 *
 *  2. runLiveE2eeRoundtrip() — opt-in. The browser actually ECIES-encrypts a
 *     payload to that key, POSTs it E2EE, and decrypts the E2EE response. A
 *     successful authenticated decrypt proves the enclave held the KMS-released
 *     private key and ran the full channel live. The demo prompt reaches
 *     Anthropic in plaintext once decrypted inside the enclave (disclosed).
 *
 * Nothing renders a fake pass: every failure surfaces honestly.
 */
import type { AttestationReport, KeyedPublicKey } from '@/api/attestation'
import { postE2eeChat } from '@/api/attestation'
import { randomNonce } from '@/api/attestation'
import { utf8ToBytes } from '@noble/hashes/utils.js'
import type { Check, ReportBindingResult } from './reportBinding'
import {
  E2EE_ALGO,
  E2EE_VERSION,
  aadComponentAmbiguous,
  eciesDecrypt,
  eciesEncrypt,
  generateClientKeypair,
  normalizeSecp256k1PublicKeyHex,
  requestMessagesAad,
  responseChatAad,
  selectE2eeKey,
} from './e2ee'
import { E2EE_DEMO_MAX_TOKENS } from '@/constants/attestation'

/** A verification step; `gating: false` marks an informational (non-blocking) row. */
export type E2eeCheck = Check & { gating?: boolean }

export interface E2eeChannelResult {
  /** True iff the gating checks (capability + key present + key attested) pass. */
  ok: boolean
  /** Whether a live round-trip can be attempted (capability advertised + key present). */
  available: boolean
  /** The attested recipient key, if any. */
  e2eeKey: KeyedPublicKey | null
  checks: E2eeCheck[]
}

interface KeyCustodyEntry {
  role?: string
  purpose?: string
  path?: string
  signature_chain?: unknown[]
}
interface KeyCustody {
  provider?: string
  keys?: KeyCustodyEntry[]
}

/**
 * Verification-side proof (always-on, free). `binding` is the already-computed
 * ACI report binding — its verified `workload_keyset_digest` is what makes the
 * E2EE key trustworthy (the key is inside that keyset).
 */
export function verifyE2eeChannel(
  report: AttestationReport,
  binding: ReportBindingResult,
): E2eeChannelResult {
  const checks: E2eeCheck[] = []
  const push = (id: string, ok: boolean, detail?: string, gating = true) =>
    checks.push({ id, ok, detail, gating })

  // 1) capability — the workload advertises it can terminate E2EE v2.
  const versions = report.service_capabilities?.supported_e2ee_versions ?? []
  const capable = versions.includes(E2EE_VERSION)
  push('capability', capable, `supported_e2ee_versions=[${versions.join(',')}]`)

  // 2) key_present — a well-formed secp256k1 recipient key exists.
  const e2eeKey = selectE2eeKey(report)
  let keyPresent = false
  if (!e2eeKey) {
    push('key_present', false, `no ${E2EE_ALGO} key in e2ee_public_keys`)
  } else {
    try {
      normalizeSecp256k1PublicKeyHex(e2eeKey.public_key)
      keyPresent = true
      push('key_present', true, `key_id=${e2eeKey.key_id ?? '-'} algo=${e2eeKey.algo}`)
    } catch (e) {
      push('key_present', false, String(e))
    }
  }

  // 3) key_attested — the key is covered by the quote-bound keyset digest. This
  //    is the gating cryptographic fact: the attested enclave declared this
  //    exclusive decryption key. Relies on the ACI binding's keyset-digest +
  //    report_data checks (the quote→report_data link is checked in hop-1).
  const keysetDigestOk =
    binding.checks.find((c) => c.id === 'workload_keyset_digest')?.ok === true
  const reportDataOk = binding.checks.find((c) => c.id === 'report_data')?.ok === true
  const attested = keyPresent && keysetDigestOk && reportDataOk
  push(
    'key_attested',
    attested,
    attested
      ? 'key ∈ workload_keyset; digest bound to report_data (quote binds report_data in hop-1)'
      : `keyset_digest=${keysetDigestOk} report_data=${reportDataOk}`,
  )

  // 4) key_custody (informational, non-gating) — dstack-KMS released this key to
  //    the attested workload. Full signature_chain verification is a follow-up;
  //    for now we surface its presence, never claim it as proof.
  const custody = report.attestation?.evidence?.key_custody as KeyCustody | undefined
  const custodyEntry = custody?.keys?.find(
    (k) => k.role === 'e2ee' || (k.purpose ?? '').includes('e2ee') || (k.path ?? '').includes('e2ee'),
  )
  const custodyOk =
    custody?.provider === 'dstack-kms' &&
    !!custodyEntry &&
    Array.isArray(custodyEntry.signature_chain) &&
    custodyEntry.signature_chain.length > 0
  push(
    'key_custody',
    custodyOk,
    custody
      ? `provider=${custody.provider ?? '-'} chain_len=${custodyEntry?.signature_chain?.length ?? 0} (chain not yet fully verified)`
      : 'no key_custody evidence',
    false,
  )

  const ok = capable && keyPresent && attested
  return { ok, available: capable && keyPresent, e2eeKey, checks }
}

export interface LiveRoundtripParams {
  apiKey: string
  model: string
  prompt: string
  maxTokens?: number
}

export interface LiveRoundtripResult {
  /** True iff the E2EE response authenticated-decrypted (the live proof). */
  ok: boolean
  /** The decrypted model reply, when the round-trip succeeded. */
  replyText?: string
  checks: E2eeCheck[]
  /** Honest human-readable error when the round-trip could not complete. */
  error?: string
  /** The prompt as sent (echoed for the "who sees what" side-by-side). */
  promptText?: string
  /** The exact wire ciphertext of the prompt — what ANY intermediary sees. */
  requestCiphertext?: string
  /** The wire ciphertext of the reply — also unreadable to intermediaries. */
  responseCiphertext?: string
  /** The attested recipient key id the prompt was sealed to. */
  sealedToKeyId?: string
}

/**
 * Live round-trip proof (opt-in). Encrypts the prompt to the attested E2EE key,
 * POSTs it E2EE, and decrypts the E2EE response. A successful authenticated
 * decrypt is the proof: only the attested enclave (holding the KMS-released
 * private key) could have decrypted our request and produced this reply.
 */
export async function runLiveE2eeRoundtrip(
  report: AttestationReport,
  params: LiveRoundtripParams,
): Promise<LiveRoundtripResult> {
  const checks: E2eeCheck[] = []
  const push = (id: string, ok: boolean, detail?: string) => checks.push({ id, ok, detail })

  const e2eeKey = selectE2eeKey(report)
  if (!e2eeKey) {
    return { ok: false, checks, error: `no ${E2EE_ALGO} key advertised on this deployment` }
  }
  const model = params.model.trim()
  if (!model || aadComponentAmbiguous(model)) {
    return { ok: false, checks, error: 'invalid model id (empty or contains | CR LF)' }
  }
  if (!params.apiKey.trim()) {
    return { ok: false, checks, error: 'an API key is required to run the live inference round-trip' }
  }

  let modelKeyHex: string
  try {
    modelKeyHex = normalizeSecp256k1PublicKeyHex(e2eeKey.public_key)
  } catch (e) {
    return { ok: false, checks, error: `attested E2EE key is malformed: ${String(e)}` }
  }

  const client = generateClientKeypair()
  const nonce = randomNonce(16)
  const timestamp = Math.floor(Date.now() / 1000)

  // Encrypt the prompt to the attested key, bound to the request AAD.
  const reqAad = requestMessagesAad({ model, messageIndex: 0, contentIndex: null, nonce, timestamp })
  const ciphertext = eciesEncrypt(modelKeyHex, utf8ToBytes(params.prompt), reqAad)
  push('request_encrypted', true, `ciphertext ${ciphertext.length / 2} bytes, sealed to attested key`)

  // Surfaced to the UI so a viewer can SEE the proof: the plaintext they typed,
  // the wire ciphertext an intermediary is limited to, and the attested key it
  // was sealed to. Present from here on, even if a later step fails.
  const base = {
    promptText: params.prompt,
    requestCiphertext: ciphertext,
    sealedToKeyId: e2eeKey.key_id ?? undefined,
  }

  const headers: Record<string, string> = {
    Authorization: `Bearer ${params.apiKey.trim()}`,
    'X-E2EE-Version': E2EE_VERSION,
    'X-Client-Pub-Key': client.publicKeyHex,
    'X-Model-Pub-Key': modelKeyHex,
    'X-E2EE-Nonce': nonce,
    'X-E2EE-Timestamp': String(timestamp),
  }
  const body = {
    model,
    messages: [{ role: 'user', content: ciphertext }],
    max_tokens: params.maxTokens ?? E2EE_DEMO_MAX_TOKENS,
    stream: false,
  }

  let resp
  try {
    resp = await postE2eeChat(headers, body)
  } catch (e) {
    push('response_received', false, String(e))
    return { ok: false, checks, error: `request failed (network/CORS): ${String(e)}`, ...base }
  }
  if (!resp.ok) {
    push('response_received', false, `HTTP ${resp.status}`)
    const detail =
      (resp.body as { error?: { message?: string } } | undefined)?.error?.message ?? resp.rawText.slice(0, 200)
    return { ok: false, checks, error: `gateway returned HTTP ${resp.status}: ${detail}`, ...base }
  }
  push('response_received', true, `HTTP ${resp.status}`)

  // Pull the encrypted assistant content out of the E2EE response.
  const parsed = resp.body as
    | { id?: string; choices?: { message?: { content?: string } }[] }
    | undefined
  const responseId = parsed?.id ?? ''
  const encryptedContent = parsed?.choices?.[0]?.message?.content
  if (typeof encryptedContent !== 'string' || encryptedContent.length === 0) {
    push('response_encrypted', false, 'no choices[0].message.content in response')
    return { ok: false, checks, error: 'response had no encrypted content to decrypt', ...base }
  }
  push('response_encrypted', true, `id=${responseId} ciphertext ${encryptedContent.length / 2} bytes`)

  // THE proof: authenticated-decrypt the response with our client key + the
  // response AAD. Success ⟹ the enclave decrypted our request and E2EE-replied.
  try {
    const respAad = responseChatAad({
      model,
      responseId,
      choiceIndex: 0,
      fieldName: 'content',
      nonce,
      timestamp,
    })
    const plaintext = eciesDecrypt(client.privateKey, encryptedContent, respAad)
    const replyText = new TextDecoder().decode(plaintext)
    push('response_decrypted', true, 'authenticated decrypt OK — enclave-exclusive key proven')
    return { ok: true, replyText, checks, ...base, responseCiphertext: encryptedContent }
  } catch (e) {
    push('response_decrypted', false, String(e))
    return { ok: false, checks, error: `response decrypt failed: ${String(e)}`, ...base, responseCiphertext: encryptedContent }
  }
}
