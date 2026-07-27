<template>
  <img :src="iconSrc" :alt="type" class="inline-block h-[1.05em] w-[1.05em] shrink-0 align-[-0.15em]" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import checkIcon from '@/assets/icons/check.svg'
import crossIcon from '@/assets/icons/cross.svg'
import warnIcon from '@/assets/icons/warn.svg'

const props = defineProps<{
  type: 'check' | 'cross' | 'warn'
}>()

const icons = {
  check: checkIcon,
  cross: crossIcon,
  warn: warnIcon
} as const

const iconSrc = computed(() => icons[props.type])
</script>
