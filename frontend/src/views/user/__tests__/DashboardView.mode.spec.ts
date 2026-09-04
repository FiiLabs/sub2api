/**
 * APEXONE-EXT: 控制台的双模式。
 *
 * 共享模式下顶部换成供给侧的数（消费侧那四个数对供给者一个都不成立），切过去要
 * 补拉一次，否则看到一屏 0。模式切换器本身已移到侧栏常驻（见 AppSidebar.mode.spec），
 * 这里只保证 Dashboard 不再自带一个（否则同页重复）。
 */
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import DashboardView from '../DashboardView.vue'

const { supplyApi, holder, usageApi, authStore, getMyPlatformQuotas } = vi.hoisted(() => ({
  supplyApi: { getWallet: vi.fn(), listAccounts: vi.fn() },
  // 这个 mock 必须是**响应式**的：一半的用例测的就是"模式变了之后组件怎么办"，
  // 用普通对象的话那些 watch 一次都不会触发，用例会以为功能没接上。
  holder: { store: null as unknown as { mode: 'usage' | 'sharing'; canSwitchMode: boolean; setMode: ReturnType<typeof vi.fn>; ensureStatus: ReturnType<typeof vi.fn> } },
  usageApi: {
    getDashboardStats: vi.fn(),
    getDashboardTrend: vi.fn(),
    getDashboardModels: vi.fn(),
    getByDateRange: vi.fn(),
  },
  authStore: { user: { id: 1, balance: 10 }, isSimpleMode: false, refreshUser: vi.fn() },
  getMyPlatformQuotas: vi.fn(),
}))

vi.mock('@/api/supply', () => ({ supplyAPI: supplyApi }))
vi.mock('@/stores/supply', async () => {
  const { reactive } = await import('vue')
  holder.store = reactive({ mode: 'usage' as const, canSwitchMode: false, setMode: vi.fn(), ensureStatus: vi.fn() })
  return { useSupplyStore: () => holder.store }
})
vi.mock('@/stores/auth', () => ({ useAuthStore: () => authStore }))
vi.mock('@/api/usage', () => ({ usageAPI: usageApi }))
vi.mock('@/api/user', () => ({ getMyPlatformQuotas }))
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

// 消费侧那几个子组件在这里无关紧要，但它们各自还要拉 store/接口。
// 换成占位可以让这个 spec 只回答"哪一格被换掉了"。
const stubs = {
  AppLayout: { template: '<main><slot /></main>' },
  LoadingSpinner: true,
  UserDashboardStats: { template: '<div data-testid="usage-stats" />' },
  UserDashboardCharts: { template: '<div data-testid="charts" />' },
  UserDashboardRecentUsage: { template: '<div data-testid="recent" />' },
  UserDashboardQuickActions: { template: '<div data-testid="usage-actions" />' },
  Icon: true,
}

async function mountDashboard() {
  const wrapper = mount(DashboardView, { global: { stubs } })
  await flushPromises()
  return wrapper
}

