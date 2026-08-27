<!--
  APEXONE-EXT: 控制台共享模式下的精简引导。

  与 /supply 上那张完整版共用同一套文案键（supply.guide.*）。不另写一套，是因为
  同一个流程用两种说法讲，第二次读到的人会以为流程变了——而这两处恰恰是同一个人
  在同一天里会先后看到的。

  这里只留三步的**标题**：控制台是概览，真正要动手的表单在 /supply，所以这张卡
  唯一的终点是那个按钮。步骤的解释留给他到了那一页再读，此刻讲细节只会让他
  在一个动不了手的地方读完又要重读一遍。

  "有没有账号"的判断放在父组件而不是这里：账号数要等接口回来才作数，
  在组件内拿 0 当初值判断，会让已经接入过的人先闪一下这张卡。
-->
<template>
  <div class="card p-6" data-testid="supply-guide-card">
    <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supply.guide.title') }}</h2>
    <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('supply.guide.subtitle') }}</p>

    <ol class="mt-4 flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:gap-x-8">
      <li
        v-for="step in guideSteps"
        :key="step.n"
        class="flex items-center gap-2"
        :data-testid="`supply-guide-step-${step.n}`"
      >
        <span
          class="flex h-6 w-6 flex-none items-center justify-center rounded-full bg-primary-100 text-xs font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
        >
          {{ step.n }}
        </span>
        <span class="text-sm text-gray-700 dark:text-gray-300">{{ t(step.titleKey) }}</span>
      </li>
    </ol>

    <!-- 精简版**也**带这一句。它是最容易在"精简"时被第一个删掉的东西——
         正因为如此，它必须留在两处：一个只在完整页出现的免责，等于默认
         大多数人不会读到它。 -->
    <p class="mt-4 text-sm text-gray-500 dark:text-dark-400" data-testid="supply-guide-disclaimer">
      {{ t('supply.guide.after3') }}
    </p>

    <button class="btn btn-primary mt-4" data-testid="supply-guide-cta" @click="router.push('/supply')">
      <Icon name="link" size="sm" />
      <span>{{ t('supply.guide.cta') }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { SUPPLY_GUIDE_STEPS } from '@/constants/supplyGuide'

const router = useRouter()
const { t } = useI18n()
const guideSteps = SUPPLY_GUIDE_STEPS
</script>
