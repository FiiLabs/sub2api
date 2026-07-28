/**
 * The rule this file exists to protect:
 *
 *   A measurement mismatch is a hard FAIL only when the reference we compared
 *   against is the authoritative copy.
 *
 * Otherwise the /proof page tells visitors "do not trust this service" every
 * time GitHub is unreachable, or whenever the copy baked into the page lags the
 * live enclave — which it always does, by construction. That is a false alarm
 * about the most sensitive claim the page makes.
 */
import { describe, expect, it, vi } from 'vitest'
import { compareToReference } from '../tdxVerify'
import { loadReference } from '../reference'
import {
  REFERENCE_JSON_URL,
  REFERENCE_JSON_MIRROR_URLS,
  SUB2API_REFERENCE,
  type EnclaveReference,
} from '@/constants/attestation'

const REF: EnclaveReference = {
  appId: '9467ce766ed63423e86a19d2f36cc9a9926daf27',
  osImage: 'dstack-0.5.9',
  osImageHash: 'b'.repeat(64),
  composeHash: 'c'.repeat(64),
  confidential: false,
}

const MATCHING = {
  appIdFromEventLog: REF.appId,
  osImageHashFromEventLog: REF.osImageHash,
  composeHashFromEventLog: REF.composeHash,
  composeHashFromConfigId: REF.composeHash,
}
const STALE = { ...MATCHING, composeHashFromEventLog: 'd'.repeat(64) }

describe('compareToReference', () => {
  it('passes every comparison when the measurements match', () => {
    for (const authoritative of [true, false]) {
      const checks = compareToReference(MATCHING, REF, authoritative)
      expect(checks).toHaveLength(4)
      expect(checks.every((c) => c.ok)).toBe(true)
      expect(checks.some((c) => c.indeterminate)).toBe(false)
    }
  })

  it('fails a mismatch against the authoritative reference (real tamper signal)', () => {
    const checks = compareToReference(STALE, REF, true)
    const c = checks.find((x) => x.id === 'measurement_compose_hash_eventlog')!
    expect(c.ok).toBe(false)
    expect(c.indeterminate).toBeUndefined()
    expect(c.gating).toBe(true)
  })

  it('marks a mismatch against a non-authoritative reference undecided, not failed', () => {
    const checks = compareToReference(STALE, REF, false)
    const c = checks.find((x) => x.id === 'measurement_compose_hash_eventlog')!
    expect(c.ok).toBe(false)
    expect(c.indeterminate).toBe(true)
    // Non-gating is what keeps the overall verdict out of the red.
    expect(c.gating).toBe(false)
    // And the other three still report normally.
    expect(checks.filter((x) => x.ok)).toHaveLength(3)
  })
})

function jsonResponse(body: unknown, ok = true): Response {
  return {
    ok,
    json: async () => body,
  } as unknown as Response
}

function referenceJson(overrides: Partial<EnclaveReference> = {}) {
  return {
    version: 1,
    enclave: { ...REF, ...overrides },
    attestor: { baseUrl: 'https://attestor.example/' },
  }
}

describe('loadReference', () => {
  it('prefers the repo copy and marks it authoritative', async () => {
    const fetchImpl = vi.fn(async () => jsonResponse(referenceJson()))
    const loaded = await loadReference(fetchImpl as unknown as typeof fetch)
    expect(loaded.source).toBe('repo')
    expect(loaded.authoritative).toBe(true)
    expect(loaded.url).toBe(REFERENCE_JSON_URL)
    expect(loaded.attestorBaseUrl).toBe('https://attestor.example')
    expect(fetchImpl).toHaveBeenCalledTimes(1)
  })

  it('falls back to a mirror when the repo is unreachable, marked non-authoritative', async () => {
    expect(REFERENCE_JSON_MIRROR_URLS.length).toBeGreaterThan(0)
    const fetchImpl = vi.fn(async (url: string) =>
      url === REFERENCE_JSON_URL ? Promise.reject(new Error('blocked')) : jsonResponse(referenceJson()),
    )
    const loaded = await loadReference(fetchImpl as unknown as typeof fetch)
    expect(loaded.source).toBe('mirror')
    expect(loaded.authoritative).toBe(false)
    expect(loaded.url).toBe(REFERENCE_JSON_MIRROR_URLS[0])
  })

  it('falls back to the baked-in copy when everything is unreachable', async () => {
    const fetchImpl = vi.fn(async () => {
      throw new Error('offline')
    })
    const loaded = await loadReference(fetchImpl as unknown as typeof fetch)
    expect(loaded.source).toBe('baked-in')
    expect(loaded.authoritative).toBe(false)
    expect(loaded.reference).toBe(SUB2API_REFERENCE)
  })

  it('rejects a malformed payload and moves on to the next source', async () => {
    const fetchImpl = vi.fn(async (url: string) =>
      url === REFERENCE_JSON_URL
        ? jsonResponse({ version: 1, enclave: { appId: 'nope' } })
        : jsonResponse(referenceJson()),
    )
    const loaded = await loadReference(fetchImpl as unknown as typeof fetch)
    expect(loaded.source).toBe('mirror')
  })

  it('ignores a non-https attestor baseUrl rather than following it', async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse({ ...referenceJson(), attestor: { baseUrl: 'http://evil.example' } }),
    )
    const loaded = await loadReference(fetchImpl as unknown as typeof fetch)
    expect(loaded.attestorBaseUrl).toBeUndefined()
  })
})