describe('DashboardView console modes', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    holder.store.mode = 'usage'
    holder.store.canSwitchMode = false
    usageApi.getDashboardStats.mockResolvedValue({ total_api_keys: 1 })
    usageApi.getDashboardTrend.mockResolvedValue({ trend: [] })
    usageApi.getDashboardModels.mockResolvedValue({ models: [] })
    usageApi.getByDateRange.mockResolvedValue({ items: [] })
    getMyPlatformQuotas.mockResolvedValue({ platform_quotas: [] })
    supplyApi.getWallet.mockResolvedValue({
      available_credit: 12.5,
      frozen_credit: 3,
      history_credit: 40,
      spent_credit: 0,
    })
    supplyApi.listAccounts.mockResolvedValue([
      { id: 1, schedulable: true },
      { id: 2, schedulable: false },
    ])
  })

  it('leaves the usage-mode console exactly as it was', async () => {
    const wrapper = await mountDashboard()

    expect(wrapper.find('[data-testid="usage-stats"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="usage-actions"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="supply-dashboard-stats"]').exists()).toBe(false)
    // 消费者占绝大多数：不该为一格他们看不到的卡片去打两个供给接口。
    expect(supplyApi.getWallet).not.toHaveBeenCalled()

    wrapper.unmount()
  })

  it('swaps the top stats and the main actions in sharing mode', async () => {
    holder.store.mode = 'sharing'
    holder.store.canSwitchMode = true

    const wrapper = await mountDashboard()

    expect(wrapper.find('[data-testid="usage-stats"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="usage-actions"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="supply-stat-available"]').text()).toContain('12.50')
    expect(wrapper.get('[data-testid="supply-stat-history"]').text()).toContain('40.00')
    // 在池账号数，以及"其中几个真在接单"——挂着不等于在赚钱。
    expect(wrapper.get('[data-testid="supply-stat-accounts"]').text()).toContain('2')
    expect(wrapper.get('[data-testid="supply-action-connect"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="supply-action-withdraw"]').exists()).toBe(true)

    // 其余不动：趋势图和最近用量两种身份都看得懂。
    expect(wrapper.find('[data-testid="charts"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="recent"]').exists()).toBe(true)

    wrapper.unmount()
  })

  it('no longer hosts its own mode switch — it moved to the global sidebar', async () => {
    // 2026-09：切换器提到了侧栏顶部常驻（AppSidebar），任何页面都在，不再是
    // Dashboard 的一部分。同一页两个切换器会重复，所以这里断言它**不**在。
    // 切换器本身的存在/门控测试见 AppSidebar.mode.spec.ts。
    holder.store.canSwitchMode = true

    const wrapper = await mountDashboard()

    expect(wrapper.find('[data-testid="console-mode-switch"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('loads the supply numbers when the user switches into sharing mid-session', async () => {
    holder.store.canSwitchMode = true
    const wrapper = await mountDashboard()
    expect(supplyApi.getWallet).not.toHaveBeenCalled()

    holder.store.mode = 'sharing'
    await flushPromises()

    // 不补拉的话，切过去看到的是一屏 0——比看不到更容易被当成"我没赚到钱"。
    expect(supplyApi.getWallet).toHaveBeenCalledTimes(1)
    expect(supplyApi.listAccounts).toHaveBeenCalledTimes(1)

    wrapper.unmount()
  })

  it('walks a zero-account sharer through the three steps', async () => {
    // 一个号都没挂的人，上面那一格必然是四个 0。那不是"我今天没赚到"，
    // 是"我还没开始"——两句话之间的差别只能由这张卡来说。
    holder.store.mode = 'sharing'
    supplyApi.listAccounts.mockResolvedValue([])

    const wrapper = await mountDashboard()

    expect(wrapper.find('[data-testid="supply-guide-card"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="supply-guide-cta"]').exists()).toBe(true)
    // 精简版也要带那句免责：只在完整页出现的免责，等于默认大多数人读不到。
    expect(wrapper.get('[data-testid="supply-guide-disclaimer"]').text()).toBe('supply.guide.after3')

    wrapper.unmount()
  })

  it('does not lecture someone who already connected an account', async () => {
    holder.store.mode = 'sharing'

    const wrapper = await mountDashboard()

    expect(wrapper.find('[data-testid="supply-guide-card"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('keeps the guide out of usage mode entirely', async () => {
    // 消费者看到的控制台一个字都不该变——他没有供给账号这件事在这里没有意义。
    const wrapper = await mountDashboard()

    expect(wrapper.find('[data-testid="supply-guide-card"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('shows zeros instead of blowing up when the supply endpoints fail', async () => {
    holder.store.mode = 'sharing'
    supplyApi.getWallet.mockRejectedValue(new Error('500'))
    supplyApi.listAccounts.mockRejectedValue(new Error('500'))

    const wrapper = await mountDashboard()

    expect(wrapper.get('[data-testid="supply-stat-available"]').text()).toContain('0.00')
    // 接口挂了时账号数停在初值 0，但那是"不知道"不是"零个"：
    // 拿它当"这人还没接入"，会对着一个挂了三个号的老供给者讲"三步开始赚钱"。
    expect(wrapper.find('[data-testid="supply-guide-card"]').exists()).toBe(false)

    wrapper.unmount()
  })
})
