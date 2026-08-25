/**
 * APEXONE-EXT: 中转接入（M7）的界面分岔。
 *
 * 这个 spec 盯两条：开关关着时**整个中转入口不存在**（一个点了每步都失败的
 * 表单比没有表单糟糕得多），开着时提交发的是 trim 过的三样东西、成功后
 * key 立即从输入框清掉——凭证不多留一秒。
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
    submitRelayAccount: vi.fn(),
    requestWithdrawal: vi.fn(),
    cancelWithdrawal: vi.fn(),
  },
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/supply', () => ({ supplyAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('@/stores/supply', () => ({
  useSupplyStore: () => ({ enabled: true, refresh: vi.fn() }),
}))
vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn() }),
}))
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function mountView() {
  return mount(SupplyView, {
    global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true } },
  })
}

describe('SupplyView relay onboarding', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, relay_enabled: true })
    api.getWallet.mockResolvedValue({ available_credit: 0, frozen_credit: 0 })
    api.listAccounts.mockResolvedValue([])
    api.listLedger.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    api.getAgreement.mockResolvedValue({ version: 'v1', published: true, accepted: true })
    api.getWithdrawalOptions.mockResolvedValue({
      available: false, enabled: false, min_amount: 0, max_pending: 1,
      channels: [], notice: '', available_credit: 0, pending_count: 0,
    })
    api.listWithdrawals.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    api.getPayoutWallets.mockResolvedValue({ channels: [], wallets: [] })
  })

  it('hides the whole relay section when the admin switch is off', async () => {
    api.getStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, relay_enabled: false })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="supply-relay-section"]').exists()).toBe(false)
  })

  it('submits trimmed fields and clears the key on success', async () => {
    api.submitRelayAccount.mockResolvedValue({ id: 1 })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-relay-section"]').exists()).toBe(true)
    await wrapper.find('[data-testid="supply-relay-base-url"]').setValue('  https://relay.example.com  ')
    await wrapper.find('[data-testid="supply-relay-api-key"]').setValue('  sk-abc  ')
    await wrapper.find('[data-testid="supply-relay-submit"]').trigger('click')
    await flushPromises()

    expect(api.submitRelayAccount).toHaveBeenCalledWith({
      base_url: 'https://relay.example.com',
      api_key: 'sk-abc',
      name: undefined,
    })
    // 成功后 key 立即清空——凭证不多留一秒。
    const keyInput = wrapper.find('[data-testid="supply-relay-api-key"]').element as HTMLInputElement
    expect(keyInput.value).toBe('')
    expect(api.listAccounts).toHaveBeenCalledTimes(2)
  })

  it('refuses to submit with empty fields, without calling the API', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.find('[data-testid="supply-relay-submit"]').trigger('click')
    await flushPromises()

    expect(api.submitRelayAccount).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('supply.relay.fieldsRequired')
  })
})
