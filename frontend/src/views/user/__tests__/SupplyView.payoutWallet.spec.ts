/**
 * APEXONE-EXT: 提现表单在「链上渠道」与「人工渠道」之间的分岔。
 *
 * 这个 spec 只盯一件事：选中的渠道是链上渠道时，收款账号那一栏**不能**是一个
 * 自由输入框。
 *
 * 它值得单独成文，是因为这条性质失守时看起来完全正常——输入框在那儿、能填、
 * 能提交、后端返回 200，单子建出来了。区别只在那串字符没有经过任何校验就成了
 * 一个链上收款地址，而链上转账不可逆：钱会成功转到一个谁也不认识的地址里，
 * 区块浏览器上写着「已转账」，没有任何下游能把它兜回来。
 *
 * 后端在建单时会用 ResolvePayoutAddress 覆盖掉手填的账号，所以这不是唯一一道门。
 * 但那道门在**建单**这一刻才生效，而这道门在**画表单**这一刻就生效——
 * 两道门挡的是不同的东西：后端挡住"钱打错地方"，前端挡住"让他以为该手填"。
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

// 文案一律回落成 key 本身：这个文件的断言全都在结构上（哪个控件在场、
// 请求发了什么），钉住具体中文只会让改一句提示语就红一片。
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
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

/** 挂载、等异步加载跑完，再把渠道选成 channel。 */
async function mountWithChannel(channel: string) {
  const wrapper = mountSupplyView()
  await flushPromises()
  await wrapper.find('[data-testid="supply-withdrawal-channel"]').setValue(channel)
  await flushPromises()
  return wrapper
}

