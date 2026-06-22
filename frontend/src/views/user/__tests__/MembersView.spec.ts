import { describe, expect, it, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MembersView from '../MembersView.vue'
import { useWorkspaceStore } from '@/stores/workspaces'
import { workspacesAPI } from '@/api/workspaces'

vi.mock('@/api/workspaces', () => ({
  workspacesAPI: {
    listMembers: vi.fn().mockResolvedValue({
      members: [
        {
          id: 1,
          team_id: 7,
          user_id: 11,
          role: 'owner',
          status: 'active',
          user: { id: 11, email: 'owner@example.com', username: 'Owner', role: 'user', balance: 0, concurrency: 1, status: 'active', allowed_groups: null, balance_notify_enabled: true, balance_notify_threshold: null, balance_notify_extra_emails: [], created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
          key_count: 2,
          last_7d_actual_cost: 1.25
        },
        {
          id: 3,
          team_id: 7,
          user_id: 22,
          role: 'developer',
          status: 'active',
          user: { id: 22, email: 'member@example.com', username: 'Member', role: 'user', balance: 0, concurrency: 1, status: 'active', allowed_groups: null, balance_notify_enabled: true, balance_notify_threshold: null, balance_notify_extra_emails: [], created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
          key_count: 0,
          last_7d_actual_cost: 0
        }
      ],
      invitations: [{ id: 2, team_id: 7, email: 'dev@example.com', role: 'developer', status: 'pending', expires_at: '2026-06-30T00:00:00Z', created_at: '2026-06-16T00:00:00Z' }]
    }),
    inviteMember: vi.fn().mockResolvedValue({
      invitation: { id: 9, team_id: 7, email: 'new@example.com', role: 'developer', status: 'pending', expires_at: '2026-07-30T00:00:00Z', created_at: '2026-06-22T00:00:00Z' },
      token: 'plain-token',
      accept_link: 'https://app.example.com/teams/accept?token=plain-token'
    }),
    updateMember: vi.fn().mockResolvedValue({}),
    removeMember: vi.fn().mockResolvedValue({ message: 'ok' }),
    transferOwnership: vi.fn().mockResolvedValue({}),
    loadWorkspaces: vi.fn()
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: { value: false }, copyToClipboard: vi.fn().mockResolvedValue(true) })
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      // Echo the key (with simple interpolation) so assertions can match on keys.
      t: (key: string) => key
    })
  }
})

