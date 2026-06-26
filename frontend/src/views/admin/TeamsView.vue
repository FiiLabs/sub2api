<template>
  <AppLayout>
    <TablePageLayout>
      <!-- Search + filters + actions -->
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="flex flex-1 flex-wrap items-center gap-3">
            <!-- Search -->
            <div class="relative w-full md:w-72">
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
              />
              <input
                v-model="searchQuery"
                type="text"
                :placeholder="t('admin.teams.search')"
                class="input pl-10"
                @input="handleSearch"
              />
            </div>

            <!-- Status filter -->
            <div class="w-full sm:w-40">
              <Select
                v-model="statusFilter"
                :options="[
                  { value: '', label: t('admin.teams.allStatus') },
                  { value: 'active', label: t('common.active') },
                  { value: 'disabled', label: t('admin.teams.disabled') }
                ]"
                @change="applyFilter"
              />
            </div>
          </div>

          <div class="flex items-center justify-end gap-2">
            <button
              @click="loadTeams"
              :disabled="loading"
              class="btn btn-secondary px-2 md:px-3"
              :title="t('common.refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button @click="openCreate" class="btn btn-primary">
              <Icon name="plus" size="md" class="mr-2" />
              {{ t('admin.teams.create') }}
            </button>
          </div>
        </div>
      </template>

      <!-- Teams table -->
      <template #table>
        <DataTable
          :columns="columns"
          :data="teams"
          :loading="loading"
          :actions-count="2"
          :server-side-sort="false"
        >
          <template #cell-name="{ row }">
            <div class="flex flex-col">
              <span class="font-medium text-gray-900 dark:text-white">{{ row.name }}</span>
              <span class="text-xs text-gray-400 dark:text-dark-500">{{ row.slug }}</span>
            </div>
          </template>

          <template #cell-owner="{ row }">
            <span class="text-sm text-gray-700 dark:text-gray-300">
              {{ row.owner?.email || '-' }}
            </span>
          </template>

          <template #cell-status="{ value }">
            <div class="flex items-center gap-1.5">
              <span
                :class="[
                  'inline-block h-2 w-2 rounded-full',
                  value === 'active' ? 'bg-green-500' : 'bg-red-500'
                ]"
              ></span>
              <span class="text-sm text-gray-700 dark:text-gray-300">
                {{ value === 'active' ? t('common.active') : t('admin.teams.disabled') }}
              </span>
            </div>
          </template>

          <template #cell-member_count="{ value }">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ value }}</span>
          </template>

          <template #cell-balance="{ value }">
            <span class="font-medium text-gray-900 dark:text-white">${{ Number(value).toFixed(2) }}</span>
          </template>

          <template #cell-concurrency="{ value }">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ value === 0 ? t('admin.teams.unlimited') : value }}</span>
          </template>
          <template #cell-rpm_limit="{ value }">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ value === 0 ? t('admin.teams.unlimited') : value }}</span>
          </template>

          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button
                @click="openDetail(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
              >
                <Icon name="users" size="sm" />
                <span class="text-xs">{{ t('admin.teams.manage') }}</span>
              </button>
              <button
                @click="handleToggleTeamStatus(row)"
                :class="[
                  'flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors',
                  row.status === 'active'
                    ? 'hover:bg-orange-50 hover:text-orange-600 dark:hover:bg-orange-900/20 dark:hover:text-orange-400'
                    : 'hover:bg-green-50 hover:text-green-600 dark:hover:bg-green-900/20 dark:hover:text-green-400'
                ]"
              >
                <Icon v-if="row.status === 'active'" name="ban" size="sm" />
                <Icon v-else name="checkCircle" size="sm" />
                <span class="text-xs">{{ row.status === 'active' ? t('admin.teams.disable') : t('admin.teams.enable') }}</span>
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.teams.noTeams')"
              :description="t('admin.teams.noTeamsDescription')"
            />
          </template>
        </DataTable>
      </template>

      <!-- Pagination -->
      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <!-- Team detail modal -->
    <BaseDialog
      :show="showDetail"
      :title="detailTeam ? detailTeam.name : t('admin.teams.detailTitle')"
      width="wide"
      @close="closeDetail"
    >
      <div v-if="detailLoading" class="flex justify-center py-12">
        <LoadingSpinner />
      </div>

      <div v-else-if="detailTeam" class="space-y-6">
        <!-- Summary -->
        <div class="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <div>
            <div class="text-xs uppercase text-gray-400 dark:text-dark-500">{{ t('admin.teams.owner') }}</div>
            <div class="mt-1 text-sm text-gray-900 dark:text-white">{{ detailTeam.owner?.email || '-' }}</div>
          </div>
          <div>
            <div class="text-xs uppercase text-gray-400 dark:text-dark-500">{{ t('admin.teams.status') }}</div>
            <div class="mt-1 text-sm text-gray-900 dark:text-white">
              {{ detailTeam.status === 'active' ? t('common.active') : t('admin.teams.disabled') }}
            </div>
          </div>
          <div>
            <div class="text-xs uppercase text-gray-400 dark:text-dark-500">{{ t('admin.teams.members') }}</div>
            <div class="mt-1 text-sm text-gray-900 dark:text-white">{{ detailTeam.member_count }}</div>
          </div>
          <div>
            <div class="text-xs uppercase text-gray-400 dark:text-dark-500">{{ t('admin.teams.balance') }}</div>
            <div class="mt-1 text-sm text-gray-900 dark:text-white">${{ Number(detailTeam.balance).toFixed(2) }}</div>
          </div>
        </div>

        <!-- Add member -->
        <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
          <h4 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.teams.addMember') }}</h4>
          <div class="flex flex-col gap-3 sm:flex-row sm:items-end">
            <div class="flex-1">
              <label class="mb-1 block text-xs text-gray-500 dark:text-dark-400">{{ t('admin.teams.addMemberInput') }}</label>
              <input
                v-model="addMemberInput"
                type="text"
                :placeholder="t('admin.teams.addMemberPlaceholder')"
                class="input w-full"
                @keyup.enter="handleAddMember"
              />
            </div>
            <div class="w-full sm:w-40">
              <label class="mb-1 block text-xs text-gray-500 dark:text-dark-400">{{ t('admin.teams.role') }}</label>
              <Select v-model="addMemberRole" :options="assignableRoleOptions" />
            </div>
            <button
              type="button"
              class="btn btn-primary"
              :disabled="addMemberSubmitting || !addMemberInput.trim()"
              @click="handleAddMember"
            >
              <Icon name="userPlus" size="sm" class="mr-1.5" />
              {{ t('admin.teams.addMember') }}
            </button>
          </div>
        </div>

        <!-- Members table -->
        <div>
          <h4 class="mb-2 text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.teams.members') }}</h4>
          <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
            <table class="min-w-full divide-y divide-gray-100 dark:divide-dark-700">
              <thead class="bg-gray-50 dark:bg-dark-800">
                <tr>
                  <th class="px-4 py-2 text-left text-xs font-medium uppercase text-gray-500">{{ t('common.user') }}</th>
                  <th class="px-4 py-2 text-left text-xs font-medium uppercase text-gray-500">{{ t('admin.teams.role') }}</th>
                  <th class="px-4 py-2 text-left text-xs font-medium uppercase text-gray-500">{{ t('admin.teams.status') }}</th>
                  <th class="px-4 py-2 text-right text-xs font-medium uppercase text-gray-500">{{ t('admin.teams.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900">
                <tr v-for="member in members" :key="member.id">
                  <td class="px-4 py-3">
                    <div class="font-medium text-gray-900 dark:text-white">
                      {{ member.user?.username || member.user?.email || member.user_id }}
                    </div>
                    <div class="text-xs text-gray-500">{{ member.user?.email }}</div>
                  </td>
                  <td class="px-4 py-3">
                    <Select
                      v-if="member.role !== 'owner'"
                      :model-value="member.role"
                      :options="assignableRoleOptions"
                      class="w-32"
                      @update:model-value="(val) => handleChangeRole(member, String(val))"
                    />
                    <span v-else class="badge badge-purple">{{ t('admin.teams.roles.owner') }}</span>
                  </td>
                  <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-300">
                    {{ t('admin.teams.memberStatus.' + member.status) }}
                  </td>
                  <td class="px-4 py-3">
                    <div v-if="member.role !== 'owner'" class="flex items-center justify-end gap-1">
                      <button
                        v-if="member.status !== 'left'"
                        @click="handleToggleMemberStatus(member)"
                        class="rounded px-2 py-1 text-xs font-medium transition-colors"
                        :class="member.status === 'active'
                          ? 'text-orange-600 hover:bg-orange-50 dark:text-orange-400 dark:hover:bg-orange-900/20'
                          : 'text-green-600 hover:bg-green-50 dark:text-green-400 dark:hover:bg-green-900/20'"
                      >
                        {{ member.status === 'active' ? t('admin.teams.suspend') : t('admin.teams.activate') }}
                      </button>
                      <button
                        @click="handleRemoveMember(member)"
                        class="rounded px-2 py-1 text-xs font-medium text-red-600 transition-colors hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
                      >
                        {{ t('admin.teams.remove') }}
                      </button>
                    </div>
                    <span v-else class="block text-right text-xs text-gray-400">-</span>
                  </td>
                </tr>
                <tr v-if="members.length === 0">
                  <td colspan="4" class="px-4 py-8 text-center text-sm text-gray-500">{{ t('common.noData') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- Pending invitations -->
        <div>
          <h4 class="mb-2 text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.teams.pendingInvitations') }}</h4>
          <div class="divide-y divide-gray-100 rounded-lg border border-gray-200 dark:divide-dark-700 dark:border-dark-700">
            <div
              v-for="invitation in invitations"
              :key="invitation.id"
              class="flex items-center justify-between px-4 py-3"
            >
              <div>
                <div class="font-medium text-gray-900 dark:text-white">{{ invitation.email }}</div>
                <div class="text-xs text-gray-500">
                  {{ t('admin.teams.roles.' + invitation.role) }} · {{ invitation.status }}
                </div>
              </div>
              <div class="text-xs text-gray-500">{{ formatDateTime(invitation.expires_at) }}</div>
            </div>
            <div v-if="invitations.length === 0" class="px-4 py-8 text-center text-sm text-gray-500">
              {{ t('common.noData') }}
            </div>
          </div>
        </div>
      </div>

      <template #footer>
        <div class="flex justify-end">
          <button type="button" class="btn btn-secondary" @click="closeDetail">{{ t('common.close') }}</button>
        </div>
      </template>
    </BaseDialog>

    <!-- Create team modal -->
    <BaseDialog
      :show="showCreate"
      :title="t('admin.teams.createTitle')"
      width="normal"
      @close="closeCreate"
    >
      <form id="create-team-form" @submit.prevent="handleCreate" class="space-y-4">
        <div>
          <label class="input-label" for="create-team-name">{{ t('admin.teams.teamName') }}</label>
          <input
            id="create-team-name"
            v-model="createForm.name"
            type="text"
            required
            maxlength="100"
            class="input"
            :placeholder="t('admin.teams.teamNamePlaceholder')"
          />
        </div>
        <div>
          <label class="input-label" for="create-team-owner">{{ t('admin.teams.ownerLabel') }}</label>
          <input
            id="create-team-owner"
            v-model="createForm.owner"
            type="text"
            required
            class="input"
            :placeholder="t('admin.teams.ownerPlaceholder')"
          />
        </div>
        <div>
          <label class="input-label" for="create-team-concurrency">{{ t('admin.teams.concurrency') }}</label>
          <input id="create-team-concurrency" v-model.number="createForm.concurrency" type="number" min="0" class="input" />
          <p class="mt-1 text-xs text-gray-400">{{ t('admin.teams.concurrencyHint') }}</p>
        </div>
        <div>
          <label class="input-label" for="create-team-rpm">{{ t('admin.teams.rpmLimit') }}</label>
          <input id="create-team-rpm" v-model.number="createForm.rpm_limit" type="number" min="0" class="input" />
          <p class="mt-1 text-xs text-gray-400">{{ t('admin.teams.rpmLimitHint') }}</p>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeCreate">
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="create-team-form"
            class="btn btn-primary"
            :disabled="createSubmitting || !createForm.name.trim() || !createForm.owner.trim()"
          >
            <svg
              v-if="createSubmitting"
              class="-ml-1 mr-2 h-4 w-4 animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            {{ t('common.create') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Remove member confirmation -->
    <ConfirmDialog
      :show="showRemoveConfirm"
      :title="t('admin.teams.remove')"
      :message="t('admin.teams.removeConfirm', { user: removingMember?.user?.email || removingMember?.user_id })"
      :danger="true"
      @confirm="confirmRemoveMember"
      @cancel="showRemoveConfirm = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { formatDateTime } from '@/utils/format'
import { adminTeamsAPI, type AdminAssignableRole } from '@/api/admin/teams'
import type { AdminTeam, AdminTeamMember, TeamInvitation } from '@/types'
import type { Column } from '@/components/common/types'
import Icon from '@/components/icons/Icon.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import EmptyState from '@/components/common/EmptyState.vue'

const { t } = useI18n()
const appStore = useAppStore()

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.teams.columns.name'), sortable: false },
  { key: 'owner', label: t('admin.teams.columns.owner'), sortable: false },
  { key: 'status', label: t('admin.teams.columns.status'), sortable: false },
  { key: 'member_count', label: t('admin.teams.columns.members'), sortable: false },
  { key: 'balance', label: t('admin.teams.columns.balance'), sortable: false },
  { key: 'concurrency', label: t('admin.teams.columns.concurrency'), sortable: false },
  { key: 'rpm_limit', label: t('admin.teams.columns.rpmLimit'), sortable: false },
  { key: 'created_at', label: t('admin.teams.columns.created'), sortable: false },
  { key: 'actions', label: t('admin.teams.columns.actions'), sortable: false }
])

// Roles assignable via the admin API (owner excluded).
const assignableRoleOptions = computed(() =>
  (['admin', 'billing', 'developer', 'viewer'] as AdminAssignableRole[]).map((role) => ({
    value: role,
    label: t('admin.teams.roles.' + role)
  }))
)

const teams = ref<AdminTeam[]>([])
const loading = ref(false)
const searchQuery = ref('')
const statusFilter = ref('')

const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})

let abortController: AbortController | null = null

const loadTeams = async () => {
  abortController?.abort()
  const currentAbortController = new AbortController()
  abortController = currentAbortController
  const { signal } = currentAbortController
  loading.value = true
  try {
    const response = await adminTeamsAPI.list(
      {
        page: pagination.page,
        page_size: pagination.page_size,
        search: searchQuery.value || undefined,
        status: (statusFilter.value || undefined) as 'active' | 'disabled' | undefined
      },
      { signal }
    )
    if (signal.aborted) return
    teams.value = response.items
    pagination.total = response.total
    pagination.pages = response.pages
  } catch (error: any) {
    const errorInfo = error as { name?: string; code?: string }
    if (
      errorInfo?.name === 'AbortError' ||
      errorInfo?.name === 'CanceledError' ||
      errorInfo?.code === 'ERR_CANCELED'
    ) {
      return
    }
    appStore.showError(error?.message || t('admin.teams.failedToLoad'))
    console.error('Error loading teams:', error)
  } finally {
    if (abortController === currentAbortController) {
      loading.value = false
    }
  }
}

let searchTimeout: ReturnType<typeof setTimeout>
const handleSearch = () => {
  clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    pagination.page = 1
    loadTeams()
  }, 300)
}

