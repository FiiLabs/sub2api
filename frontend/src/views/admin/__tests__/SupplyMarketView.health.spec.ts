/**
 * APEXONE-EXT: 定价与供给健康度卡片。
 *
 * 这张卡是只读的，所以这里盯的不是"点了会发生什么"，而是四件读错了就会让人
 * 做出错误动作的事：
 *   1. 四格显示的是拿到的数（虚报营收会让人误以为在赚钱）；
 *   2. 切窗口真的重新去问后端，而不是在旧数上换个标题；
 *   3. 偏差高亮只在真的偏了的时候亮（常亮等于没有）；
 *   4. exhausted_today 的红色告警在**没有流水**时也出得来——那正是它最该出现的时刻。
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { SupplyMarketHealth } from '@/api/admin/supplyMarket'
import SupplyMarketView from '../SupplyMarketView.vue'

const {
  getHealth,
  getSettlementSettings,
  getPoolSettings,
  getProbationSettings,
  getOnboardingSettings,
  getAgreementSettings,
  getWithdrawalSettings,
  getPayoutChainSettings,
} = vi.hoisted(() => ({
  getHealth: vi.fn(),
  getSettlementSettings: vi.fn(),
  getPoolSettings: vi.fn(),
  getProbationSettings: vi.fn(),
  getOnboardingSettings: vi.fn(),
  getAgreementSettings: vi.fn(),
  getWithdrawalSettings: vi.fn(),
  getPayoutChainSettings: vi.fn(),
}))

vi.mock('@/api/admin/supplyMarket', () => ({
  adminSupplyMarketAPI: {
    getHealth,
    getSettlementSettings,
    getPoolSettings,
    getProbationSettings,
    getOnboardingSettings,
    getAgreementSettings,
    getWithdrawalSettings,
    getPayoutChainSettings,
  },
}))

const showError = vi.fn()

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess: vi.fn(),
  }),
}))

// t 回 key + 参数：断言要认的是"哪句话出现了、带着什么数"，把真实文案抄进测试
// 等于把文案钉死，改一个错别字就红一片。参数一并吐出来是必须的——这张卡上的
// 数字大半是插进提示语里的（营收、分成、偏差），只回 key 会把它们全吃掉。
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key} ${JSON.stringify(params)}` : key,
    }),
  }
})

function createHealth(overrides: Partial<SupplyMarketHealth> = {}): SupplyMarketHealth {
  return {
    window_days: 30,
    list_value: 10000,
    revenue: 1800,
    supplier_payout: 1530,
    gross_margin: 270,
    effective_multiplier: 0.18,
    configured_multiplier: 0.18,
    effective_share: 0.85,
    configured_share: 0.85,
    overflow_list_value: 2000,
    overflow_share: 0.2,
    exhausted_today: 0,
    supply_accounts: [
      {
        account_id: 1,
        name: 'max-20x-a',
        owner_user_id: 11,
        list_value: 6000,
        monthly_output: 6000,
        supplier_earned: 918,
        requests: 4200,
      },
      {
        account_id: 2,
        name: 'pro-b',
        owner_user_id: 12,
        list_value: 400,
        monthly_output: 400,
        supplier_earned: 61,
        requests: 130,
      },
    ],
    median_monthly_output: 3200,
    supplier_count: 2,
    ...overrides,
  }
}

async function mountView() {
  const wrapper = mount(SupplyMarketView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Toggle: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}

describe('admin SupplyMarketView — pricing & supply health card', () => {
  beforeEach(() => {
    showError.mockReset()
    getHealth.mockReset()
    getHealth.mockResolvedValue(createHealth())

    getSettlementSettings.mockResolvedValue({
      enabled: true,
      share_ratio: 0.85,
      freeze_hours: 720,
      spend_from_wallet_first: false,
      share_ratio_max: 1,
      freeze_hours_max: 2160,
    })
    getPoolSettings.mockResolvedValue({
      enabled: true,
      supply_group_id: 3,
      overflow_group_id: 4,
      daily_overflow_limit: 0,
      usage_day: '2026-08-26',
      overflow_used_today: 0,
      overflow_denied_today: 0,
    })
    getProbationSettings.mockResolvedValue({
      enabled: false,
      min_observation_minutes: 60,
      required_successes: 2,
      probe_interval_minutes: 15,
      probe_model: '',
      drain_window_minutes: 10,
      min_observation_minutes_max: 43200,
      required_successes_max: 20,
      probe_interval_minutes_min: 5,
      probe_interval_minutes_max: 1440,
      drain_window_minutes_max: 1440,
    })
    getOnboardingSettings.mockResolvedValue({
      relay_enabled: false,
      max_accounts_per_user: 5,
      max_accounts_per_ip: 0,
      user_cap_enabled: true,
      ip_cap_enabled: false,
      max_accounts_per_user_cap: 100,
      max_accounts_per_ip_cap: 10000,
    })
    getAgreementSettings.mockResolvedValue({
      version: 'v1',
      url: '',
      body: '',
      published: true,
      version_max_len: 64,
      url_max_len: 512,
      body_max_len: 100000,
    })
    getWithdrawalSettings.mockResolvedValue({
      enabled: false,
      min_amount: 100,
      max_pending: 3,
      notice: '',
      notify_emails: [],
      available: false,
      notify_configured: false,
      min_amount_max: 1000000,
      max_pending_cap: 20,
      notice_max_len: 1000,
      notify_emails_max: 10,
      notify_email_max_len: 254,
    })
    getPayoutChainSettings.mockResolvedValue({
      enabled: false,
      rpc_url: '',
      token_address: '',
      token_symbol: 'USDT',
      disperse_address: '',
      chain_id: 56,
      confirmations: 3,
      signer_configured: false,
      status: { mode: 'disabled', summary: 'disabled', source: 'none', applied_at: '' },
    })
  })

  it('renders the four headline numbers as returned', async () => {
    const wrapper = await mountView()

    expect(getHealth).toHaveBeenCalledWith(30)
    // 牌价等值与实付分开显示：把 list_value 当营收会把收入虚报五倍以上。
    expect(wrapper.get('[data-testid="supply-health-list-value"]').text()).toContain('$10,000.00')
    expect(wrapper.get('[data-testid="supply-health-list-value"]').text()).toContain('$1,800.00')
    expect(wrapper.get('[data-testid="supply-health-gross-margin"]').text()).toContain('$270.00')
    expect(wrapper.get('[data-testid="supply-health-median-output"]').text()).toContain('$3,200.00')
    expect(wrapper.get('[data-testid="supply-health-overflow-share"]').text()).toContain('20.0%')
  })

  it('lists supply accounts in backend order and flags the ones under the $1500 line', async () => {
    const wrapper = await mountView()

    const rows = wrapper.findAll('[data-testid^="supply-health-account-"]')
      .filter(row => row.element.tagName === 'TR')
    expect(rows.map(row => row.attributes('data-testid'))).toEqual([
      'supply-health-account-1',
      'supply-health-account-2',
    ])

    // $6000 的号不该被标记，$400 的号该被标灰并带「低于预期」。
    expect(wrapper.find('[data-testid="supply-health-account-low-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="supply-health-account-low-2"]').exists()).toBe(true)
    expect(rows[1].classes().join(' ')).toContain('text-gray-400')
  })

  it('refetches with the new window when the switcher changes', async () => {
    const wrapper = await mountView()
    getHealth.mockClear()
    getHealth.mockResolvedValue(createHealth({ window_days: 7, list_value: 2500 }))

    await wrapper.get('[data-testid="supply-health-window"]').setValue(7)
    await flushPromises()

    expect(getHealth).toHaveBeenCalledWith(7)
    expect(wrapper.get('[data-testid="supply-health-list-value"]').text()).toContain('$2,500.00')
  })

  it('keeps the self-check quiet when effective and configured agree', async () => {
    const wrapper = await mountView()

    expect(wrapper.find('[data-testid="supply-health-mismatch"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="supply-health-check-multiplier"]').classes().join(' ')).not.toContain('amber')
  })

  it('highlights the self-check when the drift is over 5%', async () => {
    // 真库上见过的形态：窗口跨越了一次调价，实际值是新旧计费的加权混合。
    getHealth.mockResolvedValue(
      createHealth({ effective_multiplier: 0.614, configured_multiplier: 0.18, effective_share: 0.73 })
    )
    const wrapper = await mountView()

    const multiplier = wrapper.get('[data-testid="supply-health-check-multiplier"]')
    expect(multiplier.classes().join(' ')).toContain('bg-amber-50')
    expect(multiplier.text()).toContain('supplyAdmin.health.selfCheck.drift')
    // 分成偏了 |0.73 − 0.85| / 0.85 = 14.1%，也过线。
    const share = wrapper.get('[data-testid="supply-health-check-share"]')
    expect(share.classes().join(' ')).toContain('bg-amber-50')
    expect(share.text()).toContain('14.1%')
    expect(wrapper.get('[data-testid="supply-health-mismatch"]').text()).toContain(
      'supplyAdmin.health.selfCheck.mismatch'
    )
  })

  it('does not compare the multiplier when no supply pool is configured', async () => {
    // configured_multiplier = 0 的意思是「没配供给池」，不是「配了 0 倍」。
    // 拿它当分母，高亮会在一个根本没开供给池的站点上长亮不灭。
    getHealth.mockResolvedValue(createHealth({ configured_multiplier: 0, effective_multiplier: 1 }))
    const wrapper = await mountView()

    const multiplier = wrapper.get('[data-testid="supply-health-check-multiplier"]')
    expect(multiplier.classes().join(' ')).not.toContain('bg-amber-50')
    expect(multiplier.text()).toContain('supplyAdmin.health.selfCheck.noPool')
    expect(wrapper.find('[data-testid="supply-health-mismatch"]').exists()).toBe(false)
  })

  it('raises the exhausted-fallback alarm even in a window with no volume', async () => {
    // 池全空时请求是被拒的，不留下任何用量——所以这条告警必须活在空状态之外。
    getHealth.mockResolvedValue(
      createHealth({
        list_value: 0,
        revenue: 0,
        supplier_payout: 0,
        gross_margin: 0,
        effective_multiplier: 0,
        effective_share: 0,
        overflow_list_value: 0,
        overflow_share: 0,
        exhausted_today: 12,
        supply_accounts: [],
        median_monthly_output: 0,
        supplier_count: 0,
      })
    )
    const wrapper = await mountView()

    expect(wrapper.get('[data-testid="supply-health-exhausted"]').text()).toContain(
      'supplyAdmin.health.exhausted.title'
    )
    expect(wrapper.get('[data-testid="supply-health-empty"]').exists()).toBe(true)
    // 空窗口里不画四格与自检：那些比率的分母是流水，画出来只会是一排 0 和一个
    // 必然越界的偏差。
    expect(wrapper.find('[data-testid="supply-health-list-value"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="supply-health-mismatch"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('NaN')
  })

  it('shows a retry instead of a blank card when the readings fail to load', async () => {
    getHealth.mockRejectedValue(new Error('503'))
    const wrapper = await mountView()

    expect(wrapper.get('[data-testid="supply-health-error"]').exists()).toBe(true)
    expect(showError).toHaveBeenCalled()
    // 读数挂了不该把下面七组配置一起挡住：它们各走各的接口。
    expect(wrapper.find('[data-testid="supply-payout-chain-card"]').exists()).toBe(true)

    getHealth.mockResolvedValue(createHealth())
    await wrapper.get('[data-testid="supply-health-retry"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-health-error"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="supply-health-list-value"]').text()).toContain('$10,000.00')
  })
})
