<!--
  APEXONE-EXT: 共享模式下的主操作。

  消费侧那几个（建密钥 / 看用量 / 批量出图）对供给者一个都用不上。这里只留两件
  他真正会做的事：再挂一个号，和把赚到的钱取出来。

  两个按钮都落到 /supply：提现表单就在那一页上，而它藏在两次异步加载之后
  （状态 → 钱包/渠道），此刻用锚点滚过去只会滚到一个还没渲染出来的位置。
  与其做一个时灵时不灵的锚点，不如老实把人送到那一页——他到了就能看见。
-->
<template>
  <div class="card" data-testid="supply-quick-actions">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('supply.console.actionsTitle') }}</h2>
    </div>
    <div class="space-y-3 p-4">
      <button
        class="group flex w-full items-center gap-4 rounded-xl bg-gray-50 p-4 text-left transition-all duration-200 hover:bg-gray-100 dark:bg-dark-800/50 dark:hover:bg-dark-800"
        data-testid="supply-action-connect"
        @click="router.push('/supply')"
      >
        <div class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-xl bg-primary-100 transition-transform group-hover:scale-105 dark:bg-primary-900/30">
          <Icon name="link" size="lg" class="text-primary-600 dark:text-primary-400" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('supply.console.connect') }}</p>
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('supply.console.connectDesc') }}</p>
        </div>
        <Icon name="chevronRight" size="md" class="text-gray-400 transition-colors group-hover:text-primary-500 dark:text-dark-500" />
      </button>

      <button
        class="group flex w-full items-center gap-4 rounded-xl bg-gray-50 p-4 text-left transition-all duration-200 hover:bg-gray-100 dark:bg-dark-800/50 dark:hover:bg-dark-800"
        data-testid="supply-action-withdraw"
        @click="router.push('/supply')"
      >
        <div class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-xl bg-emerald-100 transition-transform group-hover:scale-105 dark:bg-emerald-900/30">
          <Icon name="dollar" size="lg" class="text-emerald-600 dark:text-emerald-400" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('supply.console.withdraw') }}</p>
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('supply.console.withdrawDesc') }}</p>
        </div>
        <Icon name="chevronRight" size="md" class="text-gray-400 transition-colors group-hover:text-emerald-500 dark:text-dark-500" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const router = useRouter()
const { t } = useI18n()
</script>
