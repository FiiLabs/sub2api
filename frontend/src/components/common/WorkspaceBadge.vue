<template>
  <span
    :class="[
      'flex items-center gap-1.5 min-w-0',
      sizeText,
      variant === 'chip' ? ['rounded-lg px-2 py-1', palette.chip] : ''
    ]"
  >
    <Icon
      :name="icon"
      :size="iconSize"
      class="flex-shrink-0"
      :class="variant === 'plain' ? palette.icon : ''"
    />
    <span
      v-if="!compact"
      :class="['inline-flex min-w-0 items-center gap-1.5', responsiveCompact ? 'hidden sm:inline-flex' : '']"
    >
      <span class="truncate font-medium">{{ name }}</span>
      <span v-if="showType" class="truncate opacity-70">· {{ typeLabel }}</span>
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { useWorkspaceIdentity } from '@/composables/useWorkspaceIdentity'
import type { WorkspaceSubject } from '@/types'

const props = withDefaults(
  defineProps<{
    subject?: WorkspaceSubject | null
    showType?: boolean
    variant?: 'chip' | 'plain'
    compact?: boolean
    responsiveCompact?: boolean
    size?: 'sm' | 'md'
  }>(),
  { showType: false, variant: 'chip', compact: false, responsiveCompact: false, size: 'md' }
)

const { icon, name, typeLabel, palette } = useWorkspaceIdentity(() => props.subject)
const sizeText = computed(() => (props.size === 'sm' ? 'text-xs' : 'text-sm'))
const iconSize = computed<'sm' | 'md'>(() => (props.size === 'sm' ? 'sm' : 'md'))
</script>
