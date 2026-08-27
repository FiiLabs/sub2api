/**
 * APEXONE-EXT: /supply 的区块顺序与新手引导。
 *
 * 这个 spec 存在的唯一理由是它会**红**。
 *
 * 区块顺序和引导卡都属于"看起来无所谓"的那类设计：下一个人重构模板时顺手把提现
 * 排回第一屏、或者为了转化率把那句"早期收益可能很有限"删掉，都不会有任何东西报错。
 * 所以三条判据全部写成硬断言：
 *   1. 顺序按 DOM 里 testid 的**出现次序**断言，不是断言"它存在"——存在性对
 *      "谁在前面"这个问题一个字都没说；
 *   2. 引导卡只对零账号用户在场，两个方向都测；
 *   3. after3 那句免责必须在引导卡里。
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
  },
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/supply', () => ({ supplyAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('@/stores/supply', () => ({ useSupplyStore: () => ({ enabled: true, refresh: vi.fn() }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
// t 做真插值（`key {a} {b}`），不是原样回 key：分成比例那条断言的全部意义在于
// 「渲染出来的数字来自后端」，而一个吞掉参数的 mock 会让写死 80% 的实现照样通过。
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

/** 按 testid 在 HTML 里第一次出现的位置排序——这就是用户从上往下读到的次序。 */
function domOrder(html: string, testids: string[]): string[] {
  return testids
    .map((id) => ({ id, at: html.indexOf(`data-testid="${id}"`) }))
    .filter((item) => item.at >= 0)
    .sort((a, b) => a.at - b.at)
    .map((item) => item.id)
}

const ACCOUNT = {
  id: 1,
  name: 'a',
  platform: 'anthropic',
  supply_state: 'active',
  status: 'active',
  schedulable: true,
  created_at: '2026-01-01T00:00:00Z',
  probe_passes: 0,
}