const applyFilter = () => {
  pagination.page = 1
  loadTeams()
}

const handlePageChange = (page: number) => {
  pagination.page = Math.max(1, Math.min(page, pagination.pages || 1))
  loadTeams()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize
  pagination.page = 1
  loadTeams()
}

const handleToggleTeamStatus = async (team: AdminTeam) => {
  const newStatus = team.status === 'active' ? 'disabled' : 'active'
  try {
    await adminTeamsAPI.update(team.id, { status: newStatus })
    appStore.showSuccess(
      newStatus === 'active' ? t('admin.teams.teamEnabled') : t('admin.teams.teamDisabled')
    )
    loadTeams()
    if (detailTeam.value?.id === team.id) {
      detailTeam.value = { ...detailTeam.value, status: newStatus }
    }
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.teams.failedToUpdate'))
    console.error('Error toggling team status:', error)
  }
}

// ==================== Create modal ====================

const showCreate = ref(false)
const createSubmitting = ref(false)
const createForm = reactive({ name: '', owner: '', concurrency: 5, rpm_limit: 0 })

const openCreate = () => {
  createForm.name = ''
  createForm.owner = ''
  showCreate.value = true
}

const closeCreate = () => {
  showCreate.value = false
  createForm.name = ''
  createForm.owner = ''
}

