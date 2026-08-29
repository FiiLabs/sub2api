/**
 * APEXONE-EXT: /supply 账号表的「就地重新授权」。
 *
 * 这一组守三件事，重要性递减：
 *
 *  1. **徽章与按钮只看 needs_reauth**。判据在服务端，前端拿 status / probe_error
 *     自己拼的那一天，界面会对着一个坏号说「一切正常」。
 *  2. **两条授权流程互不干扰**。重新授权用的是一组独立的 ref，不是接入卡那份
 *     pendingAuth——共用的后果是点开某一行的重新授权会把顶部接入卡切成第二步，
 *     而那张卡上可见的提交按钮打的是**建号**接口。这一条只有测试守得住：
 *     它不会报错，只会让人建出一个多余的号。
 *  3. **失败时展开行不收**。授权码贴错是最常见的一种失败，收起来他得从第一步重来。
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
    startReauth: vi.fn(),
    completeReauth: vi.fn(),
  },
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/supply', () => ({ supplyAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('@/stores/supply', () => ({ useSupplyStore: () => ({ enabled: true, refresh: vi.fn() }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
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

const BROKEN_ACCOUNT = {
  id: 1,
  name: 'broken',
  platform: 'anthropic',
  supply_state: 'active',
  status: 'error',
  schedulable: false,
  created_at: '2026-01-01T00:00:00Z',
  probe_passes: 0,
  email_address: 'supplier@example.com',
  needs_reauth: true,
  error_message: 'API returned 401: {"raw":"upstream body with secrets"}',
}

const HEALTHY_ACCOUNT = {
  id: 2,
  name: 'healthy',
  platform: 'anthropic',
  supply_state: 'active',
  status: 'active',
  schedulable: true,
  created_at: '2026-01-01T00:00:00Z',
  probe_passes: 2,
}

describe('SupplyView re-authorization', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 2 })
    api.getWallet.mockResolvedValue({ available_credit: 0, frozen_credit: 0, history_credit: 0, spent_credit: 0 })
    api.listAccounts.mockResolvedValue([BROKEN_ACCOUNT, HEALTHY_ACCOUNT])
    api.listLedger.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    api.getAgreement.mockResolvedValue({ version: 'v1', published: true, accepted: true })
    api.getWithdrawalOptions.mockResolvedValue({
      available: true, enabled: true, min_amount: 1, max_pending: 3,
      channels: ['bsc-usdt'], notice: '', available_credit: 0, pending_count: 0, onchain_channels: [],
    })
    api.listWithdrawals.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    api.getPayoutWallets.mockResolvedValue({ channels: [], wallets: [] })
    api.startReauth.mockResolvedValue({ auth_url: 'https://claude.ai/oauth/authorize?x=1', session_id: 'sess-1' })
  })

  it('徽章与按钮只随 needs_reauth 出现', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-needs-reauth-1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="supply-reauth-1"]').exists()).toBe(true)

    // 健康的号一个都不该有。缺省（字段不存在）与 false 走同一条路。
    expect(wrapper.find('[data-testid="supply-needs-reauth-2"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="supply-reauth-2"]').exists()).toBe(false)
  })

  it('需要重新授权时，账号状态栏给的是「该做什么」而不是原始上游报错', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-reauth-status-hint-1"]').exists()).toBe(true)
    // 原始报错可能带 token 片段、内部主机名或整个 JSON 响应体——不该出现在页面上。
    expect(wrapper.html()).not.toContain('upstream body with secrets')
  })

  it('点开只展开那一行，且不动顶部的接入卡', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="supply-reauth-1"]').trigger('click')
    await flushPromises()

    expect(api.startReauth).toHaveBeenCalledWith(1)
    expect(wrapper.find('[data-testid="supply-reauth-editor-1"]').exists()).toBe(true)
    // 另一行不受影响。
    expect(wrapper.find('[data-testid="supply-reauth-editor-2"]').exists()).toBe(false)

    // 接入卡纹丝不动：开始按钮还在，接入用的那个 code 输入框仍然不在。
    // 这两条一起证明重新授权没有借用 pendingAuth。
    expect(wrapper.find('[data-testid="supply-start-oauth"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="supply-auth-code"]').exists()).toBe(false)
  })

  it('发起失败时收回展开行并报错', async () => {
    api.startReauth.mockRejectedValue(new Error('boom'))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="supply-reauth-1"]').trigger('click')
    await flushPromises()

    // 一个拿不到授权链接的空两步流程，比没有更让人困惑。
    expect(wrapper.find('[data-testid="supply-reauth-editor-1"]').exists()).toBe(false)
    expect(showError).toHaveBeenCalled()
  })

  it('提交后按响应替换那一行，徽章随之消失', async () => {
    api.completeReauth.mockResolvedValue({
      ...BROKEN_ACCOUNT,
      status: 'active',
      schedulable: true,
      needs_reauth: false,
      error_message: undefined,
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="supply-reauth-1"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="supply-reauth-code-1"]').setValue('auth-code')
    await wrapper.get('[data-testid="supply-complete-reauth-1"]').trigger('click')
    await flushPromises()

    expect(api.completeReauth).toHaveBeenCalledWith(1, { session_id: 'sess-1', code: 'auth-code' })
    // 用响应替换，而不是重新拉一次列表——同 pauseAccount 的既有做法。
    expect(api.listAccounts).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="supply-needs-reauth-1"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="supply-reauth-editor-1"]').exists()).toBe(false)
    expect(showSuccess).toHaveBeenCalledWith('supply.reauth.success')
  })

  it('回到观察期时用另一句提示，不让人以为马上就能接单', async () => {
    api.completeReauth.mockResolvedValue({
      ...BROKEN_ACCOUNT,
      supply_state: 'pending_review',
      status: 'active',
      needs_reauth: false,
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="supply-reauth-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="supply-reauth-code-1"]').setValue('auth-code')
    await wrapper.get('[data-testid="supply-complete-reauth-1"]').trigger('click')
    await flushPromises()

    expect(showSuccess).toHaveBeenCalledWith('supply.reauth.successPending')
  })

  it('提交失败保留展开行——授权码贴错不该让人从第一步重来', async () => {
    api.completeReauth.mockRejectedValue(new Error('bad code'))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="supply-reauth-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="supply-reauth-code-1"]').setValue('wrong')
    await wrapper.get('[data-testid="supply-complete-reauth-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="supply-reauth-editor-1"]').exists()).toBe(true)
    expect(showError).toHaveBeenCalled()
  })

  it('身份不符时说明白用哪个邮箱，而不是一句「失败了」', async () => {
    // 泛化文案会把人直接推回解绑重挂——正是这条路径要消灭的动作。
    api.completeReauth.mockRejectedValue({
      response: { data: { code: 'SUPPLIER_REAUTH_IDENTITY_MISMATCH' } },
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="supply-reauth-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="supply-reauth-code-1"]').setValue('other-account-code')
    await wrapper.get('[data-testid="supply-complete-reauth-1"]').trigger('click')
    await flushPromises()

    const message = showError.mock.calls.at(-1)?.[0] as string
    expect(message).toContain('supplier@example.com')
  })

  it('空授权码不发请求', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="supply-reauth-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="supply-complete-reauth-1"]').trigger('click')
    await flushPromises()

    expect(api.completeReauth).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('supply.error.codeRequired')
  })

  it('已入池的号坏掉时，探测失败原因照样显示', async () => {
    // 它此前挂在 pending_review 那个 template 里，于是一个入了池后来坏掉的号
    // 在这一栏什么都不显示——而那恰恰是最需要被看见的状态。
    api.listAccounts.mockResolvedValue([
      { ...BROKEN_ACCOUNT, probe_error: 'API returned 401: token expired' },
    ])
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.html()).toContain('supply.accounts.probeError')
  })
})
