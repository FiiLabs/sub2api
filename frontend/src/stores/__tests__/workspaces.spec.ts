import { describe, expect, it, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useWorkspaceStore } from '../workspaces'

vi.mock('@/api/workspaces', () => ({
  workspacesAPI: {
    list: vi.fn().mockResolvedValue({
      workspaces: [
        { billing_subject_id: 1, type: 'user', user_id: 9, name: 'Personal', role: 'owner', permissions: { 'team.usage.view': true }, balance: 3 },
        { billing_subject_id: 2, type: 'team', team_id: 7, name: 'Platform', role: 'admin', permissions: { 'team.members.manage': true }, balance: 20 }
      ]
    })
  }
}))

describe('workspace store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
  })

  it('loads workspaces and defaults to personal workspace', async () => {
    const store = useWorkspaceStore()
    await store.loadWorkspaces()
    expect(store.activeWorkspace?.billing_subject_id).toBe(1)
    expect(store.activeWorkspace?.type).toBe('user')
  })

  it('switches active workspace and persists the selected subject', async () => {
    const store = useWorkspaceStore()
    await store.loadWorkspaces()
    store.switchWorkspace(2)
    expect(store.activeWorkspace?.name).toBe('Platform')
    expect(localStorage.getItem('active_workspace_subject_id')).toBe('2')
  })

  it('bumps subjectVersion on workspace switch', async () => {
    const store = useWorkspaceStore()
    await store.loadWorkspaces()
    const before = store.subjectVersion
    store.switchWorkspace(2)
    expect(store.subjectVersion).toBe(before + 1)
  })

  it('does not bump subjectVersion when switching to a non-existent workspace', async () => {
    const store = useWorkspaceStore()
    await store.loadWorkspaces()
    const before = store.subjectVersion
    store.switchWorkspace(999)
    expect(store.subjectVersion).toBe(before)
  })
})
