<template>
  <BaseDialog :show="show" :title="t('admin.teams.balanceHistoryTitle')" width="wide" :z-index="40" @close="$emit('close')">
    <div v-if="team" class="space-y-4">
      <div class="flex items-center justify-between rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="min-w-0">
          <p class="truncate font-medium text-gray-900 dark:text-white">{{ team.name }}</p>
        </div>
        <div class="flex-shrink-0 text-right">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.teams.currentBalance') }}</p>
          <p class="text-xl font-bold text-gray-900 dark:text-white">${{ Number(team.balance).toFixed(2) }}</p>
        </div>
      </div>

      <div v-if="loading" class="flex justify-center py-8">
        <svg class="h-8 w-8 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" /><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" /></svg>
      </div>
      <div v-else-if="history.length === 0" class="py-8 text-center"><p class="text-sm text-gray-500">{{ t('admin.teams.noBalanceHistory') }}</p></div>
      <div v-else class="max-h-[28rem] space-y-3 overflow-y-auto">
        <div v-for="item in history" :key="item.id" class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800">
          <div class="flex items-start justify-between">
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-white">{{ item.value >= 0 ? t('redeem.balanceAddedAdmin') : t('redeem.balanceDeductedAdmin') }}</p>
              <p v-if="item.notes" class="mt-0.5 text-xs text-gray-500 dark:text-dark-400" :title="item.notes">{{ item.notes.length > 60 ? item.notes.substring(0, 55) + '...' : item.notes }}</p>
              <p class="mt-0.5 text-xs text-gray-400 dark:text-dark-500">{{ formatDateTime(item.used_at || item.created_at) }}</p>
            </div>
            <p :class="['text-sm font-semibold', item.value >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400']">{{ item.value >= 0 ? '+' : '' }}${{ item.value.toFixed(2) }}</p>
          </div>
        </div>
      </div>

      <div v-if="totalPages > 1" class="flex items-center justify-center gap-2 pt-2">
        <button :disabled="currentPage <= 1" class="btn btn-secondary px-3 py-1 text-sm" @click="load(currentPage - 1)">{{ t('pagination.previous') }}</button>
        <span class="text-sm text-gray-500 dark:text-dark-400">{{ currentPage }} / {{ totalPages }}</span>
        <button :disabled="currentPage >= totalPages" class="btn btn-secondary px-3 py-1 text-sm" @click="load(currentPage + 1)">{{ t('pagination.next') }}</button>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminTeamsAPI, type TeamBalanceHistoryItem } from '@/api/admin/teams'
import { formatDateTime } from '@/utils/format'
import type { AdminTeam } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{ show: boolean; team: AdminTeam | null }>()
defineEmits(['close'])
const { t } = useI18n()

const history = ref<TeamBalanceHistoryItem[]>([])
const loading = ref(false)
const currentPage = ref(1)
const total = ref(0)
const pageSize = 15
const totalPages = computed(() => Math.ceil(total.value / pageSize) || 1)

watch(() => props.show, (v) => { if (v && props.team) load(1) })

const load = async (page: number) => {
  if (!props.team) return
  loading.value = true
  currentPage.value = page
  try {
    const res = await adminTeamsAPI.getTeamBalanceHistory(props.team.id, page, pageSize)
    history.value = res.items || []
    total.value = res.total || 0
  } catch (e) {
    console.error('Failed to load team balance history:', e)
  } finally {
    loading.value = false
  }
}
</script>
