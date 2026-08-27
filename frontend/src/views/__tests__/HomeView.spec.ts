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
    // 这里要的是 matches:false（全局 setup 默认 true），但覆盖时必须把监听方法
    // 一起给全：@vueuse 的 useMediaQuery 会调 addEventListener，退化路径调
    // addListener，只给 { matches } 会让引入了 usePreferredReducedMotion 的
    // 子组件（VideoPlayer）在挂载时抛 TypeError。
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn()
      }))
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
    expect(text).toContain('Claude Fable 5 Is Live')
    expect(text).toContain('22% of official API pricing')
    expect(text).toContain('TEE Privacy Protection')
    expect(text).toContain('Hermes')
    expect(text).toContain('Client Support')

    // Architecture core claim
    expect(text).toContain("We can't sell your data. We can't even read it.")
    expect(text).toContain('plaintext never touches our hands')

    // Comparison table
    expect(text).toContain('22% of the Price. None of the Snooping.')
    expect(text).toContain('Opaque Gateway')
    expect(text).toContain('Official API')
    // 这三行在 proof-solo 上被改写过（tee-control 原文是
    // "Operator Can Read / Resell Your Data" / "Can't — TEE-Sealed" / "Provider Policy"），
    // 断言跟随现行文案，宣称本身不变。
    expect(text).toContain('Prompts Sealed From Operator')
    expect(text).toContain('TEE-sealed, provable')
    expect(text).toContain('Policy only')

    // Conservative availability wording
    expect(text).toContain('Availability design goal')
    expect(text).toContain('Automatic failover')

    // 三条供货通道的数据流向分别陈述:中转通道不在 TEE 密封内,这条披露必须留在页面上
    expect(text).toContain('Platform-owned accounts')
    expect(text).toContain('never through any provider device')
    expect(text).toContain('which is outside the TEE seal')

    // 供给侧区块:分成比例可以承诺,收入金额不能——这句免责是这一段的底线
    expect(text).toContain('Turn idle subscription quota into income.')
    expect(text).toContain('Current revenue share: 80%')
    expect(text).toContain('not any particular amount of income')
    expect(text).toContain('demand still building')
    expect(text).toContain('USDT paid on-chain')
    expect(text).toContain('72-hour hold')

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
    // 任何具体可用性数字都是一句我们没有赔付条款兜底的承诺
    expect(text).not.toContain('99.9')
    expect(text).not.toContain('availability target')
    // 价格对比只与「官方 API」比。规则与理由写在 i18n/locales/*/home.ts 顶部——
    // 这里刻意不列竞品名单：把名单写进仓库，本身就违反了那条规则。
    expect(text).toContain('Official API')
    // 旧的五折口径:改价后任何残留都是价格错标
    expect(text).not.toContain('Half the price')
    expect(text).not.toContain('50%')
    expect(text).not.toContain('0.5x')
    // 带时效的上线措辞过一天就是假的
    expect(text).not.toContain('Live Today')
    // 供给侧不承诺具体收入
    expect(text.toLowerCase()).not.toContain('per month')
    expect(text).not.toContain('guaranteed income')
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
