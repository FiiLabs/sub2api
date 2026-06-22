<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 class="text-xl font-semibold text-gray-900 dark:text-white">{{ activeWorkspace?.name }}</h2>
          <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('members.description') }}</p>
        </div>
        <button
          v-if="canManageMembers"
          type="button"
          class="btn-primary"
          @click="openInvite"
        >
          {{ t('members.invite') }}
        </button>
      </div>

      <div v-if="!isTeamWorkspace" class="card p-6">
        <p class="text-sm text-gray-600 dark:text-dark-300">{{ t('members.teamRequired') }}</p>
      </div>

      <div v-else-if="loading" class="flex justify-center py-12">
        <LoadingSpinner />
      </div>

      <template v-else>
        <div class="card overflow-hidden">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('members.title') }}</h3>
          </div>
          <div class="overflow-x-auto">
            <table class="min-w-full divide-y divide-gray-100 dark:divide-dark-700">
              <thead class="bg-gray-50 dark:bg-dark-800">
                <tr>
                  <th class="px-6 py-3 text-left text-xs font-medium uppercase text-gray-500">{{ t('common.user') }}</th>
                  <th class="px-6 py-3 text-left text-xs font-medium uppercase text-gray-500">{{ t('members.role') }}</th>
                  <th class="px-6 py-3 text-left text-xs font-medium uppercase text-gray-500">{{ t('members.status') }}</th>
                  <th class="px-6 py-3 text-right text-xs font-medium uppercase text-gray-500">7d</th>
                  <th class="px-6 py-3 text-right text-xs font-medium uppercase text-gray-500">Keys</th>
                  <th v-if="showActionsColumn" class="px-6 py-3 text-right text-xs font-medium uppercase text-gray-500">{{ t('members.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900">
                <tr v-for="member in members" :key="member.id">
                  <td class="px-6 py-4">
                    <div class="font-medium text-gray-900 dark:text-white">{{ member.user?.username || member.user?.email || member.user_id }}</div>
                    <div class="text-sm text-gray-500">{{ member.user?.email }}</div>
                  </td>
                  <td class="px-6 py-4 text-sm text-gray-700 dark:text-dark-300">
                    <Select
                      v-if="canManageMembers && member.role !== 'owner'"
                      :model-value="member.role"
                      :options="assignableRoleOptions"
                      class="w-36"
                      @update:model-value="(val) => handleChangeRole(member, String(val))"
                    />
                    <span v-else>{{ roleLabel(member.role) }}</span>
                  </td>
                  <td class="px-6 py-4 text-sm text-gray-700 dark:text-dark-300">{{ t('members.memberStatus.' + member.status) }}</td>
                  <td class="px-6 py-4 text-right text-sm font-medium text-gray-900 dark:text-white">${{ formatCost(member.last_7d_actual_cost || 0) }}</td>
                  <td class="px-6 py-4 text-right text-sm text-gray-700 dark:text-dark-300">{{ member.key_count || 0 }}</td>
                  <td v-if="showActionsColumn" class="px-6 py-4">
                    <div v-if="member.role !== 'owner'" class="flex items-center justify-end gap-1">
                      <button
                        v-if="canManageMembers && member.status !== 'left'"
                        type="button"
                        class="rounded px-2 py-1 text-xs font-medium transition-colors"
                        :class="member.status === 'active'
                          ? 'text-orange-600 hover:bg-orange-50 dark:text-orange-400 dark:hover:bg-orange-900/20'
                          : 'text-green-600 hover:bg-green-50 dark:text-green-400 dark:hover:bg-green-900/20'"
                        @click="handleToggleStatus(member)"
                      >
                        {{ member.status === 'active' ? t('members.suspend') : t('members.activate') }}
                      </button>
                      <button
                        v-if="isOwnerActor && member.status === 'active'"
                        type="button"
                        class="rounded px-2 py-1 text-xs font-medium text-primary-600 transition-colors hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
                        @click="handleTransfer(member)"
                      >
                        {{ t('members.transferOwnership') }}
                      </button>
                      <button
                        v-if="canManageMembers"
                        type="button"
                        class="rounded px-2 py-1 text-xs font-medium text-red-600 transition-colors hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
                        @click="handleRemove(member)"
                      >
                        {{ t('members.remove') }}
                      </button>
                    </div>
                    <span v-else class="block text-right text-xs text-gray-400">-</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div class="card overflow-hidden">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('members.pendingInvitations') }}</h3>
          </div>
          <div class="divide-y divide-gray-100 dark:divide-dark-700">
            <div v-for="invitation in invitations" :key="invitation.id" class="flex items-center justify-between px-6 py-4">
              <div>
                <div class="font-medium text-gray-900 dark:text-white">{{ invitation.email }}</div>
                <div class="text-sm text-gray-500">{{ roleLabel(invitation.role) }} · {{ invitation.status }}</div>
              </div>
              <div class="text-sm text-gray-500">{{ formatDate(invitation.expires_at) }}</div>
            </div>
            <div v-if="invitations.length === 0" class="px-6 py-8 text-center text-sm text-gray-500">{{ t('common.noData') }}</div>
          </div>
        </div>
      </template>
    </div>

    <!-- Invite member modal -->
    <BaseDialog
      :show="inviteOpen"
      :title="t('members.inviteTitle')"
      width="normal"
      @close="closeInvite"
    >
      <div class="space-y-4">
        <div>
          <label class="input-label" for="invite-email">{{ t('members.email') }}</label>
          <input
            id="invite-email"
            v-model="inviteForm.email"
            type="email"
            class="input"
            :placeholder="t('members.emailPlaceholder')"
            @keyup.enter="handleInvite"
          />
        </div>
        <div>
          <label class="input-label">{{ t('members.role') }}</label>
          <Select v-model="inviteForm.role" :options="assignableRoleOptions" />
        </div>

        <!-- Copyable accept link shown after a successful invite -->
        <div v-if="acceptLink" class="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
          <label class="input-label">{{ t('members.acceptLink') }}</label>
          <div class="flex items-center gap-2">
            <input :value="acceptLink" type="text" readonly class="input flex-1 text-xs" @focus="selectOnFocus" />
            <button type="button" class="btn btn-secondary shrink-0" @click="copyAcceptLink">
              <Icon name="copy" size="sm" class="mr-1.5" />
              {{ t('members.copyLink') }}
            </button>
          </div>
          <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('members.acceptLinkHint') }}</p>
        </div>
      </div>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeInvite">{{ t('common.close') }}</button>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="inviteSubmitting || !inviteForm.email.trim()"
            @click="handleInvite"
          >
            <Icon v-if="inviteSubmitting" name="refresh" size="sm" class="mr-2 animate-spin" />
            {{ inviteSubmitting ? t('members.sending') : t('members.send') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Remove member confirmation -->
    <ConfirmDialog
      :show="showRemoveConfirm"
      :title="t('members.removeTitle')"
      :message="t('members.removeConfirm', { user: removingMember?.user?.email || removingMember?.user_id })"
      :danger="true"
      @confirm="confirmRemove"
      @cancel="showRemoveConfirm = false"
    />

    <!-- Transfer ownership confirmation -->
    <ConfirmDialog
      :show="showTransferConfirm"
      :title="t('members.transferTitle')"
      :message="t('members.transferConfirm', { user: transferringMember?.user?.email || transferringMember?.user_id })"
      @confirm="confirmTransfer"
      @cancel="showTransferConfirm = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useWorkspaceStore } from '@/stores/workspaces'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { workspacesAPI } from '@/api/workspaces'
import type { TeamInvitation, TeamMember, TeamRole } from '@/types'

type AssignableRole = Exclude<TeamRole, 'owner'>

const { t } = useI18n()
const workspaceStore = useWorkspaceStore()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const loading = ref(false)
const inviteOpen = ref(false)
const members = ref<TeamMember[]>([])
const invitations = ref<TeamInvitation[]>([])

const activeWorkspace = computed(() => workspaceStore.activeWorkspace)
const isTeamWorkspace = computed(() => workspaceStore.isTeamWorkspace)
const canManageMembers = computed(() => workspaceStore.canManageMembers)
// Ownership transfer is owner-only (stricter than manage-members).
const isOwnerActor = computed(() => workspaceStore.activeWorkspace?.role === 'owner')
// Show the actions column when the actor can do anything to a member row.
const showActionsColumn = computed(() => canManageMembers.value || isOwnerActor.value)

const assignableRoleOptions = computed(() =>
  (['admin', 'billing', 'developer', 'viewer'] as AssignableRole[]).map((role) => ({
    value: role,
    label: roleLabel(role)
  }))
)

function roleLabel(role: string): string {
  const key = `members.roles.${role}`
  const label = t(key)
  return label === key ? role : label
}

function formatCost(value: number): string {
  return value.toFixed(4)
}

function formatDate(value: string): string {
  return new Date(value).toLocaleDateString()
}

function selectOnFocus(event: FocusEvent): void {
  ;(event.target as HTMLInputElement)?.select()
}

async function loadMembers(): Promise<void> {
  const teamId = activeWorkspace.value?.team_id
  if (!teamId) return
  loading.value = true
  try {
    const data = await workspacesAPI.listMembers(teamId)
    members.value = data.members
    invitations.value = data.invitations
  } finally {
    loading.value = false
  }
}

// ==================== Invite ====================

const inviteForm = reactive<{ email: string; role: AssignableRole }>({ email: '', role: 'developer' })
const inviteSubmitting = ref(false)
const acceptLink = ref('')

function openInvite(): void {
  inviteForm.email = ''
  inviteForm.role = 'developer'
  acceptLink.value = ''
  inviteOpen.value = true
}

function closeInvite(): void {
  inviteOpen.value = false
  acceptLink.value = ''
}

async function handleInvite(): Promise<void> {
  const teamId = activeWorkspace.value?.team_id
  if (!teamId) return
  const email = inviteForm.email.trim()
  if (!email) {
    appStore.showError(t('members.emailRequired'))
    return
  }
  inviteSubmitting.value = true
  try {
    const res = await workspacesAPI.inviteMember(teamId, { email, role: inviteForm.role })
    acceptLink.value = res.accept_link || ''
    appStore.showSuccess(t('members.inviteSuccess'))
    await loadMembers()
  } catch (error: unknown) {
    const err = error as { message?: string }
    appStore.showError(err?.message || t('members.inviteFailed'))
  } finally {
    inviteSubmitting.value = false
  }
}

async function copyAcceptLink(): Promise<void> {
  if (acceptLink.value) {
    await copyToClipboard(acceptLink.value, t('members.linkCopied'))
  }
}

// ==================== Member row actions ====================

async function handleChangeRole(member: TeamMember, role: string): Promise<void> {
  const teamId = activeWorkspace.value?.team_id
  if (!teamId || role === member.role) return
  try {
    await workspacesAPI.updateMember(teamId, member.user_id, { role: role as TeamRole })
    appStore.showSuccess(t('members.memberUpdated'))
    await loadMembers()
  } catch (error: unknown) {
    const err = error as { message?: string }
    appStore.showError(err?.message || t('members.failedToUpdateMember'))
  }
}

async function handleToggleStatus(member: TeamMember): Promise<void> {
  const teamId = activeWorkspace.value?.team_id
  if (!teamId) return
  const newStatus = member.status === 'active' ? 'suspended' : 'active'
  try {
    await workspacesAPI.updateMember(teamId, member.user_id, { status: newStatus })
    appStore.showSuccess(t('members.memberUpdated'))
    await loadMembers()
  } catch (error: unknown) {
    const err = error as { message?: string }
    appStore.showError(err?.message || t('members.failedToUpdateMember'))
  }
}

const showRemoveConfirm = ref(false)
const removingMember = ref<TeamMember | null>(null)

function handleRemove(member: TeamMember): void {
  removingMember.value = member
  showRemoveConfirm.value = true
}

async function confirmRemove(): Promise<void> {
  const teamId = activeWorkspace.value?.team_id
  if (!teamId || !removingMember.value) return
  try {
    await workspacesAPI.removeMember(teamId, removingMember.value.user_id)
    appStore.showSuccess(t('members.memberRemoved'))
    showRemoveConfirm.value = false
    removingMember.value = null
    await loadMembers()
  } catch (error: unknown) {
    const err = error as { message?: string }
    appStore.showError(err?.message || t('members.failedToRemoveMember'))
  }
}

// ==================== Transfer ownership ====================

const showTransferConfirm = ref(false)
const transferringMember = ref<TeamMember | null>(null)

function handleTransfer(member: TeamMember): void {
  transferringMember.value = member
  showTransferConfirm.value = true
}

async function confirmTransfer(): Promise<void> {
  const teamId = activeWorkspace.value?.team_id
  if (!teamId || !transferringMember.value) return
  try {
    await workspacesAPI.transferOwnership(teamId, transferringMember.value.user_id)
    appStore.showSuccess(t('members.transferSuccess'))
    showTransferConfirm.value = false
    transferringMember.value = null
    await loadMembers()
    await workspaceStore.loadWorkspaces()
  } catch (error: unknown) {
    const err = error as { message?: string }
    appStore.showError(err?.message || t('members.failedToTransfer'))
  }
}

onMounted(() => {
  loadMembers().catch((error) => console.error('Failed to load team members:', error))
})
</script>
