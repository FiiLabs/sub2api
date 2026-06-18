import { describe, expect, it, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MembersView from '../MembersView.vue'
import { useWorkspaceStore } from '@/stores/workspaces'

vi.mock('@/api/workspaces', () => ({
  workspacesAPI: {
    listMembers: vi.fn().mockResolvedValue({
      members: [
        { id: 1, team_id: 7, user_id: 11, role: 'owner', status: 'active', user: { id: 11, email: 'owner@example.com', username: 'Owner', role: 'user', balance: 0, concurrency: 1, status: 'active', allowed_groups: null, balance_notify_enabled: true, balance_notify_threshold: null, balance_notify_extra_emails: [], created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }, key_count: 2, last_7d_actual_cost: 1.25 }
      ],
      invitations: [{ id: 2, team_id: 7, email: 'dev@example.com', role: 'developer', status: 'pending', expires_at: '2026-06-30T00:00:00Z', created_at: '2026-06-16T00:00:00Z' }]
    })
  }
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

describe('MembersView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('renders team members and pending invitations', async () => {
    const store = useWorkspaceStore()
    store.workspaces = [{ billing_subject_id: 2, type: 'team', team_id: 7, name: 'Platform', role: 'admin', permissions: { 'team.members.manage': true }, balance: 20 }]
    store.activeSubjectId = 2

    const wrapper = mount(MembersView, {
      global: {
        stubs: { AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true, Icon: true },
        mocks: { $t: (key: string) => key }
      }
    })

    await Promise.resolve()
    await Promise.resolve()

    expect(wrapper.text()).toContain('owner@example.com')
    expect(wrapper.text()).toContain('dev@example.com')
    expect(wrapper.text()).toContain('developer')
  })
})
