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
          @click="inviteOpen = true"
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
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900">
                <tr v-for="member in members" :key="member.id">
                  <td class="px-6 py-4">
                    <div class="font-medium text-gray-900 dark:text-white">{{ member.user?.username || member.user?.email || member.user_id }}</div>
                    <div class="text-sm text-gray-500">{{ member.user?.email }}</div>
                  </td>
                  <td class="px-6 py-4 text-sm text-gray-700 dark:text-dark-300">{{ member.role }}</td>
                  <td class="px-6 py-4 text-sm text-gray-700 dark:text-dark-300">{{ member.status }}</td>
                  <td class="px-6 py-4 text-right text-sm font-medium text-gray-900 dark:text-white">${{ formatCost(member.last_7d_actual_cost || 0) }}</td>
                  <td class="px-6 py-4 text-right text-sm text-gray-700 dark:text-dark-300">{{ member.key_count || 0 }}</td>
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
                <div class="text-sm text-gray-500">{{ invitation.role }} · {{ invitation.status }}</div>
              </div>
              <div class="text-sm text-gray-500">{{ formatDate(invitation.expires_at) }}</div>
            </div>
            <div v-if="invitations.length === 0" class="px-6 py-8 text-center text-sm text-gray-500">{{ t('common.noData') }}</div>
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { useWorkspaceStore } from '@/stores/workspaces'
import { workspacesAPI } from '@/api/workspaces'
import type { TeamInvitation, TeamMember } from '@/types'

const { t } = useI18n()
const workspaceStore = useWorkspaceStore()
const loading = ref(false)
const inviteOpen = ref(false)
const members = ref<TeamMember[]>([])
const invitations = ref<TeamInvitation[]>([])

const activeWorkspace = computed(() => workspaceStore.activeWorkspace)
const isTeamWorkspace = computed(() => workspaceStore.isTeamWorkspace)
const canManageMembers = computed(() => workspaceStore.canManageMembers)

function formatCost(value: number): string {
  return value.toFixed(4)
}

function formatDate(value: string): string {
  return new Date(value).toLocaleDateString()
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

onMounted(() => {
  loadMembers().catch((error) => console.error('Failed to load team members:', error))
})
</script>
