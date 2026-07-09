import { describe, expect, it } from 'vitest'
import {
  classifyHttpFailure,
  mkFailure,
  type FailureCategory,
  type FailureSeverity,
} from '../e2eeProof'

describe('classifyHttpFailure', () => {
  const cases: Array<{ status: number; message?: string; expected: FailureCategory }> = [
    { status: 401, expected: 'auth' },
    { status: 402, expected: 'quota' },
    // 403 is ambiguous: only a balance-worded message counts as a quota case.
    { status: 403, message: 'Insufficient account balance', expected: 'quota' },
    { status: 403, message: '余额不足', expected: 'quota' },
    { status: 403, message: 'forbidden: ip not allowed', expected: 'other' },
    { status: 403, expected: 'other' },
    { status: 429, expected: 'rate_limit' },
    { status: 400, expected: 'invalid_request' },
    { status: 500, expected: 'service' },
    { status: 502, expected: 'service' },
    { status: 503, expected: 'service' },
    { status: 504, expected: 'service' },
    // Unknown / unmapped statuses fall through to the generic bucket.
    { status: 418, expected: 'other' },
    { status: 410, expected: 'other' },
  ]

  it.each(cases)('$status "$message" → $expected', ({ status, message, expected }) => {
    expect(classifyHttpFailure(status, message)).toBe(expected)
  })
})

describe('mkFailure — category → severity', () => {
  const cases: Array<{ category: FailureCategory; severity: FailureSeverity }> = [
    { category: 'quota', severity: 'operational' },
    { category: 'auth', severity: 'operational' },
    { category: 'rate_limit', severity: 'operational' },
    { category: 'invalid_request', severity: 'operational' },
    { category: 'service', severity: 'operational' },
    { category: 'other', severity: 'operational' },
    { category: 'network', severity: 'inconclusive' },
    { category: 'trust', severity: 'trust' },
  ]

  it.each(cases)('$category → $severity', ({ category, severity }) => {
    expect(mkFailure(category).severity).toBe(severity)
  })

  it('carries status + rawDetail through', () => {
    const f = mkFailure('quota', { status: 402, rawDetail: 'Insufficient account balance' })
    expect(f).toEqual({
      category: 'quota',
      severity: 'operational',
      status: 402,
      rawDetail: 'Insufficient account balance',
    })
  })
})
