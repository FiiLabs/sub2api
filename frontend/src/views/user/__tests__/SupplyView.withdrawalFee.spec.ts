/**
 * APEXONE-EXT: 提现手续费在界面上的呈现——免手续费改版后只剩「事后账」。
 *
 * 表单里的手续费预告已随免手续费改版整体移除（gas 由金库承担，全额到账），
 * 这个 spec 盯的是剩下的两条性质，失守时都不报任何错：
 *
 *   1. **表单里不再出现任何手续费文案。** 一个残留的「手续费 X」读起来是
 *      一笔仍会发生的扣款——后端已经不扣了。
 *   2. **历史单列表照旧显示当年的 fee/net，且用后端给的 net_amount，不在前端
 *      重算。** 免费之前的旧单子上真扣过钱，那行「手续费 X · 到账 Y」必须
 *      还原当年的快照；amount - fee 抄进 TypeScript 就有了第二份公式，
 *      两份迟早在某次改动里分岔。
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
    bindPayoutWallet: vi.fn(),
    unbindPayoutWallet: vi.fn(),
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

// 与 payoutWallet.spec 不同：这里的 t 把插值参数也吐出来。
// 这个文件的重心恰恰是「哪个数出现在了哪句话里」，只回落 key 会让
// 「手续费 0.3」和「手续费 0」在断言里长得一模一样。
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

const BOUND = '0xde709f2102306220921060314715629080e2fb77'

function withdrawalOptions(overrides: Record<string, unknown> = {}) {
  return {
    available: true,
    enabled: true,
    min_amount: 10,
    max_pending: 3,
    channels: ['BSC-USDT', '支付宝'],
    notice: '',
    available_credit: 500,
    pending_count: 0,
    onchain_channels: [{ channel: 'BSC-USDT', network: 'bsc', token_symbol: 'USDT' }],
    ...overrides,
  }
}

/** 一张免费之前走了链上的已打款单，fee/net 都来自后端的历史快照。 */
function onchainWithdrawal(overrides: Record<string, unknown> = {}) {
  return {
    id: 11,
    amount: 100,
    status: 'paid',
    payout_channel: 'BSC-USDT',
    payout_account: BOUND,
    fee_amount: 0.3,
    net_amount: 99.7,
    network: 'bsc',
    token_symbol: 'USDT',
    created_at: '2026-08-22T00:00:00Z',
    updated_at: '2026-08-22T00:00:00Z',
    ...overrides,
  }
}

function mountSupplyView() {
  return mount(SupplyView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: true,
      },
    },
  })
}

describe('SupplyView withdrawal fee', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getStatus.mockResolvedValue({ enabled: true, settlement_enabled: true })
    api.getWallet.mockResolvedValue({ available_credit: 500, frozen_credit: 0, total_earned: 500 })
    api.listAccounts.mockResolvedValue([])
    api.listLedger.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    api.getAgreement.mockResolvedValue({ version: 'v1', published: true, accepted: true })
    api.getWithdrawalOptions.mockResolvedValue(withdrawalOptions())
    api.listWithdrawals.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    api.getPayoutWallets.mockResolvedValue({
      channels: [],
      wallets: [{ id: 1, network: 'bsc', address: BOUND, created_at: '', updated_at: '' }],
    })
  })

  it('shows no fee copy anywhere on the request form', async () => {
    // 免手续费后表单里不该残留任何「手续费」文案——残留的那一句读起来
    // 是一笔仍会发生的扣款，而后端已经不扣了。
    const wrapper = mountSupplyView()
    await flushPromises()
    await wrapper.find('[data-testid="supply-withdrawal-channel"]').setValue('BSC-USDT')
    await flushPromises()
    await wrapper.find('[data-testid="supply-withdrawal-amount"]').setValue('100')
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-withdrawal-fee"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="supply-withdrawal-net-preview"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="supply-withdrawal-fee-not-covered"]').exists()).toBe(false)
  })

  it('renders fee and net on the list from the response, not from arithmetic', async () => {
    // net_amount 刻意给一个**不等于** amount - fee_amount 的数：断言显示的是它，
    // 就证明了前端没有自己再算一遍。真实数据里两者相等，所以只有假数据能测出来。
    api.listWithdrawals.mockResolvedValue({
      items: [onchainWithdrawal({ fee_amount: 0.3, net_amount: 42.42 })],
      total: 1,
      page: 1,
      pages: 1,
    })

    const wrapper = mountSupplyView()
    await flushPromises()

    const line = wrapper.find('[data-testid="supply-withdrawal-fee-11"]')
    expect(line.exists()).toBe(true)
    expect(line.text()).toContain('0.30')
    expect(line.text()).toContain('42.42')
  })

  it('keeps fee-free rows clean — no fee line on a zero-fee withdrawal', async () => {
    // 免费改版后的新单 fee_amount 恒 0：那行「手续费 X · 到账 Y」不该出现，
    // 出现一句「手续费 0.00」等于告诉他这里本来要收钱。
    api.listWithdrawals.mockResolvedValue({
      items: [onchainWithdrawal({ id: 12, fee_amount: 0, net_amount: 100 })],
      total: 1,
      page: 1,
      pages: 1,
    })

    const wrapper = mountSupplyView()
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-withdrawal-fee-12"]').exists()).toBe(false)
  })

  it('marks on-chain rows as auto payout', async () => {
    api.listWithdrawals.mockResolvedValue({
      items: [onchainWithdrawal()],
      total: 1,
      page: 1,
      pages: 1,
    })

    const wrapper = mountSupplyView()
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-withdrawal-11"]').text()).toContain(
      'supply.withdrawal.fee.auto bsc'
    )
  })
})
