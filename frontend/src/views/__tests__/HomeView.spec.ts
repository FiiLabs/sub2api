import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en'
import HomeView from '../HomeView.vue'

const { checkAuth, fetchPublicSettings } = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  fetchPublicSettings: vi.fn()
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    docUrl: 'https://docs.publicai.io',
    publicSettingsLoaded: true,
    fetchPublicSettings
  }),
  useAuthStore: () => ({
    isAuthenticated: false,
    isAdmin: false,
    user: null,
    checkAuth
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => {
        const value = key.split('.').reduce<unknown>((current, part) => {
          if (current && typeof current === 'object' && part in current) {
            return (current as Record<string, unknown>)[part]
          }
          return undefined
        }, en)
        return typeof value === 'string' ? value : key
      }
    })
  }
})

function mountHomeView() {
  return mount(HomeView, {
    global: {
      stubs: {
        Icon: true,
        LocaleSwitcher: true,
        RouterLink: {
          props: ['to'],
          template: '<a :href="typeof to === \'string\' ? to : \'#\'"><slot /></a>'
        }
      }
    }
  })
}

describe('HomeView ApexOne landing content', () => {
  beforeEach(() => {
    localStorage.clear()
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false })
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders the verifiable-privacy landing without overclaiming model proof or uptime', () => {
    const wrapper = mountHomeView()
    const text = wrapper.text()

    // Hero
    expect(text).toContain('Data privacy you can verify. Powered by TEE')
    expect(text).toContain('Claude Fable 5 Live Today')
    expect(text).toContain('0.5x official API price')
    expect(text).toContain('TEE Privacy Protection')
    expect(text).toContain('Hermes')
    expect(text).toContain('Client Support')

    // Architecture core claim
    expect(text).toContain("We can't sell your data. We can't even read it.")
    expect(text).toContain('plaintext never touches our hands')

    // Comparison table
    expect(text).toContain('Half the Price. None of the Snooping.')
    expect(text).toContain('Opaque Gateway')
    expect(text).toContain('Official API')
    expect(text).toContain('Operator Can Read / Resell Your Data')
    expect(text).toContain("Can't — TEE-Sealed")
    expect(text).toContain('Provider Policy')

    // Conservative availability wording
    expect(text).toContain('Availability Design Target')
    expect(text).toContain('Automatic failover')

    // Pricing & CTA
    expect(text).toContain('ApexOne — Pay As You Go')
    expect(text).toContain('GPT and Gemini Coming Soon')
    expect(text).toContain('Build on AI You Can Verify.')
    expect(text).toContain('No Training on Your Data')
    expect(text).toContain('Metadata-Only Audit Logs')
    expect(text).toContain('Claude is a trademark of Anthropic, PBC.')

    // Overclaim guards
    expect(text).not.toContain('Verified-real models')
    expect(text).not.toContain('Real-model guarantee')
    expect(text).not.toContain('90-day availability')
    expect(text).not.toContain('Uptime SLA')
    expect(text).not.toContain('99.99% Uptime')
    expect(text).not.toContain('availability target')
    expect(text).not.toContain('GPT-5')
    expect(text).not.toContain('Anthropic 4.x')
    expect(text).not.toContain('never silently swapped')
    expect(text).not.toContain('full-precision')
    expect(text).not.toContain('or sponsored by')
    expect(text).not.toContain('Explore Team')
    expect(text).not.toContain('PublicAI Team')
    expect(text).not.toContain('From 10% of list price')
    expect(wrapper.find('a[href="#team"]').exists()).toBe(false)
    expect(wrapper.findAll('a[href="/proof"]').length).toBeGreaterThan(0)

    wrapper.unmount()
  })
})
