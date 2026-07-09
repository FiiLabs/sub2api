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

  it('renders ApexOne as a single private Claude route without overclaiming model proof', () => {
    const wrapper = mountHomeView()
    const text = wrapper.text()

    expect(text).toContain('The AI gateway you can actually verify, at half the official API price.')
    expect(text).toContain('Claude Fable 5 live · TEE-attested private routing')
    expect(text).toContain('Anthropic receives plaintext under its own policy')
    expect(text).toContain('50% of official API pricing')
    expect(text).toContain('Claude Fable 5 live, GPT and Gemini next')
    expect(text).toContain('GPT coming soon')
    expect(text).toContain('99.99% availability target')
    expect(text).toContain('TEE-verified gateway')
    expect(text).toContain('Metadata-only audit logs')
    expect(text).toContain('ApexOne')
    expect(text).toContain('ApexOne — Pay as you go')
    expect(text).toContain('Claude is a trademark of Anthropic, PBC.')

    expect(text).not.toContain('Verified-real models')
    expect(text).not.toContain('Real-model guarantee')
    expect(text).not.toContain('90-day availability')
    expect(text).not.toContain('Uptime SLA')
    expect(text).not.toContain('GPT-5.x')
    expect(text).not.toContain('Anthropic 4.x')
    expect(text).not.toContain('never silently swapped')
    expect(text).not.toContain('full-precision')
    expect(text).not.toContain('Explore Team')
    expect(text).not.toContain('PublicAI Team')
    expect(text).not.toContain('From 10% of list price')
    expect(wrapper.find('a[href="#team"]').exists()).toBe(false)
    expect(wrapper.findAll('a[href="/proof"]').length).toBeGreaterThan(0)

    wrapper.unmount()
  })
})