// Owner accepts either a numeric user id or an email address.
const handleCreate = async () => {
  const name = createForm.name.trim()
  const owner = createForm.owner.trim()
  if (!name) {
    appStore.showError(t('admin.teams.nameRequired'))
    return
  }
  if (!owner) {
    appStore.showError(t('admin.teams.ownerRequired'))
    return
  }
  const payload: { name: string; owner_user_id?: number; owner_email?: string; concurrency?: number; rpm_limit?: number } = { name }
  if (/^\d+$/.test(owner)) {
    payload.owner_user_id = Number(owner)
  } else {
    payload.owner_email = owner
  }
  payload.concurrency = createForm.concurrency
  payload.rpm_limit = createForm.rpm_limit
  createSubmitting.value = true
  try {
    await adminTeamsAPI.create(payload)
    appStore.showSuccess(t('admin.teams.createSuccess'))
    closeCreate()
    loadTeams()
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.teams.failedToCreate'))
    console.error('Error creating team:', error)
  } finally {
    createSubmitting.value = false
  }
}

// ==================== Detail modal ====================

const showDetail = ref(false)
const detailLoading = ref(false)
const detailTeam = ref<AdminTeam | null>(null)
const members = ref<AdminTeamMember[]>([])
const invitations = ref<TeamInvitation[]>([])

