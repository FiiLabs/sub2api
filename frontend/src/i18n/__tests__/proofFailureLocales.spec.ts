import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

// Categories mirror FailureCategory in utils/attestation/e2eeProof.ts. ProofView
// resolves titles/bodies as `proof.e2ee.live.failure.<category>.{title,body}`, so
// a typo or a missing key would silently render the raw key path in the UI.
const CATEGORIES = [
  'quota',
  'auth',
  'rate_limit',
  'invalid_request',
  'service',
  'network',
  'trust',
  'other',
] as const

// Labels resolved by resolveAction() in ProofView.vue.
const ACTIONS = ['topup', 'registerTopup', 'manageKeys', 'login', 'retry'] as const

const locales = { en, zh } as const

describe('proof live-test failure locale keys', () => {
  for (const [name, locale] of Object.entries(locales)) {
    const failure = (locale as any).proof.e2ee.live.failure
    const action = (locale as any).proof.e2ee.live.action

    it(`${name}: every failure category has a non-empty title + body`, () => {
      for (const cat of CATEGORIES) {
        expect(typeof failure[cat]?.title, `${name}.${cat}.title`).toBe('string')
        expect(failure[cat].title.length, `${name}.${cat}.title`).toBeGreaterThan(0)
        expect(typeof failure[cat]?.body, `${name}.${cat}.body`).toBe('string')
        expect(failure[cat].body.length, `${name}.${cat}.body`).toBeGreaterThan(0)
      }
    })

    it(`${name}: has the shared prefix + note`, () => {
      expect(failure.encryptedPrefix?.length).toBeGreaterThan(0)
      expect(failure.unaffectedNote?.length).toBeGreaterThan(0)
    })

    it(`${name}: every action label exists`, () => {
      for (const key of ACTIONS) {
        expect(typeof action[key], `${name}.action.${key}`).toBe('string')
        expect(action[key].length, `${name}.action.${key}`).toBeGreaterThan(0)
      }
    })
  }
})
