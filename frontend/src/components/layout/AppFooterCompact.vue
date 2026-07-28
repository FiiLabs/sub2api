<template>
  <footer
    class="flex-shrink-0 border-t border-gray-200/60 px-4 py-3 dark:border-dark-800 sm:px-6 lg:px-8"
  >
    <div class="flex flex-col items-center justify-between gap-2 sm:flex-row">
      <!-- 版权 -->
      <span class="text-xs text-gray-500 dark:text-dark-400">
        © {{ currentYear }} ApexOne · All rights reserved
      </span>

      <!-- 联系方式 + 社区 -->
      <div class="flex items-center gap-3">
        <a
          :href="`mailto:${supportEmail}`"
          class="flex items-center gap-1.5 text-xs text-gray-500 transition-colors hover:text-primary-600 dark:text-dark-400 dark:hover:text-primary-400"
          :aria-label="`Email ${supportEmail}`"
        >
          <Icon name="mail" size="sm" class="flex-shrink-0" />
          <span class="hidden sm:inline">{{ supportEmail }}</span>
        </a>

        <span class="h-3 w-px bg-gray-300 dark:bg-dark-700" aria-hidden="true"></span>

        <a
          v-for="item in socialLinks"
          :key="item.label"
          :href="item.href"
          target="_blank"
          rel="external noreferrer"
          class="text-gray-400 transition-colors hover:text-primary-600 dark:text-dark-500 dark:hover:text-primary-400"
          :aria-label="`${item.label} link`"
        >
          <svg class="size-4" viewBox="0 0 44 44" aria-hidden="true" focusable="false">
            <path fill="currentColor" :d="item.path" />
          </svg>
        </a>
      </div>
    </div>
  </footer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { supportEmail, socialLinks } from './footerLinks'

const currentYear = computed(() => new Date().getFullYear())
</script>
