<template>
  <div class="min-h-screen bg-gray-50 pt-16 text-gray-900 dark:bg-dark-950 dark:text-white md:pt-[72px]">
    <Header />

    <main class="mx-auto max-w-5xl px-4 py-10 sm:px-6">
      <!-- 未开启 / 拉取失败：优雅空状态，不破图不报错 -->
      <section
        v-if="!ready"
        class="flex min-h-[50vh] flex-col items-center justify-center text-center"
        data-testid="stats-empty"
      >
        <span class="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-primary-50 text-2xl dark:bg-primary-500/10">📊</span>
        <h1 class="text-fluid-xl font-bold tracking-tight text-gray-900 dark:text-white">{{ t('statsPage.empty.title') }}</h1>
        <p class="mt-2 max-w-md text-fluid-sm text-gray-500 dark:text-dark-400">{{ t('statsPage.empty.desc') }}</p>
        <router-link to="/home" class="btn btn-secondary mt-6">{{ t('statsPage.cta.home') }}</router-link>
      </section>

      <template v-else>
        <!-- 标题 -->
        <section class="mb-10 text-center">
          <span :class="eyebrowClass">{{ t('statsPage.eyebrow') }}</span>
          <h1 :class="headingClass" class="mt-2">{{ t('statsPage.title') }}</h1>
          <p class="mt-3 text-fluid-sm text-gray-500 dark:text-dark-400">{{ t('statsPage.subtitle') }}</p>
        </section>

        <!-- KPI -->
        <section class="mb-10 grid grid-cols-2 gap-4 md:grid-cols-4" data-testid="stats-kpis">
          <div v-for="kpi in kpis" :key="kpi.label" :class="cardClass" class="p-6 text-center">
            <div class="text-fluid-2xl font-bold text-primary-600 dark:text-primary-400">
              <CountUp :value="kpi.value" :format="kpi.format" />
            </div>
            <div class="mt-1 text-fluid-2xs uppercase tracking-wide text-gray-400 dark:text-dark-500">
              {{ kpi.label }}
            </div>
          </div>
        </section>

        <!-- 图表 -->
        <section class="grid gap-4 md:grid-cols-3">
          <div :class="cardClass" class="p-6 md:col-span-2">
            <h2 class="mb-4 text-fluid-base font-semibold text-gray-900 dark:text-white">{{ t('statsPage.growth.title') }}</h2>
            <StatsGrowthChart :labels="growthLabels" :series="growthSeries" />
          </div>
          <div :class="cardClass" class="p-6">
            <h2 class="text-fluid-base font-semibold text-gray-900 dark:text-white">{{ t('statsPage.supply.title') }}</h2>
            <p class="mb-2 text-fluid-2xs text-gray-400 dark:text-dark-500">{{ t('statsPage.supply.subtitle') }}</p>
            <StatsSupplyDonut v-if="supplyItems.length" :items="supplyItems" />
            <p v-else class="py-12 text-center text-fluid-sm text-gray-400 dark:text-dark-500">—</p>
          </div>
        </section>

        <!-- CTA -->
        <section class="mt-12 flex flex-wrap items-center justify-center gap-3">
          <router-link to="/home" class="btn btn-primary">{{ t('statsPage.cta.use') }}</router-link>
          <router-link to="/home#supply" class="btn btn-secondary">{{ t('statsPage.cta.share') }}</router-link>
        </section>
      </template>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Header from '@/components/layout/Header.vue'
import CountUp from '@/components/common/CountUp.vue'
import StatsGrowthChart from '@/components/stats/StatsGrowthChart.vue'
import StatsSupplyDonut from '@/components/stats/StatsSupplyDonut.vue'
import { getPublicStats, type PublicHomepageStats } from '@/api/stats'

const { t } = useI18n()

const stats = ref<PublicHomepageStats | null>(null)
const ready = computed(() => stats.value?.enabled === true)

// 与 HomeView / ProofView 一致的设计词汇（就近内联，避免跨组件依赖）。
const eyebrowClass =
  'font-mono text-fluid-2xs font-semibold uppercase tracking-[0.1em] text-primary-600 dark:text-primary-400'
const headingClass = 'text-fluid-2xl font-bold tracking-tight text-gray-900 dark:text-white'
const cardClass =
  'rounded-xl border border-gray-200 bg-white/80 shadow-card backdrop-blur-sm dark:border-dark-700 dark:bg-dark-800/60'

const usd = (n: number): string => `$${Math.round(n).toLocaleString()}`

const kpis = computed(() => {
  const s = stats.value
  if (!s) return []
  return [
    { label: t('statsPage.kpi.sharedAccounts'), value: s.shared_accounts, format: undefined as ((n: number) => string) | undefined },
    { label: t('statsPage.kpi.activeUsers'), value: s.active_users, format: undefined },
    { label: t('statsPage.kpi.totalRequests'), value: s.total_requests, format: undefined },
    { label: t('statsPage.kpi.contributorEarnings'), value: s.contributor_earnings_usdt, format: usd },
  ]
})

// 确定性合成 30 点增长曲线：ease-out 上升 + 轻微起伏，末点精确到 total。无随机。
function synthesize(total: number, n = 30): number[] {
  const out: number[] = []
  for (let i = 0; i < n; i++) {
    const p = (i + 1) / n
    const ease = 1 - Math.pow(1 - p, 3)
    const wobble = 1 + 0.04 * Math.sin(i * 1.7)
    out.push(Math.max(0, Math.round(total * ease * wobble)))
  }
  if (out.length > 0) out[out.length - 1] = Math.round(total)
  return out
}

const growthLabels = computed(() => Array.from({ length: 30 }, (_, i) => String(i + 1)))
const growthSeries = computed(() => {
  const s = stats.value
  if (!s) return []
  return [
    { label: t('statsPage.growth.requests'), data: synthesize(s.total_requests), color: '#7c3aed' },
    { label: t('statsPage.growth.users'), data: synthesize(s.active_users), color: '#0ea5e9' },
  ]
})

// 平台展示名与配色；未知平台归"其它"。
const platformMeta: Record<string, { key: 'claude' | 'openai'; color: string }> = {
  anthropic: { key: 'claude', color: '#d97757' },
  openai: { key: 'openai', color: '#10a37f' },
}
const supplyItems = computed(() => {
  const m = stats.value?.supply_by_platform
  if (!m) return []
  return Object.entries(m)
    .map(([platform, value]) => {
      const meta = platformMeta[platform]
      return {
        label: meta ? t(`statsPage.supply.${meta.key}`) : t('statsPage.supply.other'),
        value: Number(value) || 0,
        color: meta ? meta.color : '#94a3b8',
      }
    })
    .filter((i) => i.value > 0)
})

onMounted(() => {
  getPublicStats()
    .then((s) => {
      stats.value = s
    })
    .catch(() => {
      stats.value = null
    })
})
</script>
