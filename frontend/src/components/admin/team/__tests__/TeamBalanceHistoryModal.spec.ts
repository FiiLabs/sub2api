import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

const apiMocks = vi.hoisted(() => ({
  getTeamBalanceHistory: vi.fn(),
}))

vi.mock('@/api/admin/teams', () => ({
  adminTeamsAPI: {
    getTeamBalanceHistory: apiMocks.getTeamBalanceHistory,
  },
}))

vi.mock('@/utils/format', () => ({
  formatDateTime: (s: string) => s,
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
    props: ['show', 'title', 'width', 'zIndex'],
    template: '<div v-if="show"><slot /><slot name="footer" /></div>',
  },
}))

import TeamBalanceHistoryModal from '../TeamBalanceHistoryModal.vue'
import type { AdminTeam } from '@/types'
import type { TeamBalanceHistoryItem } from '@/api/admin/teams'

function makeTeam(overrides: Partial<AdminTeam> = {}): AdminTeam {
  return {
    id: 7,
    name: 'Alpha Team',
    slug: 'alpha-team',
    status: 'active',
    owner_user_id: 1,
    owner: null,
    billing_subject_id: 20,
    balance: 100.00,
    member_count: 3,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    concurrency: 0,
    rpm_limit: 0,
    ...overrides,
  }
}

function makeHistoryItem(overrides: Partial<TeamBalanceHistoryItem> = {}): TeamBalanceHistoryItem {
  return {
    id: 1,
    code: 'CODE001',
    type: 'admin_balance',
    value: 10.00,
    status: 'used',
    billing_subject_id: 20,
    used_at: '2026-01-02T10:00:00Z',
    created_at: '2026-01-01T00:00:00Z',
    notes: '',
    ...overrides,
  }
}

function makeEmptyResponse() {
  return { items: [], total: 0, page: 1, page_size: 15, pages: 0 }
}

async function mountAndOpen(teamOverrides: Partial<AdminTeam> = {}) {
  const w = mount(TeamBalanceHistoryModal, {
    props: { show: false, team: makeTeam(teamOverrides) },
  })
  await w.setProps({ show: true })
  await flushPromises()
  return w
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  apiMocks.getTeamBalanceHistory.mockResolvedValue(makeEmptyResponse())
})

describe('TeamBalanceHistoryModal', () => {
  it('show=true 时调用 getTeamBalanceHistory(teamId, 1, 15)', async () => {
    await mountAndOpen()
    expect(apiMocks.getTeamBalanceHistory).toHaveBeenCalledWith(7, 1, 15)
  })

  it('渲染团队名称和余额', async () => {
    const w = await mountAndOpen()
    expect(w.html()).toContain('Alpha Team')
    expect(w.html()).toContain('100.00')
  })

  it('无历史记录时显示 noBalanceHistory 文字', async () => {
    const w = await mountAndOpen()
    expect(w.html()).toContain('admin.teams.noBalanceHistory')
  })

  it('有历史记录时渲染各条目', async () => {
    apiMocks.getTeamBalanceHistory.mockResolvedValueOnce({
      items: [
        makeHistoryItem({ id: 1, value: 20.00, notes: 'Top-up' }),
        makeHistoryItem({ id: 2, value: -5.00, notes: 'Deduction' }),
      ],
      total: 2,
      page: 1,
      page_size: 15,
      pages: 1,
    })
    const w = await mountAndOpen()
    // 充值显示 balanceAddedAdmin，扣减显示 balanceDeductedAdmin
    expect(w.html()).toContain('redeem.balanceAddedAdmin')
    expect(w.html()).toContain('redeem.balanceDeductedAdmin')
    expect(w.html()).toContain('Top-up')
    expect(w.html()).toContain('Deduction')
    // 金额正负符号：正数显示 +$20.00，负数显示 $-5.00（模板里 $ 在前 value.toFixed(2) 在后）
    expect(w.html()).toContain('+$20.00')
    expect(w.html()).toContain('$-5.00')
  })

  it('show 再次打开时重新加载第 1 页', async () => {
    const w = await mountAndOpen()
    expect(apiMocks.getTeamBalanceHistory).toHaveBeenCalledTimes(1)

    await w.setProps({ show: false })
    await w.setProps({ show: true })
    await flushPromises()

    expect(apiMocks.getTeamBalanceHistory).toHaveBeenCalledTimes(2)
    expect(apiMocks.getTeamBalanceHistory).toHaveBeenLastCalledWith(7, 1, 15)
  })

  it('多页时显示分页按钮', async () => {
    apiMocks.getTeamBalanceHistory.mockResolvedValueOnce({
      items: Array.from({ length: 15 }, (_, i) => makeHistoryItem({ id: i + 1 })),
      total: 30,
      page: 1,
      page_size: 15,
      pages: 2,
    })
    const w = await mountAndOpen()
    expect(w.html()).toContain('pagination.previous')
    expect(w.html()).toContain('pagination.next')
  })

  it('单页时不显示分页按钮', async () => {
    apiMocks.getTeamBalanceHistory.mockResolvedValueOnce({
      items: [makeHistoryItem({ id: 1 })],
      total: 1,
      page: 1,
      page_size: 15,
      pages: 1,
    })
    const w = await mountAndOpen()
    expect(w.html()).not.toContain('pagination.previous')
    expect(w.html()).not.toContain('pagination.next')
  })

  it('备注超过 60 字符时截断显示（text 内容含省略号，全文在 title 属性）', async () => {
    const longNote = 'A'.repeat(70)
    apiMocks.getTeamBalanceHistory.mockResolvedValueOnce({
      items: [makeHistoryItem({ id: 1, notes: longNote })],
      total: 1,
      page: 1,
      page_size: 15,
      pages: 1,
    })
    const w = await mountAndOpen()
    // 截断后以 ... 结尾，显示内容为前55个字符+'...'
    expect(w.html()).toContain('AAAAA...')
    // title 属性保留全文（组件设计如此），但文本节点只有55个字符
    const noteEl = w.find('[title]')
    expect(noteEl.exists()).toBe(true)
    expect(noteEl.text()).toContain('...')
    expect(noteEl.text().length).toBeLessThan(longNote.length)
  })
})