// Stub heavy/teleporting children so we can interact with rendered DOM directly.
const globalStubs = {
  AppLayout: { template: '<div><slot /></div>' },
  LoadingSpinner: true,
  Icon: true,
  // Render BaseDialog/ConfirmDialog inline (no Teleport) when shown.
  BaseDialog: { props: ['show', 'title'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  ConfirmDialog: {
    props: ['show', 'title', 'message'],
    emits: ['confirm', 'cancel'],
    template: '<div v-if="show" class="confirm-dialog"><button class="confirm-yes" @click="$emit(\'confirm\')">yes</button></div>'
  },
  // Expose a button that triggers update:model-value to simulate a role change.
  Select: {
    props: ['modelValue', 'options'],
    emits: ['update:modelValue'],
    template: '<button class="select-stub" @click="$emit(\'update:modelValue\', \'admin\')">{{ modelValue }}</button>'
  }
}

function mountAsRole(role: string) {
  const store = useWorkspaceStore()
  store.workspaces = [{ billing_subject_id: 2, type: 'team', team_id: 7, name: 'Platform', role: role as any, permissions: { 'team.members.manage': true }, balance: 20 }]
  store.activeSubjectId = 2
  return mount(MembersView, { global: { stubs: globalStubs } })
}

describe('MembersView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('renders team members and pending invitations', async () => {
    const wrapper = mountAsRole('admin')
    await flushPromises()

    expect(wrapper.text()).toContain('owner@example.com')
    expect(wrapper.text()).toContain('member@example.com')
    expect(wrapper.text()).toContain('dev@example.com')
    expect(wrapper.text()).toContain('developer')
  })

  it('invites a member and shows a copyable accept link', async () => {
    const wrapper = mountAsRole('admin')
    await flushPromises()

    // Open invite modal
    await wrapper.find('button.btn-primary').trigger('click')
    await flushPromises()

    const emailInput = wrapper.find('#invite-email')
    await emailInput.setValue('new@example.com')

    // Click the send button (the one that is not "close")
    const sendBtn = wrapper.findAll('button').find((b) => b.text() === 'members.send')
    expect(sendBtn).toBeTruthy()
    await sendBtn!.trigger('click')
    await flushPromises()

    expect(workspacesAPI.inviteMember).toHaveBeenCalledWith(7, { email: 'new@example.com', role: 'developer' })
    // Accept link field is rendered with the returned link
    const linkInput = wrapper.findAll('input').find((i) => (i.element as HTMLInputElement).value.includes('teams/accept?token='))
    expect(linkInput).toBeTruthy()
  })

  it('changes a member role via the role select', async () => {
    const wrapper = mountAsRole('admin')
    await flushPromises()

    // The non-owner member row renders a Select stub; clicking emits update:modelValue 'admin'.
    const select = wrapper.find('button.select-stub')
    expect(select.exists()).toBe(true)
    await select.trigger('click')
    await flushPromises()

    expect(workspacesAPI.updateMember).toHaveBeenCalledWith(7, 22, { role: 'admin' })
  })

  it('suspends an active member', async () => {
    const wrapper = mountAsRole('admin')
    await flushPromises()

    const suspendBtn = wrapper.findAll('button').find((b) => b.text() === 'members.suspend')
    expect(suspendBtn).toBeTruthy()
    await suspendBtn!.trigger('click')
    await flushPromises()

    expect(workspacesAPI.updateMember).toHaveBeenCalledWith(7, 22, { status: 'suspended' })
  })

  it('removes a member after confirmation', async () => {
    const wrapper = mountAsRole('admin')
    await flushPromises()

    const removeBtn = wrapper.findAll('button').find((b) => b.text() === 'members.remove')
    expect(removeBtn).toBeTruthy()
    await removeBtn!.trigger('click')
    await flushPromises()

    await wrapper.find('button.confirm-yes').trigger('click')
    await flushPromises()

    expect(workspacesAPI.removeMember).toHaveBeenCalledWith(7, 22)
  })

  it('hides transfer ownership when the actor is not the owner', async () => {
    const wrapper = mountAsRole('admin')
    await flushPromises()

    const transferBtn = wrapper.findAll('button').find((b) => b.text() === 'members.transferOwnership')
    expect(transferBtn).toBeUndefined()
  })

  it('transfers ownership when the actor is the owner', async () => {
    const wrapper = mountAsRole('owner')
    await flushPromises()

    const transferBtn = wrapper.findAll('button').find((b) => b.text() === 'members.transferOwnership')
    expect(transferBtn).toBeTruthy()
    await transferBtn!.trigger('click')
    await flushPromises()

    await wrapper.find('button.confirm-yes').trigger('click')
    await flushPromises()

    expect(workspacesAPI.transferOwnership).toHaveBeenCalledWith(7, 22)
  })

  it('does not show member mutation controls without manage permission', async () => {
    const store = useWorkspaceStore()
    store.workspaces = [{ billing_subject_id: 2, type: 'team', team_id: 7, name: 'Platform', role: 'viewer' as any, permissions: {}, balance: 20 }]
    store.activeSubjectId = 2
    const wrapper = mount(MembersView, { global: { stubs: globalStubs } })
    await flushPromises()

    // No invite button, no role select, no remove/suspend buttons.
    expect(wrapper.find('button.btn-primary').exists()).toBe(false)
    expect(wrapper.find('button.select-stub').exists()).toBe(false)
    expect(wrapper.findAll('button').find((b) => b.text() === 'members.remove')).toBeUndefined()
  })
})
