<template>
  <div
    v-if="isTeam"
    aria-hidden="true"
    class="pointer-events-none fixed inset-0 z-20 select-none overflow-hidden"
    style="opacity: 0.05"
  >
    <div
      class="absolute left-1/2 top-1/2 flex -translate-x-1/2 -translate-y-1/2 -rotate-[20deg] flex-col gap-6 whitespace-nowrap"
    >
      <div
        v-for="row in 12"
        :key="row"
        :class="['text-2xl font-medium tracking-wide', palette.watermarkText]"
      >
        <span v-for="col in 8" :key="col" class="mr-10">{{ text }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useWorkspaceIdentity } from '@/composables/useWorkspaceIdentity'

const { t } = useI18n()
const { isTeam, name, palette } = useWorkspaceIdentity()
const text = computed(() => `${name.value} · ${t('workspace.team')}`)
</script>