describe('SupplyView layout order & onboarding guide', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 0 })
    api.getWallet.mockResolvedValue({ available_credit: 0, frozen_credit: 0, history_credit: 0, spent_credit: 0 })
    api.listAccounts.mockResolvedValue([])
    api.listLedger.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    api.getAgreement.mockResolvedValue({ version: 'v1', published: true, accepted: true })
    api.getWithdrawalOptions.mockResolvedValue({
      available: true,
      enabled: true,
      min_amount: 1,
      max_pending: 3,
      channels: ['bsc-usdt'],
      notice: '',
      available_credit: 0,
      pending_count: 0,
      onchain_channels: [],
    })
    api.listWithdrawals.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    api.getPayoutWallets.mockResolvedValue({ channels: [], wallets: [] })
  })

  it('puts earning before cashing out: connect and accounts come above the withdrawal card', async () => {
    // 提现对一个还没接入任何号的人是纯噪音——余额是 0、地址没绑、提交必然被拒。
    // 它排在接入之后不是审美选择，是这一页对"我该先做什么"的回答。
    api.listAccounts.mockResolvedValue([ACCOUNT])
    const wrapper = mountView()
    await flushPromises()

    expect(
      domOrder(wrapper.html(), [
        'supply-ledger-card',
        'supply-withdrawal-card',
        'supply-accounts-card',
        'supply-connect-card',
        'supply-wallet-card',
      ])
    ).toEqual([
      'supply-wallet-card',
      'supply-connect-card',
      'supply-accounts-card',
      'supply-withdrawal-card',
      'supply-ledger-card',
    ])
  })

  it('slots the guide between the wallet overview and the connect card', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(
      domOrder(wrapper.html(), ['supply-connect-card', 'supply-guide-card', 'supply-wallet-card'])
    ).toEqual(['supply-wallet-card', 'supply-guide-card', 'supply-connect-card'])
  })

  it('shows the three-step guide to a user with no supply accounts', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-guide-card"]').exists()).toBe(true)
    // 三步都在，而且编号是连着的：少一步的引导比没有引导更容易把人卡住。
    for (const n of [1, 2, 3]) {
      expect(wrapper.find(`[data-testid="supply-guide-step-${n}"]`).exists()).toBe(true)
    }
  })

  it('hides the guide once the user has connected something', async () => {
    // 已经接入过的人不需要再读一遍步骤；对他们这张卡是纯占位，
    // 只会把"我的号在不在接单"往下推一屏。
    api.listAccounts.mockResolvedValue([ACCOUNT])
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-guide-card"]').exists()).toBe(false)
  })

  it('always carries the "earnings may be limited" line inside the guide', async () => {
    // 这条是防转化率优化的：把它删掉的那次改动，必须先让这条测试变红。
    const wrapper = mountView()
    await flushPromises()

    const guide = wrapper.get('[data-testid="supply-guide-card"]')
    const disclaimer = guide.find('[data-testid="supply-guide-disclaimer"]')
    expect(disclaimer.exists()).toBe(true)
    expect(disclaimer.text()).toBe('supply.guide.after3')
  })

  /**
   * 引导卡里那句 after1 写的是「比例见下方接入卡」。这组测试守的是那句话别再变回
   * 一句空话——它曾经是：接入卡上从来没有比例，比例只出现在流水表格里，
   * 而没挂过号的人根本没有流水。新用户照着指路看过去，什么都找不到。
   */
  describe('the revenue share the guide points at', () => {
    it('renders the ratio the backend reported, not a hardcoded one', async () => {
      // 后端给 0.8 就显示 80%。这个数是运营配置，前端写死它的那一刻，
      // 运营改配置就等于让界面开始对供给者报一个错的分成。
      api.getStatus.mockResolvedValue({
        enabled: true,
        settlement_enabled: true,
        account_count: 0,
        share_ratio: 0.8,
      })
      const wrapper = mountView()
      await flushPromises()

      expect(wrapper.get('[data-testid="supply-connect-share-ratio"]').text()).toContain('80%')
    })

    it('tracks the setting instead of pinning one number', async () => {
      // 同一份代码在比例被改成 0.65 的部署上必须显示 65%。这条与上一条成对存在：
      // 单独一条 80% 的断言，写死 80% 的实现照样能过。
      api.getStatus.mockResolvedValue({
        enabled: true,
        settlement_enabled: true,
        account_count: 0,
        share_ratio: 0.65,
      })
      const wrapper = mountView()
      await flushPromises()

      const text = wrapper.get('[data-testid="supply-connect-share-ratio"]').text()
      expect(text).toContain('65%')
      expect(text).not.toContain('80%')
    })

    it('says nothing at all when the backend has no ratio to give', async () => {
      // 说不出比例，比说一个 0% 好——后者读起来像"你一分钱都拿不到"，
      // 而真相只是这台部署的后端没读到设置。
      const wrapper = mountView()
      await flushPromises()

      expect(wrapper.find('[data-testid="supply-connect-share-ratio"]').exists()).toBe(false)
    })

    it('refuses ratios that cannot be true', async () => {
      // >1 意味着平台倒贴。这种值只可能来自脏数据或误配，把它照实显示出来
      // （"你拿 150%"）会被当成承诺，而那是一句我们兑现不了的话。
      api.getStatus.mockResolvedValue({
        enabled: true,
        settlement_enabled: true,
        account_count: 0,
        share_ratio: 1.5,
      })
      const wrapper = mountView()
      await flushPromises()

      expect(wrapper.find('[data-testid="supply-connect-share-ratio"]').exists()).toBe(false)
    })

    it('keeps the ratio visible to someone still sitting at the agreement gate', async () => {
      // 还没同意协议的人，正是最需要这个数的人——他要靠它决定要不要同意。
      // 把比例藏在协议门禁之后，等于要求他先签字再看价钱。
      api.getStatus.mockResolvedValue({
        enabled: true,
        settlement_enabled: true,
        account_count: 0,
        share_ratio: 0.8,
      })
      api.getAgreement.mockResolvedValue({ version: 'v1', published: true, accepted: false })
      const wrapper = mountView()
      await flushPromises()

      expect(wrapper.find('[data-testid="supply-agreement-gate"]').exists()).toBe(true)
      expect(wrapper.get('[data-testid="supply-connect-share-ratio"]').text()).toContain('80%')
    })
  })

  it('links out to the full docs instead of inlining a wall of text', async () => {
    const wrapper = mountView()
    await flushPromises()

    const link = wrapper.get('[data-testid="supply-guide-docs"]')
    // 外链一律 noopener：目标页拿到 window.opener 就能改写本页地址。
    expect(link.attributes('rel')).toContain('noopener')
    expect(link.attributes('target')).toBe('_blank')
  })
})
