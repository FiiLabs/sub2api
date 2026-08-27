<!--
  APEXONE-EXT: 双边市场——控制台模式切换器。

  为什么常驻在页面顶部、而不是收进设置菜单：同时供给又消费的人是存在的（把闲置额度
  挂上来、再用赚到的分成去调用），对他们来说这不是一次性配置，是每天要来回切的东西。
  藏进设置里等于让他每次多点三下；更糟的是，自动判定万一判错（比如一个刚接入完
  就想回去看用量的人），他会以为控制台变了个样子回不去了。
-->
<template>
  <div class="flex items-center gap-2" data-testid="console-mode-switch">
    <span class="text-xs text-gray-400 dark:text-dark-500">{{ t('supply.console.modeLabel') }}</span>
    <div class="inline-flex rounded-lg border border-gray-200 bg-gray-50 p-0.5 dark:border-dark-700 dark:bg-dark-800/60">
      <button
        v-for="option in options"
        :key="option.value"
        type="button"
        class="rounded-md px-3 py-1.5 text-xs font-medium transition-colors"
        :class="
          option.value === mode
            ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700 dark:text-primary-400'
            : 'text-gray-500 hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200'
        "
        :aria-pressed="option.value === mode"
        :data-testid="`console-mode-${option.value}`"
        @click="emit('update:mode', option.value)"
      >
        {{ option.label }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConsoleMode } from '@/stores/supply'

defineProps<{ mode: ConsoleMode }>()
const emit = defineEmits<{ (e: 'update:mode', value: ConsoleMode): void }>()

const { t } = useI18n()

const options = computed((): { value: ConsoleMode; label: string }[] => [
  { value: 'usage', label: t('supply.console.usageMode') },
  { value: 'sharing', label: t('supply.console.sharingMode') },
])
</script>
