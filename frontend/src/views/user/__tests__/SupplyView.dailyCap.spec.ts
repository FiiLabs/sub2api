/**
 * APEXONE-EXT: /supply 账号表的每日共享上限。
 *
 * 这是这个视图**第一个**覆盖账号表的 spec（此前只有版面顺序、中转、提现三块有测试）。
 *
 * 四条里最要紧的是第二条：闸门是调度层过滤、不写库，所以触顶时 account.schedulable
 * **仍然是 true**。界面若只看它，就会在一个一分钱赚不到的号上显示「接单中」——
 * 那正是这个功能存在的意义所在，也是它最容易被一次「顺手简化模板」改回去的地方。
 *
 * 第四条守的是别的 spec：另外四个 spec 的 fixture 里的账号对象**没有**这些新字段，
 * 模板里任何一个没做 `??` 兜底的表达式都会让它们一起变红。
 */
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import SupplyView from '../SupplyView.vue'

const { api, showError, showSuccess } = vi.hoisted(() => ({
  api: {
    getStatus: vi.fn(),
    getWallet: vi.fn(),
    listAccounts: vi.fn(),
    listLedger: vi.fn(),
    getAgreement: vi.fn(),
    getWithdrawalOptions: vi.fn(),
    listWithdrawals: vi.fn(),
    getPayoutWallets: vi.fn(),
    setDailyCap: vi.fn(),
  },
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/supply', () => ({ supplyAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('@/stores/supply', () => ({ useSupplyStore: () => ({ enabled: true, refresh: vi.fn() }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
// t 做真插值：不然一个把数字写死在模板里的实现也能通过。
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key} ${Object.values(params).join(' ')}` : key,
    }),
  }
})

function mountView() {
  return mount(SupplyView, {
    global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true } },
  })
}

const BASE_ACCOUNT = {
  id: 1,
  name: 'a',
  platform: 'anthropic',
  supply_state: 'active',
  status: 'active',
  schedulable: true,
  created_at: '2026-01-01T00:00:00Z',
  probe_passes: 0,
}

describe('SupplyView daily share cap', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 1 })
    api.getWallet.mockResolvedValue({ available_credit: 0, frozen_credit: 0, history_credit: 0, spent_credit: 0 })
    api.listAccounts.mockResolvedValue([BASE_ACCOUNT])
    api.listLedger.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    api.getAgreement.mockResolvedValue({ version: 'v1', published: true, accepted: true })
    api.getWithdrawalOptions.mockResolvedValue({
      available: true, enabled: true, min_amount: 1, max_pending: 3,
      channels: ['bsc-usdt'], notice: '', available_credit: 0, pending_count: 0, onchain_channels: [],
    })
    api.listWithdrawals.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    api.getPayoutWallets.mockResolvedValue({ channels: [], wallets: [] })
  })

  it('没设上限时显示「不限」，不显示触顶提示', async () => {
    const wrapper = mountView()
    await flushPromises()

    const cell = wrapper.get('[data-testid="supply-daily-cap-cell-1"]')
    expect(cell.text()).toContain('supply.accounts.dailyCapUnlimited')
    expect(wrapper.find('[data-testid="supply-daily-cap-reached-1"]').exists()).toBe(false)
  })

  it('设了上限时显示「已用 / 上限」，并标明是官方牌价口径', async () => {
    api.listAccounts.mockResolvedValue([
      { ...BASE_ACCOUNT, daily_cost_limit_usd: 20, daily_cost_used_usd: 3.5, daily_token_limit: 0 },
    ])
    const wrapper = mountView()
    await flushPromises()

    const cell = wrapper.get('[data-testid="supply-daily-cap-cell-1"]')
    expect(cell.text()).toContain('$3.50')
    expect(cell.text()).toContain('$20.00')
    // 这句是防投诉的：分母是官方牌价，不是他的收益（两者差一个倍率）。
    expect(cell.text()).toContain('supply.accounts.dailyCapBasisHint')
  })

  it('触顶时必须说明原因，且**不能**仍显示「接单中」', async () => {
    // schedulable 仍是 true —— 这正是线上的真实形态：闸门不写库。
    api.listAccounts.mockResolvedValue([
      { ...BASE_ACCOUNT, schedulable: true, daily_cap_reached: true, daily_cost_limit_usd: 20, daily_cost_used_usd: 20 },
    ])
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-daily-cap-reached-1"]').exists()).toBe(true)
    const row = wrapper.get('[data-testid="supply-account-1"]')
    expect(row.text()).not.toContain('supply.accounts.schedulable')
  })

  it('保存时发出解析后的数字，并从响应回填', async () => {
    api.setDailyCap.mockResolvedValue({
      ...BASE_ACCOUNT, daily_cost_limit_usd: 12.34, daily_cost_used_usd: 0, daily_token_limit: 5000,
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="supply-daily-cap-edit-1"]').trigger('click')
    await wrapper.get('[data-testid="supply-daily-cap-cost-1"]').setValue('12.34')
    await wrapper.get('[data-testid="supply-daily-cap-tokens-1"]').setValue('5000')
    await wrapper.get('[data-testid="supply-daily-cap-save-1"]').trigger('click')
    await flushPromises()

    expect(api.setDailyCap).toHaveBeenCalledWith(1, {
      daily_cost_limit_usd: 12.34,
      daily_token_limit: 5000,
    })
    // 渲染的是响应里的值，不是输入框里的值：服务端会夹区间、截到分。
    expect(wrapper.get('[data-testid="supply-daily-cap-cell-1"]').text()).toContain('$12.34')
  })

  it('留空 = 0（取消上限），不是「不改」', async () => {
    api.listAccounts.mockResolvedValue([{ ...BASE_ACCOUNT, daily_cost_limit_usd: 20 }])
    api.setDailyCap.mockResolvedValue({ ...BASE_ACCOUNT, daily_cost_limit_usd: 0 })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="supply-daily-cap-edit-1"]').trigger('click')
    await wrapper.get('[data-testid="supply-daily-cap-cost-1"]').setValue('')
    await wrapper.get('[data-testid="supply-daily-cap-save-1"]').trigger('click')
    await flushPromises()

    expect(api.setDailyCap).toHaveBeenCalledWith(1, expect.objectContaining({ daily_cost_limit_usd: 0 }))
  })

  it('非法输入不发请求，只报错', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="supply-daily-cap-edit-1"]').trigger('click')
    await wrapper.get('[data-testid="supply-daily-cap-cost-1"]').setValue('-5')
    await wrapper.get('[data-testid="supply-daily-cap-save-1"]').trigger('click')
    await flushPromises()

    expect(api.setDailyCap).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('supply.error.dailyCapInvalid')
  })

  // 这一条守的是**另外四个 spec**：它们的 fixture 里没有这些新字段，
  // 模板里漏一个 `??` 兜底就会让它们一起变红。
  it('账号对象完全没有新字段时也能正常渲染', async () => {
    api.listAccounts.mockResolvedValue([BASE_ACCOUNT])
    expect(() => mountView()).not.toThrow()
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="supply-account-1"]').exists()).toBe(true)
  })

  // 每个动作按钮都要有悬停提示。
  //
  // 这条的由来是一个真实的供给者提问：「take offline 和 take offline now 有什么
  // 区别？」——说明光看按钮看不出差别。而这两者的代价不对称：选错不可逆的那个
  // 要重走 15–30 分钟观察期。提示被顺手删掉不会有任何报错，所以钉在这里。
  it('每个动作按钮都带悬停提示', async () => {
    api.listAccounts.mockResolvedValue([BASE_ACCOUNT])
    const wrapper = mountView()
    await flushPromises()

    const expected: Array<[string, string]> = [
      ['supply-pause-1', 'supply.accounts.pauseTitle'],
      ['supply-pause-immediate-1', 'supply.accounts.pauseNowTitle'],
      ['supply-detach-1', 'supply.accounts.detachTitle'],
    ]
    for (const [testid, key] of expected) {
      expect(wrapper.get(`[data-testid="${testid}"]`).attributes('title')).toBe(key)
    }
  })
})
