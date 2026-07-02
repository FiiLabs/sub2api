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

  it('renders ApexOne as a single verifiable personal plan without Team links', () => {
    const wrapper = mountHomeView()
    const text = wrapper.text()

    expect(text).toContain('Not the cheapest. The only AI gateway you can verify.')
    expect(text).toContain('Verified-real models')
    expect(text).toContain('TEE-attested privacy')
    expect(text).toContain('ApexOne')
    expect(text).toContain('An honest price. Fully provable.')
    expect(text).toContain('ApexOne — Pay as you go')
    expect(text).toContain('just below direct')

    expect(text).not.toContain('Explore Team')
    expect(text).not.toContain('PublicAI Team')
    expect(text).not.toContain('From 10% of list price')
    expect(wrapper.find('a[href="#team"]').exists()).toBe(false)

    wrapper.unmount()
  })
})
