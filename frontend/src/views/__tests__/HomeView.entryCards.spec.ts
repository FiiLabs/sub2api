/**
 * APEXONE-EXT: 首页的双入口。
 *
 * 这个 spec 守两件事：两张卡片**都**在第一屏（供给入口曾经是三个按钮里最不起眼的
 * 那个，那正是要解决的问题），以及登录后供给功能关着时它不出现——从一张
 * 「我有闲置订阅」点进去撞见"尚未开放"，比从没见过这张卡更伤。
 *
 * /proof 那条链接一并盯着：它降级成了文字链接，很容易在下一次收拾 hero 时被顺手删掉。
 */
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import zh from '@/i18n/locales/zh'
import HomeView from '../HomeView.vue'

const { authStore, supplyStore, fetchPublicSettings } = vi.hoisted(() => ({
  authStore: {
    isAuthenticated: false,
    isAdmin: false,
    user: null as { id: number } | null,
    checkAuth: vi.fn(),
  },
  supplyStore: { enabled: false, loaded: true, ensureStatus: vi.fn() },
  fetchPublicSettings: vi.fn(),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings,
  }),
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/supply', () => ({ useSupplyStore: () => supplyStore }))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => {
        const value = key.split('.').reduce<unknown>((current, part) => {
          if (current && typeof current === 'object' && part in current) {
            return (current as Record<string, unknown>)[part]
          }
          return undefined
        }, zh)
        return typeof value === 'string' ? value : key
      },
      // HomeView 用 locale 决定共享者视频取中文版还是英文版。
      // 这份 mock 喂的是 zh 文案，locale 跟着写 zh，两者别对不上。
      locale: ref('zh'),
    }),
  }
})

function mountHome() {
  return mount(HomeView, {
    global: {
      stubs: {
        Icon: true,
        LocaleSwitcher: true,
        VideoPlayer: true,
        RouterLink: {
          props: ['to'],
          template: '<a :href="typeof to === \'string\' ? to : \'#\'"><slot /></a>',
        },
      },
    },
  })
}

describe('HomeView dual entry cards', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    authStore.isAuthenticated = false
    authStore.user = null
    supplyStore.enabled = false
  })

  it('gives both sides of the market an equally weighted entry card', () => {
    const wrapper = mountHome()

    const usage = wrapper.get('[data-testid="home-entry-usage"]')
    const supply = wrapper.get('[data-testid="home-entry-supply"]')

    expect(usage.text()).toContain('我要用 AI')
    expect(supply.text()).toContain('我有闲置订阅')
    // 副文案各自讲清自己那一侧的价值主张，缺了就只剩两个空标题。
    expect(usage.text()).toContain('官方 API 的 1.4 折')
    expect(supply.text()).toContain('闲置额度变成收入')
    expect(supply.text()).toContain('提现到链上')

    // 同一份卡片样式 = 视觉权重相当。谁被改小了这条会先炸。
    expect(usage.classes().join(' ')).toBe(supply.classes().join(' '))

    // 消费入口未登录指向登录页；供给入口是本页锚点（/supply 要登录，
    // 直接跳过去只会被守卫弹回来，访客不知道自己错过了什么）。
    expect(usage.attributes('href')).toBe('/login')
    expect(supply.attributes('href')).toBe('#supply')

    wrapper.unmount()
  })

  it('keeps the proof link on the page as a secondary text link', () => {
    const wrapper = mountHome()

    const proofLinks = wrapper.findAll('a[href="/proof"]')
    expect(proofLinks.length).toBeGreaterThan(0)
    expect(proofLinks.some((link) => link.text().includes('验证隐私'))).toBe(true)
    // 降级的意思是不再是按钮，而不是不再存在。
    expect(proofLinks[0].classes()).not.toContain('btn-primary')

    wrapper.unmount()
  })

  it('still shows the supply card to visitors, whose supply status is unknowable', () => {
    // 未登录读不到按用户的开关。这张卡是获客入口，此时一律显示。
    const wrapper = mountHome()

    expect(wrapper.find('[data-testid="home-entry-supply"]').exists()).toBe(true)
    expect(supplyStore.ensureStatus).not.toHaveBeenCalled()

    wrapper.unmount()
  })

  it('drops the supply card once a logged-in user is known to have supply switched off', () => {
    authStore.isAuthenticated = true
    authStore.user = { id: 7 }
    supplyStore.enabled = false

    const wrapper = mountHome()

    expect(wrapper.find('[data-testid="home-entry-usage"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-entry-supply"]').exists()).toBe(false)
    expect(supplyStore.ensureStatus).toHaveBeenCalled()

    wrapper.unmount()
  })

  it('shows it again when that user does have supply available', () => {
    authStore.isAuthenticated = true
    authStore.user = { id: 7 }
    supplyStore.enabled = true

    const wrapper = mountHome()

    expect(wrapper.find('[data-testid="home-entry-supply"]').exists()).toBe(true)

    wrapper.unmount()
  })
})
