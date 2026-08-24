/**
 * APEXONE-EXT: 提现手续费在界面上的两次亮相——表单里的预告，和单据列表里的事后账。
 *
 * 这个 spec 盯的两条性质，失守时都不报任何错：
 *
 *   1. **拿不到报价绝不显示 0。** onchain_fees 是「此刻真能自动结算的渠道」的
 *      差集式子集：一个渠道可以在 onchain_channels 里、却不在 onchain_fees 里
 *      （没接客户端 / 金库没配 / 估不出）。那种渠道该说的是「转人工打款」，
 *      而一个 0 元手续费读起来是一个承诺——后端没做过这个承诺。
 *
 *   2. **到账金额用后端给的 net_amount，不在前端重算。** amount - fee 是一条
 *      关于钱的公式，抄进 TypeScript 就有了第二份，两份迟早在某次改动里分岔。
 *      列表上那行「手续费 X · 到账 Y」必须来自响应里的字段。
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
    onchain_fees: [{ channel: 'BSC-USDT', fee: 0.3, estimated: true }],
    ...overrides,
  }
}

/** 一张走了链上的已打款单，fee/net 都来自后端。 */
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

async function mountWithChannel(channel: string) {
  const wrapper = mountSupplyView()
  await flushPromises()
  await wrapper.find('[data-testid="supply-withdrawal-channel"]').setValue(channel)
  await flushPromises()
  return wrapper
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

  it('quotes the fee for a settleable on-chain channel', async () => {
    const wrapper = await mountWithChannel('BSC-USDT')

    const fee = wrapper.find('[data-testid="supply-withdrawal-fee"]')
    expect(fee.exists()).toBe(true)
    expect(fee.text()).toContain('supply.withdrawal.fee.quote')
    expect(fee.text()).toContain('0.30')
  })

  it('previews the net using amount minus the quoted fee', async () => {
    const wrapper = await mountWithChannel('BSC-USDT')
    await wrapper.find('[data-testid="supply-withdrawal-amount"]').setValue('100')
    await flushPromises()

    const preview = wrapper.find('[data-testid="supply-withdrawal-net-preview"]')
    expect(preview.exists()).toBe(true)
    expect(preview.text()).toContain('99.70')
  })

  it('warns instead of previewing when the fee eats the whole amount', async () => {
    // 与后端建单时的 fee >= amount 拒绝是同一条线，提前画在表单上——
    // 等提交了才打回来，他已经填完了所有东西。
    const wrapper = await mountWithChannel('BSC-USDT')
    await wrapper.find('[data-testid="supply-withdrawal-amount"]').setValue('0.3')
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-withdrawal-net-preview"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="supply-withdrawal-fee-not-covered"]').exists()).toBe(true)
  })

  it('stays quiet about coverage while the amount field is still empty', async () => {
    // 表单还空着就先警告「金额盖不住手续费」，读起来像已经做错了什么。
    const wrapper = await mountWithChannel('BSC-USDT')

    expect(wrapper.find('[data-testid="supply-withdrawal-fee-not-covered"]').exists()).toBe(false)
  })

  it('says "manual payout" — never a zero fee — when the channel has no quote', async () => {
    // onchain_channels 有它、onchain_fees 没它：会上链但此刻结算不了。
    api.getWithdrawalOptions.mockResolvedValue(withdrawalOptions({ onchain_fees: [] }))

    const wrapper = await mountWithChannel('BSC-USDT')

    const fee = wrapper.find('[data-testid="supply-withdrawal-fee"]')
    expect(fee.exists()).toBe(true)
    expect(wrapper.find('[data-testid="supply-withdrawal-fee-manual"]').exists()).toBe(true)
    expect(fee.text()).not.toContain('supply.withdrawal.fee.quote')
  })

  it('shows no fee block at all on a manual channel', async () => {
    const wrapper = await mountWithChannel('支付宝')

    expect(wrapper.find('[data-testid="supply-withdrawal-fee"]').exists()).toBe(false)
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

  it('keeps manual rows clean — no fee line, no auto-payout badge', async () => {
    api.listWithdrawals.mockResolvedValue({
      items: [
        onchainWithdrawal({
          id: 12,
          payout_channel: '支付宝',
          payout_account: 'alipay@example.com',
          fee_amount: 0,
          net_amount: 100,
          network: undefined,
          token_symbol: undefined,
        }),
      ],
      total: 1,
      page: 1,
      pages: 1,
    })

    const wrapper = mountSupplyView()
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-withdrawal-fee-12"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="supply-withdrawal-12"]').text()).not.toContain(
      'supply.withdrawal.fee.auto'
    )
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