const addMemberInput = ref('')
const addMemberRole = ref<AdminAssignableRole>('developer')
const addMemberSubmitting = ref(false)

const loadDetail = async (teamId: number) => {
  detailLoading.value = true
  try {
    const data = await adminTeamsAPI.get(teamId)
    detailTeam.value = data.team
    members.value = data.members
    invitations.value = data.invitations
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.teams.failedToLoad'))
    console.error('Error loading team detail:', error)
  } finally {
    detailLoading.value = false
  }
}

const openDetail = (team: AdminTeam) => {
  detailTeam.value = team
  members.value = []
  invitations.value = []
  addMemberInput.value = ''
  addMemberRole.value = 'developer'
  showDetail.value = true
  loadDetail(team.id)
}

const closeDetail = () => {
  showDetail.value = false
  detailTeam.value = null
  members.value = []
  invitations.value = []
}

const refreshDetailAndList = async () => {
  if (detailTeam.value) await loadDetail(detailTeam.value.id)
  loadTeams()
}

// Add member accepts either a numeric user id or an email address.
const handleAddMember = async () => {
  if (!detailTeam.value) return
  const raw = addMemberInput.value.trim()
  if (!raw) return
  const payload: { user_id?: number; email?: string; role: AdminAssignableRole } = {
    role: addMemberRole.value
  }
  if (/^\d+$/.test(raw)) {
    payload.user_id = Number(raw)
  } else {
    payload.email = raw
  }
  addMemberSubmitting.value = true
  try {
    await adminTeamsAPI.addMember(detailTeam.value.id, payload)
    appStore.showSuccess(t('admin.teams.memberAdded'))
    addMemberInput.value = ''
    await refreshDetailAndList()
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.teams.failedToAddMember'))
    console.error('Error adding team member:', error)
  } finally {
    addMemberSubmitting.value = false
  }
}

