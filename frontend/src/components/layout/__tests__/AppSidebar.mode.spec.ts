/**
 * APEXONE-EXT: 消费者与共享者是两套独立体验（真挂 DOM，不是源码文本断言）。
 *
 * 守的是"完全区分"这件事本身：消费模式看不到共享入口，共享模式看不到消费工具。
 * 只挂 DOM 才守得住——一个写错的过滤、一个漏改的分支，源码断言全都照过。
 *
 * 这与旧版本刻意相反：旧设计是"两组都在、只重排"，这一版是"只显示当前模式那一套"。
 */
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AppSidebar from '../AppSidebar.vue'

const { appStore, authStore, holder } = vi.hoisted(() => ({
  appStore: {
    sidebarCollapsed: false,
    mobileOpen: false,
    backendModeEnabled: false,
    siteName: 'Site',
    siteLogo: '',
    siteVersion: '1.0',
    publicSettingsLoaded: true,
    sidebarScrollTop: 0,
    cachedPublicSettings: { custom_menu_items: [] } as Record<string, unknown>,
    toggleSidebar: vi.fn(),
    setMobileOpen: vi.fn(),
  },
  authStore: { isAdmin: false, isSimpleMode: false },
  holder: { store: null as unknown as { enabled: boolean; canSwitchMode: boolean; mode: 'usage' | 'sharing'; setMode: ReturnType<typeof vi.fn>; ensureStatus: ReturnType<typeof vi.fn> } },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
  useOnboardingStore: () => ({ isCurrentStep: () => false, nextStep: vi.fn() }),
  useAdminSettingsStore: () => ({ opsMonitoringEnabled: true, paymentEnabled: true, customMenuItems: [], fetch: vi.fn() }),
}))

vi.mock('@/stores/supply', async () => {
  const { reactive } = await import('vue')
  holder.store = reactive({ enabled: true, canSwitchMode: true, mode: 'usage' as const, setMode: vi.fn(), ensureStatus: vi.fn() })
  return { useSupplyStore: () => holder.store }
})

vi.mock('@/composables/useBatchImageAccess', async () => {
  const { ref } = await import('vue')
  return { useBatchImageAccess: () => ({ canUseBatchImage: ref(true), refreshBatchImageAccess: vi.fn() }) }
})

vi.mock('@/utils/featureFlags', () => ({
  FeatureFlags: new Proxy({}, { get: () => ({}) }),
  makeSidebarFlag: () => () => true,
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/dashboard' }),
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

async function mountSidebar() {
  const wrapper = mount(AppSidebar, {
    global: {
      stubs: { VersionBadge: true, RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
    },
  })
  await flushPromises()
  return wrapper
}

/** 整份菜单里的所有链接 href。 */
function hrefs(wrapper: Awaited<ReturnType<typeof mountSidebar>>): string[] {
  return wrapper.findAll('.sidebar-nav a').map((a) => a.attributes('href') || '')
}

// 消费侧独有的工具（共享模式下必须一个都看不到）。
const CONSUMER_ONLY = ['/keys', '/usage', '/purchase']
// 两模式共享的出口。
const SHARED = ['/dashboard', '/profile']

describe('AppSidebar consumer/contributor separation by console mode', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    holder.store.enabled = true
    holder.store.canSwitchMode = true   // 真 store 里 canSwitchMode = enabled
    holder.store.mode = 'usage'
    authStore.isSimpleMode = false
  })

  it('usage mode shows consumer tools and never the earning entry', async () => {
    const wrapper = await mountSidebar()
    const hs = hrefs(wrapper)

    expect(hs[0]).toBe('/dashboard')
    for (const p of CONSUMER_ONLY) expect(hs).toContain(p)
    // 消费模式看不到共享入口。
    expect(hs).not.toContain('/supply')

    wrapper.unmount()
  })

  it('sharing mode shows only earning nav — no consumer tools', async () => {
    holder.store.mode = 'sharing'
    const wrapper = await mountSidebar()
    const hs = hrefs(wrapper)

    // 赚钱模式：Dashboard · 共享控制台 · 个人资料，就这三项。
    expect(hs).toEqual(['/dashboard', '/supply', '/profile'])
    // 消费工具一个都看不到。
    for (const p of CONSUMER_ONLY) expect(hs).not.toContain(p)

    wrapper.unmount()
  })

  it('the two shared exits (dashboard, profile) exist in both modes', async () => {
    const usage = await mountSidebar()
    for (const p of SHARED) expect(hrefs(usage)).toContain(p)
    usage.unmount()

    holder.store.mode = 'sharing'
    const sharing = await mountSidebar()
    for (const p of SHARED) expect(hrefs(sharing)).toContain(p)
    sharing.unmount()
  })

  it('the mode switch is the only seam — present when supply is enabled', async () => {
    const wrapper = await mountSidebar()
    expect(wrapper.find('[data-testid="console-mode-switch"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('supply off: no earning entry and no mode switch, in either mode', async () => {
    holder.store.enabled = false
    holder.store.canSwitchMode = false
    holder.store.mode = 'sharing'   // 即便被判进共享模式，功能关着时也不该露出赚钱入口
    const wrapper = await mountSidebar()

    expect(hrefs(wrapper)).not.toContain('/supply')
    expect(wrapper.find('[data-testid="console-mode-switch"]').exists()).toBe(false)

    wrapper.unmount()
  })
})
