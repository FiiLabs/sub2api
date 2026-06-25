import { describe, expect, it, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { defineComponent } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { useSubjectScopedLoader } from '@/composables/useSubjectScopedLoader'
import { useWorkspaceStore } from '@/stores/workspaces'

vi.mock('@/api/workspaces', () => ({ workspacesAPI: { list: vi.fn().mockResolvedValue({ workspaces: [] }) } }))

const Host = (load: () => void) => defineComponent({ setup() { useSubjectScopedLoader(load); return () => null } })

describe('useSubjectScopedLoader', () => {
  beforeEach(() => { setActivePinia(createPinia()); localStorage.clear() })

  it('loads once on mount', async () => {
    const load = vi.fn()
    mount(Host(load))
    await flushPromises()
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('reloads when subjectVersion bumps', async () => {
    const load = vi.fn()
    mount(Host(load))
    await flushPromises()
    const ws = useWorkspaceStore()
    ws.workspaces = [{ billing_subject_id: 1, type: 'user', name: 'P', role: 'owner', permissions: {}, balance: 0 } as any,
                     { billing_subject_id: 2, type: 'team', name: 'T', role: 'admin', permissions: {}, balance: 0 } as any]
    ws.activeSubjectId = 1
    await flushPromises()
    ws.switchWorkspace(2)
    await flushPromises()
    expect(load.mock.calls.length).toBeGreaterThanOrEqual(2)
  })

  it('does not load on mount when immediate is false', async () => {
    const load = vi.fn()
    mount(defineComponent({ setup() { useSubjectScopedLoader(load, { immediate: false }); return () => null } }))
    await flushPromises()
    expect(load).not.toHaveBeenCalled()
  })
})