const handleChangeRole = async (member: AdminTeamMember, role: string) => {
  if (!detailTeam.value || role === member.role) return
  try {
    await adminTeamsAPI.updateMember(detailTeam.value.id, member.user_id, {
      role: role as AdminAssignableRole
    })
    appStore.showSuccess(t('admin.teams.memberUpdated'))
    await refreshDetailAndList()
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.teams.failedToUpdateMember'))
    console.error('Error updating member role:', error)
  }
}

const handleToggleMemberStatus = async (member: AdminTeamMember) => {
  if (!detailTeam.value) return
  const newStatus = member.status === 'active' ? 'suspended' : 'active'
  try {
    await adminTeamsAPI.updateMember(detailTeam.value.id, member.user_id, { status: newStatus })
    appStore.showSuccess(t('admin.teams.memberUpdated'))
    await refreshDetailAndList()
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.teams.failedToUpdateMember'))
    console.error('Error updating member status:', error)
  }
}

// Remove member (with confirmation)
const showRemoveConfirm = ref(false)
const removingMember = ref<AdminTeamMember | null>(null)

const handleRemoveMember = (member: AdminTeamMember) => {
  removingMember.value = member
  showRemoveConfirm.value = true
}

const confirmRemoveMember = async () => {
  if (!detailTeam.value || !removingMember.value) return
  try {
    await adminTeamsAPI.removeMember(detailTeam.value.id, removingMember.value.user_id)
    appStore.showSuccess(t('admin.teams.memberRemoved'))
    showRemoveConfirm.value = false
    removingMember.value = null
    await refreshDetailAndList()
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.teams.failedToRemoveMember'))
    console.error('Error removing team member:', error)
  }
}

onMounted(() => {
  loadTeams()
})
</script>
