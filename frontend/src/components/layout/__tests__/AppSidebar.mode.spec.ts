/**
 * APEXONE-EXT: 侧栏两个分区的顺序。
 *
 * 同目录那个 spec 是源码文本断言，它守得住"顺序表还在"，守不住"顺序真的生效了"——
 * 一个写反的三元、一个放错位置的 v-for，源码断言全都照过。所以这一条真挂出来数
 * DOM 顺序。
 *
 * 顺序之外还有一条同样重要：切模式**不改变**任何一组的内容。模式是"我现在在干
 * 哪件事"，不是权限。
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
  holder: { store: null as unknown as { enabled: boolean; mode: 'usage' | 'sharing'; ensureStatus: ReturnType<typeof vi.fn> } },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
  useOnboardingStore: () => ({ isCurrentStep: () => false, nextStep: vi.fn() }),
  useAdminSettingsStore: () => ({ opsMonitoringEnabled: true, paymentEnabled: true, customMenuItems: [], fetch: vi.fn() }),
}))

vi.mock('@/stores/supply', async () => {
  const { reactive } = await import('vue')
  holder.store = reactive({ enabled: true, mode: 'usage' as const, ensureStatus: vi.fn() })
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

/** 供给条目在整份菜单里的位置。-1 = 不在。 */
function supplyLinkIndex(wrapper: Awaited<ReturnType<typeof mountSidebar>>): number {
  return wrapper.findAll('.sidebar-nav a').findIndex((link) => link.attributes('href') === '/supply')
}

describe('AppSidebar section order by console mode', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    holder.store.enabled = true
    holder.store.mode = 'usage'
    authStore.isSimpleMode = false
  })

  it('keeps the consumer group first in usage mode', async () => {
    const wrapper = await mountSidebar()

    const index = supplyLinkIndex(wrapper)
    expect(index).toBeGreaterThan(0)
    // 第一个链接是消费侧的仪表盘，不是供给入口。
    expect(wrapper.findAll('.sidebar-nav a')[0].attributes('href')).toBe('/dashboard')

    wrapper.unmount()
  })

  it('lifts the earning group above the consumer group in sharing mode', async () => {
    holder.store.mode = 'sharing'

    const wrapper = await mountSidebar()

    expect(supplyLinkIndex(wrapper)).toBe(0)

    wrapper.unmount()
  })

  it('does not add or remove a single entry when the mode flips', async () => {
    const usage = await mountSidebar()
    const usageHrefs = usage.findAll('.sidebar-nav a').map((a) => a.attributes('href')).sort()
    usage.unmount()

    holder.store.mode = 'sharing'
    const sharing = await mountSidebar()
    const sharingHrefs = sharing.findAll('.sidebar-nav a').map((a) => a.attributes('href')).sort()
    sharing.unmount()

    expect(sharingHrefs).toEqual(usageHrefs)
  })

  it('renders no earning section at all when supply is off, in either mode', async () => {
    holder.store.enabled = false
    holder.store.mode = 'sharing'

    const wrapper = await mountSidebar()

    // 连标题一起不渲染：一个点开什么都没有的分区看起来像功能坏了。
    expect(supplyLinkIndex(wrapper)).toBe(-1)
    expect(wrapper.text()).not.toContain('supply.navSection')

    wrapper.unmount()
  })
})
