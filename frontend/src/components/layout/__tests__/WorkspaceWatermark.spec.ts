import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { mount } from '@vue/test-utils'
import WorkspaceWatermark from '@/components/layout/WorkspaceWatermark.vue'
import { useWorkspaceStore } from '@/stores/workspaces'

vi.mock('@/api/workspaces', () => ({ workspacesAPI: { list: vi.fn().mockResolvedValue({ workspaces: [] }) } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (k: string) => k }) }))

describe('WorkspaceWatermark', () => {
  beforeEach(() => { setActivePinia(createPinia()); localStorage.clear() })

  it('团队工作区 → 渲染铺排水印含团队名', () => {
    const ws = useWorkspaceStore()
    ws.workspaces = [{ billing_subject_id: 2, type: 'team', team_id: 1, name: 'Acme', role: 'admin', permissions: {}, balance: 0 } as any]
    ws.activeSubjectId = 2
    const w = mount(WorkspaceWatermark)
    expect(w.find('.fixed').exists()).toBe(true)
    expect(w.text()).toContain('Acme')
  })

  it('个人工作区 → 不渲染', () => {
    const ws = useWorkspaceStore()
    ws.workspaces = [{ billing_subject_id: 1, type: 'user', name: 'Me', role: 'owner', permissions: {}, balance: 0 } as any]
    ws.activeSubjectId = 1
    const w = mount(WorkspaceWatermark)
    expect(w.find('.fixed').exists()).toBe(false)
  })
})
