import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

const apiMocks = vi.hoisted(() => ({
  updateBalance: vi.fn(),
}))

vi.mock('@/api/admin/teams', () => ({
  adminTeamsAPI: {
    updateBalance: apiMocks.updateBalance,
  },
}))

const appStoreMocks = vi.hoisted(() => ({
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStoreMocks,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/components/common/BaseDialog.vue', () => ({
  default: {
    name: 'BaseDialog',
    props: ['show', 'title', 'width'],
    template: '<div v-if="show"><slot /><slot name="footer" /></div>',
  },
}))

import TeamBalanceModal from '../TeamBalanceModal.vue'
import type { AdminTeam } from '@/types'

function makeTeam(overrides: Partial<AdminTeam> = {}): AdminTeam {
  return {
    id: 42,
    name: 'Test Team',
    slug: 'test-team',
    status: 'active',
    owner_user_id: 1,
    owner: null,
    billing_subject_id: 10,
    balance: 50.00,
    member_count: 2,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    concurrency: 0,
    rpm_limit: 0,
    ...overrides,
  }
}

async function mountAndOpen(operation: 'add' | 'subtract' = 'add', teamOverrides: Partial<AdminTeam> = {}) {
  const w = mount(TeamBalanceModal, {
    props: { show: false, team: makeTeam(teamOverrides), operation },
  })
  await w.setProps({ show: true })
  await flushPromises()
  return w
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  apiMocks.updateBalance.mockResolvedValue({ id: 42, balance: 100 })
})

describe('TeamBalanceModal', () => {
  it('show=true 时渲染团队名称和余额', async () => {
    const w = await mountAndOpen('add')
    expect(w.html()).toContain('Test Team')
    expect(w.html()).toContain('50.00')
  })

  it('充值：填入金额并提交调用 updateBalance(id, amount, operation, notes) 并 emit success', async () => {
    const w = await mountAndOpen('add')

    const input = w.find('input[type="number"]')
    await input.setValue('25')

    // 找提交按钮
    const form = w.find('form')
    await form.trigger('submit')
    await flushPromises()

    expect(apiMocks.updateBalance).toHaveBeenCalledWith(42, 25, 'add', '')
    expect(w.emitted('success')).toHaveLength(1)
    expect(w.emitted('close')).toHaveLength(1)
    expect(appStoreMocks.showSuccess).toHaveBeenCalledWith('admin.teams.balanceUpdated')
  })

  it('扣减：填入金额并提交调用 updateBalance(id, amount, subtract, notes)', async () => {
    const w = await mountAndOpen('subtract')

    const input = w.find('input[type="number"]')
    await input.setValue('10')

    const form = w.find('form')
    await form.trigger('submit')
    await flushPromises()

    expect(apiMocks.updateBalance).toHaveBeenCalledWith(42, 10, 'subtract', '')
    expect(w.emitted('success')).toHaveLength(1)
  })

  it('金额为 0 时提交显示 amountRequired 错误，不调用 API', async () => {
    const w = await mountAndOpen('add')

    const form = w.find('form')
    await form.trigger('submit')
    await flushPromises()

    expect(appStoreMocks.showError).toHaveBeenCalledWith('admin.teams.amountRequired')
    expect(apiMocks.updateBalance).not.toHaveBeenCalled()
  })

  it('扣减超过余额时显示 insufficientBalance 错误', async () => {
    const w = await mountAndOpen('subtract', { balance: 10 })

    const input = w.find('input[type="number"]')
    await input.setValue('20')

    const form = w.find('form')
    await form.trigger('submit')
    await flushPromises()

    expect(appStoreMocks.showError).toHaveBeenCalledWith('admin.teams.insufficientBalance')
    expect(apiMocks.updateBalance).not.toHaveBeenCalled()
  })

  it('扣减时显示"全部"按钮，点击后填入当前余额', async () => {
    const w = await mountAndOpen('subtract', { balance: 33.5 })

    const allBtn = w.findAll('button').find((b) => b.text() === 'admin.teams.withdrawAll')
    expect(allBtn).toBeTruthy()
    await allBtn!.trigger('click')

    const input = w.find('input[type="number"]')
    expect((input.element as HTMLInputElement).valueAsNumber).toBe(33.5)
  })

  it('API 失败时显示错误消息', async () => {
    apiMocks.updateBalance.mockRejectedValueOnce(new Error('Network error'))
    const w = await mountAndOpen('add')

    const input = w.find('input[type="number"]')
    await input.setValue('5')

    const form = w.find('form')
    await form.trigger('submit')
    await flushPromises()

    expect(appStoreMocks.showError).toHaveBeenCalledWith('Network error')
    expect(w.emitted('success')).toBeFalsy()
  })

  it('备注字段填写后随 updateBalance 一起提交', async () => {
    const w = await mountAndOpen('add')

    const input = w.find('input[type="number"]')
    await input.setValue('15')

    const textarea = w.find('textarea')
    await textarea.setValue('Test note')

    const form = w.find('form')
    await form.trigger('submit')
    await flushPromises()

    expect(apiMocks.updateBalance).toHaveBeenCalledWith(42, 15, 'add', 'Test note')
  })
})
