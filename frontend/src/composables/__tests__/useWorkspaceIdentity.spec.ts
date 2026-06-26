import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { useWorkspaceIdentity } from '@/composables/useWorkspaceIdentity'
import { useWorkspaceStore } from '@/stores/workspaces'
import { PERSONAL_PALETTE } from '@/constants/workspacePalette'

vi.mock('@/api/workspaces', () => ({ workspacesAPI: { list: vi.fn().mockResolvedValue({ workspaces: [] }) } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (k: string) => k }) }))

function setup() {
  let api!: ReturnType<typeof useWorkspaceIdentity>
  mount(defineComponent({ setup() { api = useWorkspaceIdentity(); return () => h('div') } }))
  return api
}

describe('useWorkspaceIdentity', () => {
  beforeEach(() => { setActivePinia(createPinia()); localStorage.clear() })

  it('个人激活 → user 图标 / 中性盘 / personal 文案', () => {
    const ws = useWorkspaceStore()
    ws.workspaces = [{ billing_subject_id: 1, type: 'user', name: 'Me', role: 'owner', permissions: {}, balance: 5 } as any]
    ws.activeSubjectId = 1
    const id = setup()
    expect(id.isTeam.value).toBe(false)
    expect(id.icon.value).toBe('user')
    expect(id.typeLabel.value).toBe('workspace.personal')
    expect(id.palette.value).toBe(PERSONAL_PALETTE)
  })

  it('团队激活 → users 图标 / 非中性盘 / team 文案', () => {
    const ws = useWorkspaceStore()
    ws.workspaces = [{ billing_subject_id: 2, type: 'team', team_id: 3, name: 'Acme', role: 'admin', permissions: {}, balance: 9 } as any]
    ws.activeSubjectId = 2
    const id = setup()
    expect(id.isTeam.value).toBe(true)
    expect(id.icon.value).toBe('users')
    expect(id.typeLabel.value).toBe('workspace.team')
    expect(id.palette.value.key).not.toBe('gray')
  })
})
