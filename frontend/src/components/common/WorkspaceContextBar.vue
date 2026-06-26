<template>
  <div
    v-if="subject"
    :class="['flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border px-3 py-2', palette.chip, palette.border]"
  >
    <span class="text-xs opacity-70">{{ t('workspace.youAreIn') }}</span>
    <WorkspaceBadge :subject="subject" show-type variant="plain" size="sm" />
    <template v-if="showBalance">
      <span class="text-xs opacity-50">|</span>
      <span class="text-xs font-medium">{{ t('common.balance') }} ${{ balance.toFixed(2) }}</span>
    </template>
    <template v-if="showRole && isTeam">
      <span class="text-xs opacity-50">|</span>
      <span class="text-xs font-medium">{{ t('workspace.roleLabel') }} {{ t('workspace.roles.' + role) }}</span>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useWorkspaceStore } from '@/stores/workspaces'
import { useWorkspaceIdentity } from '@/composables/useWorkspaceIdentity'
import WorkspaceBadge from './WorkspaceBadge.vue'

withDefaults(defineProps<{ showBalance?: boolean; showRole?: boolean }>(), {
  showBalance: true,
  showRole: true
})

const { t } = useI18n()
const store = useWorkspaceStore()
const { subject, isTeam, palette } = useWorkspaceIdentity()
const balance = computed(() => {
  const b = store.activeWorkspace?.balance ?? 0
  return Number.isFinite(b) ? b : 0
})
const role = computed(() => store.activeWorkspace?.role ?? 'viewer')
</script>
