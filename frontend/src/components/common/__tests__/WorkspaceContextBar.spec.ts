import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { mount } from '@vue/test-utils'
import WorkspaceContextBar from '@/components/common/WorkspaceContextBar.vue'
import { useWorkspaceStore } from '@/stores/workspaces'

vi.mock('@/api/workspaces', () => ({ workspacesAPI: { list: vi.fn().mockResolvedValue({ workspaces: [] }) } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (k: string) => k }) }))

const mountBar = () =>
  mount(WorkspaceContextBar, { global: { stubs: { WorkspaceBadge: true, Icon: true } } })

describe('WorkspaceContextBar', () => {
  beforeEach(() => { setActivePinia(createPinia()); localStorage.clear() })

  it('团队：显示余额 + 角色', () => {
    const ws = useWorkspaceStore()
    ws.workspaces = [{ billing_subject_id: 2, type: 'team', team_id: 1, name: 'Acme', role: 'admin', permissions: {}, balance: 12.5 } as any]
    ws.activeSubjectId = 2
    const w = mountBar()
    expect(w.text()).toContain('12.50')
    expect(w.text()).toContain('workspace.roleLabel')
    expect(w.text()).toContain('workspace.roles.admin')
  })

  it('个人：显示余额、不显角色', () => {
    const ws = useWorkspaceStore()
    ws.workspaces = [{ billing_subject_id: 1, type: 'user', name: 'Me', role: 'owner', permissions: {}, balance: 3 } as any]
    ws.activeSubjectId = 1
    const w = mountBar()
    expect(w.text()).toContain('3.00')
    expect(w.text()).not.toContain('workspace.roleLabel')
  })

  it('无激活工作区 → 不渲染', () => {
    const w = mountBar()
    expect(w.find('div').exists()).toBe(false)
  })
})