describe('SupplyView payout wallet', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getStatus.mockResolvedValue({ enabled: true, settlement_enabled: true })
    api.getWallet.mockResolvedValue({ available_credit: 500, frozen_credit: 0, total_earned: 500 })
    api.listAccounts.mockResolvedValue([])
    api.listLedger.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    api.getAgreement.mockResolvedValue({ version: 'v1', published: true, accepted: true })
    api.getWithdrawalOptions.mockResolvedValue(withdrawalOptions())
    api.listWithdrawals.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    api.getPayoutWallets.mockResolvedValue({ channels: [], wallets: [] })
  })

  it('replaces the free-text account field with the binding block on an on-chain channel', async () => {
    const wrapper = await mountWithChannel('BSC-USDT')

    expect(wrapper.find('[data-testid="supply-payout-wallet"]').exists()).toBe(true)
    // 这一条是整个文件的重心：链上渠道下**不存在**一个能手填收款账号的输入框。
    expect(wrapper.find('[data-testid="supply-withdrawal-account"]').exists()).toBe(false)
  })

  it('has no free-text account field anywhere on the form (M6b)', async () => {
    // 人工渠道已整体下线：这张表单上不存在任何一个能手填收款账号的输入框。
    // 后端的渠道列表只会给出链上渠道，但即便 API 塞回一个别的字符串，
    // 表单也只能画出"没有绑定块"，绝不能画出一个自由输入框。
    const wrapper = await mountWithChannel('支付宝')

    expect(wrapper.find('[data-testid="supply-withdrawal-account"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="supply-payout-wallet"]').exists()).toBe(false)
  })

  it('does not ask for a binding before a channel is chosen', async () => {
    // 还没选渠道时催人绑地址，是在替他做一个他还没做的决定。
    const wrapper = mountSupplyView()
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-payout-wallet"]').exists()).toBe(false)
  })

  it('never asks for bindings on a deployment without on-chain channels', async () => {
    // 没有链上渠道的部署一次也不该打这个接口——一个没装配起来的绑定服务
    // 会让每次进页面都弹一条与他无关的错误。
    api.getWithdrawalOptions.mockResolvedValue(withdrawalOptions({ onchain_channels: [] }))

    mountSupplyView()
    await flushPromises()

    expect(api.getPayoutWallets).not.toHaveBeenCalled()
  })

  it('shows the bound address read-only, with no input to fat-finger', async () => {
    api.getPayoutWallets.mockResolvedValue({
      channels: [],
      wallets: [{ id: 1, network: 'bsc', address: BOUND, created_at: '', updated_at: '' }],
    })

    const wrapper = await mountWithChannel('BSC-USDT')

    expect(wrapper.find('[data-testid="supply-payout-wallet-address"]').text()).toBe(BOUND)
    // 已绑定时输入框收起来。地址是看一眼就够的东西；让它一直摊在可编辑的
    // 输入框里只是多一次误改机会，而误改在这里不可逆。
    expect(wrapper.find('[data-testid="supply-payout-wallet-input"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="supply-payout-wallet-empty"]').exists()).toBe(false)
  })

  it('says out loud that nothing is bound yet', async () => {
    const wrapper = await mountWithChannel('BSC-USDT')

    expect(wrapper.find('[data-testid="supply-payout-wallet-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="supply-payout-wallet-input"]').exists()).toBe(true)
  })

  it('sends the address exactly as typed — the checksum is the backend\'s to judge', async () => {
    // 前端 toLowerCase 一下，EIP-55 的校验和信息就在到达后端之前消失了，
    // 而那是唯一能发现"你改过其中一位"的信号。
    const mixed = '0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed'
    api.bindPayoutWallet.mockResolvedValue({
      id: 1,
      network: 'bsc',
      address: mixed.toLowerCase(),
      created_at: '',
      updated_at: '',
    })

    const wrapper = await mountWithChannel('BSC-USDT')
    await wrapper.find('[data-testid="supply-payout-wallet-input"]').setValue(`  ${mixed}  `)
    await wrapper.find('[data-testid="supply-payout-wallet-bind"]').trigger('click')
    await flushPromises()

    expect(api.bindPayoutWallet).toHaveBeenCalledWith('bsc', mixed)
    // 显示的是**后端存的那一个**，不是他手里这串。两者不一致时，他会以为自己
    // 绑的是屏幕上看到的形态，下次跟交易记录对不上。
    expect(wrapper.find('[data-testid="supply-payout-wallet-address"]').text()).toBe(mixed.toLowerCase())
  })

  it('withdraws to the bound address, not to anything typed into the form', async () => {
    api.getPayoutWallets.mockResolvedValue({
      channels: [],
      wallets: [{ id: 1, network: 'bsc', address: BOUND, created_at: '', updated_at: '' }],
    })
    api.requestWithdrawal.mockResolvedValue({ id: 1 })

    const wrapper = await mountWithChannel('BSC-USDT')
    await wrapper.find('[data-testid="supply-withdrawal-amount"]').setValue('50')
    await wrapper.find('[data-testid="supply-withdrawal-submit"]').trigger('click')
    await flushPromises()

    expect(api.requestWithdrawal).toHaveBeenCalledWith({
      amount: 50,
      payout_channel: 'BSC-USDT',
      payout_account: BOUND,
      user_note: undefined,
    })
  })

  // 金额那一栏的运行时类型是 string | number：<input type="number"> 上的 v-model
  // 会把填进去的东西转成 number，清空时才留下空串。两半都要证，因为处理这件事的
  // 那行代码曾经只对其中一半成立——而不成立的那一半是"填了金额"，也就是
  // **每一次真实提交**都在 .trim() 上抛 TypeError，按钮看起来毫无反应。
  it('survives the number that v-model actually writes into the amount field', async () => {
    api.requestWithdrawal.mockResolvedValue({ id: 1 })
    api.getPayoutWallets.mockResolvedValue({
      channels: [],
      wallets: [{ id: 1, network: 'bsc', address: BOUND, created_at: '', updated_at: '' }],
    })

    const wrapper = await mountWithChannel('BSC-USDT')
    await wrapper.find('[data-testid="supply-withdrawal-amount"]').setValue('50')
    await wrapper.find('[data-testid="supply-withdrawal-submit"]').trigger('click')
    await flushPromises()

    expect(showError).not.toHaveBeenCalled()
    expect(api.requestWithdrawal).toHaveBeenCalledWith(
      expect.objectContaining({ amount: 50, payout_account: BOUND })
    )
  })

  it('still calls an empty amount empty', async () => {
    const wrapper = await mountWithChannel('支付宝')
    await wrapper.find('[data-testid="supply-withdrawal-submit"]').trigger('click')
    await flushPromises()

    expect(api.requestWithdrawal).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('supply.error.withdrawalAmountInvalid')
  })

  it('refuses to submit an on-chain withdrawal with nothing bound', async () => {
    const wrapper = await mountWithChannel('BSC-USDT')
    await wrapper.find('[data-testid="supply-withdrawal-amount"]').setValue('50')
    await wrapper.find('[data-testid="supply-withdrawal-submit"]').trigger('click')
    await flushPromises()

    // 建单必须在前端就停住。让它发出去，后端会拒（ResolvePayoutAddress
    // 对链上渠道是 fail-closed），但那条错误说的是"没绑地址"——他会去找
    // 一个刚刚才被这个表单藏起来的输入框。
    expect(api.requestWithdrawal).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('supply.error.payoutWalletRequired')
  })
})
