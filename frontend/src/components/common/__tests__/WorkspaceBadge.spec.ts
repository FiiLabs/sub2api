import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { mount } from '@vue/test-utils'
import WorkspaceBadge from '@/components/common/WorkspaceBadge.vue'
import type { WorkspaceSubject } from '@/types'

vi.mock('@/api/workspaces', () => ({ workspacesAPI: { list: vi.fn().mockResolvedValue({ workspaces: [] }) } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (k: string) => k }) }))

const personal: WorkspaceSubject = { billing_subject_id: 1, type: 'user', name: 'Me', role: 'owner', permissions: {}, balance: 0 }
const team: WorkspaceSubject = { billing_subject_id: 2, type: 'team', team_id: 4, name: 'Acme', role: 'admin', permissions: {}, balance: 0 }
const TEAM_FAMILIES = ['emerald', 'teal', 'sky', 'blue', 'amber', 'orange', 'rose', 'pink']

const mountBadge = (props: Record<string, unknown>) =>
  mount(WorkspaceBadge, { props, global: { stubs: { Icon: true } } })

describe('WorkspaceBadge', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('个人：名称 + personal 文案', () => {
    const w = mountBadge({ subject: personal, showType: true })
    expect(w.text()).toContain('Me')
    expect(w.text()).toContain('workspace.personal')
  })

  it('团队：名称 + team 文案 + 套用非中性 chip 类', () => {
    const w = mountBadge({ subject: team, showType: true })
    expect(w.text()).toContain('Acme')
    expect(w.text()).toContain('workspace.team')
    expect(w.classes().some((c) => TEAM_FAMILIES.some((f) => c.includes(f)))).toBe(true)
  })

  it('compact 隐藏名称', () => {
    const w = mountBadge({ subject: team, compact: true })
    expect(w.text()).not.toContain('Acme')
  })

  it('responsiveCompact：移动端隐藏文字组、图标常显', () => {
    const w = mountBadge({ subject: team, responsiveCompact: true })
    const group = w.find('span.hidden')
    expect(group.exists()).toBe(true)
    expect(group.classes()).toContain('sm:inline-flex')
    expect(w.text()).toContain('Acme')
  })
})
