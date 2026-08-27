<!--
  APEXONE-EXT: 共享模式下的顶部三个数。

  为什么是这三个：供给者打开控制台只想知道三件事——能拿到手的有多少、一共赚了多少、
  我的号还在不在池子里。消费侧那四个数（余额/密钥数/今日请求/今日消费）对他一个
  都不成立，摆在那里只会让他以为自己进错了页面。

  第二个数**不是**「本月产出」：后端没有按月聚合的供给侧接口，为了一个卡片标题去
  编一个数（比如拿累计当本月、或者前端自己按流水凑）比不显示更糟——它会被当成
  真数字用来算收入预期。所以这里显示的是后端真实提供的「累计收益」，标签也照实写。
-->
<template>
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-3" data-testid="supply-dashboard-stats">
    <div class="card p-4">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-emerald-100 p-2 dark:bg-emerald-900/30">
          <Icon name="dollar" size="md" class="text-emerald-600 dark:text-emerald-400" :stroke-width="2" />
        </div>
        <div class="min-w-0">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('supply.console.available') }}</p>
          <p class="text-xl font-bold text-emerald-600 dark:text-emerald-400" data-testid="supply-stat-available">
            {{ formatCurrency(wallet?.available_credit ?? 0) }}
          </p>
          <p class="truncate text-xs text-gray-500 dark:text-gray-400">{{ t('supply.console.availableHint') }}</p>
        </div>
      </div>
    </div>

    <div class="card p-4">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-purple-100 p-2 dark:bg-purple-900/30">
          <Icon name="chart" size="md" class="text-purple-600 dark:text-purple-400" :stroke-width="2" />
        </div>
        <div class="min-w-0">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('supply.console.history') }}</p>
          <p class="text-xl font-bold text-gray-900 dark:text-white" data-testid="supply-stat-history">
            {{ formatCurrency(wallet?.history_credit ?? 0) }}
          </p>
          <!-- 冻结中的钱写在这里而不是单独一张卡：它回答的是「累计和可提为什么对不上」，
               离开那两个数它自己没有意义。 -->
          <p class="truncate text-xs text-gray-500 dark:text-gray-400">
            {{ t('supply.console.frozenHint', { amount: formatCurrency(wallet?.frozen_credit ?? 0) }) }}
          </p>
        </div>
      </div>
    </div>

    <div class="card p-4">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-blue-100 p-2 dark:bg-blue-900/30">
          <Icon name="link" size="md" class="text-blue-600 dark:text-blue-400" :stroke-width="2" />
        </div>
        <div class="min-w-0">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('supply.console.accounts') }}</p>
          <p class="text-xl font-bold text-gray-900 dark:text-white" data-testid="supply-stat-accounts">
            {{ accountCount }}
          </p>
          <!-- 挂着 ≠ 在接单：观察期里的号一个请求都不会接到。两个数分开说，
               免得有人对着「3 个账号 / 0 收益」以为结算坏了。 -->
          <p class="truncate text-xs text-gray-500 dark:text-gray-400">
            {{ t('supply.console.schedulableHint', { count: schedulableCount }) }}
          </p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { SupplyWallet } from '@/api/supply'
import { formatCurrency } from '@/utils/format'

defineProps<{
  wallet: SupplyWallet | null
  accountCount: number
  schedulableCount: number
}>()

const { t } = useI18n()
</script>
