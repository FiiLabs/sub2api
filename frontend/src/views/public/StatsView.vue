<template>
  <!-- 跟随站点主题（浅/深自适应），与 /proof 一致；不再强制深色。 -->
  <div class="min-h-screen bg-gray-50 pt-16 text-gray-900 dark:bg-dark-950 dark:text-white md:pt-[72px]">
    <Header />

    <main class="mx-auto max-w-6xl px-4 py-10 sm:px-6 lg:py-14">
      <!-- 未开启 / 拉取失败：优雅空状态 -->
      <section
        v-if="!ready"
        class="flex min-h-[55vh] flex-col items-center justify-center text-center"
        data-testid="stats-empty"
      >
        <span class="mb-4 flex h-16 w-16 items-center justify-center rounded-2xl bg-primary-50 text-3xl dark:bg-primary-500/10">📊</span>
        <h1 class="text-fluid-xl font-bold tracking-tight text-gray-900 dark:text-white">{{ t('statsPage.empty.title') }}</h1>
        <p class="mt-2 max-w-md text-fluid-sm text-gray-500 dark:text-dark-400">{{ t('statsPage.empty.desc') }}</p>
        <router-link to="/home" class="btn btn-secondary mt-6">{{ t('statsPage.cta.home') }}</router-link>
      </section>

      <template v-else>
        <!-- HERO -->
        <section class="mb-12 text-center">
          <span class="inline-flex items-center gap-2 rounded-full border border-primary-200 bg-primary-50 px-3 py-1 font-mono text-fluid-2xs uppercase tracking-[0.15em] text-primary-700 dark:border-primary-400/30 dark:bg-primary-500/10 dark:text-primary-300">
            <span class="stats-pulse h-1.5 w-1.5 rounded-full bg-emerald-500"></span>
            {{ t('statsPage.hero.live') }}
          </span>
          <h1 class="mt-5 text-fluid-3xl font-bold tracking-tight text-gray-900 dark:text-white">{{ t('statsPage.title') }}</h1>
          <p class="mx-auto mt-3 max-w-xl text-fluid-sm text-gray-500 dark:text-dark-400">{{ t('statsPage.subtitle') }}</p>
        </section>

        <!-- KPI -->
        <section data-testid="stats-kpis" class="mb-6 grid grid-cols-2 gap-4 md:grid-cols-4">
          <div v-for="kpi in kpis" :key="kpi.label" :class="cardClass" class="relative overflow-hidden p-6">
            <div class="flex items-baseline justify-between gap-2">
              <div class="font-mono text-fluid-2xl font-bold tracking-tight text-primary-600 dark:text-primary-400">
                <CountUp :value="kpi.value" :format="kpi.format" />
              </div>
              <svg class="h-3.5 w-3.5 shrink-0 text-emerald-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M5 15l7-7 7 7" />
              </svg>
            </div>
            <div class="mt-1 text-fluid-2xs uppercase tracking-wide text-gray-400 dark:text-dark-500">{{ kpi.label }}</div>
            <div class="mt-3 -mb-1"><Sparkline :data="kpi.spark" :color="kpi.color" :height="30" /></div>
          </div>
        </section>

        <!-- 产品亮点数（真实产品事实） -->
        <section class="mb-10 grid grid-cols-2 gap-3 md:grid-cols-4">
          <div v-for="h in highlights" :key="h.label" :class="cardClass" class="flex items-center gap-3 p-4">
            <span class="font-mono text-fluid-lg font-bold text-gray-900 dark:text-white">{{ h.value }}</span>
            <span class="text-fluid-2xs leading-tight text-gray-500 dark:text-dark-400">{{ h.label }}</span>
          </div>
        </section>

        <!-- 图表 -->
        <section class="mb-10 grid gap-4 md:grid-cols-3">
          <div :class="cardClass" class="p-6 md:col-span-2">
            <h2 class="mb-4 font-mono text-fluid-xs uppercase tracking-wider text-gray-400 dark:text-dark-500">{{ t('statsPage.growth.title') }}</h2>
            <StatsGrowthChart :labels="growthLabels" :series="growthSeries" />
          </div>
          <div :class="cardClass" class="p-6">
            <h2 class="font-mono text-fluid-xs uppercase tracking-wider text-gray-400 dark:text-dark-500">{{ t('statsPage.supply.title') }}</h2>
            <p class="mb-2 text-fluid-2xs text-gray-400 dark:text-dark-500">{{ t('statsPage.supply.subtitle') }}</p>
            <StatsSupplyDonut v-if="supplyItems.length" :items="supplyItems" />
            <p v-else class="py-12 text-center text-fluid-sm text-gray-400 dark:text-dark-500">—</p>
          </div>
        </section>

        <!-- 支持的模型 -->
        <section class="mb-10">
          <div :class="cardClass" class="p-6">
            <h2 class="font-mono text-fluid-xs uppercase tracking-wider text-gray-400 dark:text-dark-500">{{ t('statsPage.models.title') }}</h2>
            <p class="mb-4 text-fluid-2xs text-gray-400 dark:text-dark-500">{{ t('statsPage.models.subtitle') }}</p>
            <div class="flex flex-wrap gap-2">
              <span
                v-for="m in models"
                :key="m.name"
                class="inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-fluid-xs font-medium"
                :class="m.live
                  ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-400/25 dark:bg-emerald-400/10 dark:text-emerald-300'
                  : 'border-gray-200 bg-gray-50 text-gray-500 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-400'"
              >
                <span class="h-1.5 w-1.5 rounded-full" :class="m.live ? 'bg-emerald-500' : 'bg-gray-400 dark:bg-dark-500'"></span>
                {{ m.name }}
                <span class="text-fluid-2xs opacity-70">{{ m.live ? t('statsPage.models.live') : t('statsPage.models.soon') }}</span>
              </span>
            </div>
          </div>
        </section>

        <!-- 信任徽章带（可验证，链到 /proof） -->
        <section :class="cardClass" class="mb-10 p-6">
          <div class="flex flex-col items-center gap-5 text-center md:flex-row md:justify-between md:text-left">
            <div>
              <span class="font-mono text-fluid-xs uppercase tracking-wider text-primary-600 dark:text-primary-400">{{ t('statsPage.trust.eyebrow') }}</span>
              <p class="mt-1 text-fluid-lg font-semibold text-gray-900 dark:text-white">{{ t('statsPage.trust.title') }}</p>
            </div>
            <div class="flex flex-wrap justify-center gap-2">
              <span
                v-for="item in trustItems"
                :key="item"
                class="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 bg-white px-3 py-1.5 text-fluid-xs text-gray-600 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300"
              >
                <svg class="h-3.5 w-3.5 shrink-0 text-emerald-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
                </svg>
                {{ item }}
              </span>
            </div>
            <router-link to="/proof" class="whitespace-nowrap text-fluid-sm font-semibold text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300">
              {{ t('statsPage.trust.cta') }}
            </router-link>
          </div>
        </section>

        <!-- CTA -->
        <section class="flex flex-wrap items-center justify-center gap-3">
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
import Sparkline from '@/components/stats/Sparkline.vue'
import { getPublicStats, type PublicHomepageStats } from '@/api/stats'

const { t } = useI18n()

const stats = ref<PublicHomepageStats | null>(null)
const ready = computed(() => stats.value?.enabled === true)

// 与 HomeView / ProofView 一致的卡片样式（站点主题、浅/深自适应）。
const cardClass =
  'rounded-xl border border-gray-200 bg-white/80 shadow-card backdrop-blur-sm dark:border-dark-700 dark:bg-dark-800/60'

const usd = (n: number): string => `$${Math.round(n).toLocaleString()}`

type Fmt = ((n: number) => string) | undefined
const kpis = computed(() => {
  const s = stats.value
  if (!s) return []
  const mk = (label: string, value: number, color: string, format: Fmt) => ({
    label,
    value,
    format,
    color,
    spark: synthesize(value, 16),
  })
  return [
    mk(t('statsPage.kpi.sharedAccounts'), s.shared_accounts, '#5d30f7', undefined),
    mk(t('statsPage.kpi.activeUsers'), s.active_users, '#9385ff', undefined),
    mk(t('statsPage.kpi.totalRequests'), s.total_requests, '#7b61ff', undefined),
    mk(t('statsPage.kpi.contributorEarnings'), s.contributor_earnings_usdt, '#10a37f', usd),
  ]
})

// 产品亮点数（产品事实，非统计数字）。
const highlights = computed(() => [
  { value: t('statsPage.highlights.discount.value'), label: t('statsPage.highlights.discount.label') },
  { value: t('statsPage.highlights.verifiable.value'), label: t('statsPage.highlights.verifiable.label') },
  { value: t('statsPage.highlights.models.value'), label: t('statsPage.highlights.models.label') },
  { value: t('statsPage.highlights.failover.value'), label: t('statsPage.highlights.failover.label') },
])

// 信任徽章（产品事实，与首页/proof 同源口径）。
const trustItems = computed(() => [
  t('statsPage.trust.tee'),
  t('statsPage.trust.attestation'),
  t('statsPage.trust.noTraining'),
])

// 支持的模型（产品事实，非统计数字）。品牌名字面量。
const models = [
  { name: 'Claude', live: true },
  { name: 'GPT-6', live: true },
  { name: 'Gemini', live: false },
]

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
    { label: t('statsPage.growth.requests'), data: synthesize(s.total_requests), color: '#5d30f7' },
    { label: t('statsPage.growth.users'), data: synthesize(s.active_users), color: '#9385ff' },
  ]
})

const platformMeta: Record<string, { key: 'claude' | 'openai'; color: string }> = {
  anthropic: { key: 'claude', color: '#d97757' },
  openai: { key: 'openai', color: '#10a37f' },
}
const supplyItems = computed(() => {
  const s = stats.value
  if (!s) return []
  // 有真实按平台分解就用真实占比。
  const real = s.supply_by_platform
  if (real) {
    const items = Object.entries(real)
      .map(([platform, value]) => {
        const meta = platformMeta[platform]
        return {
          label: meta ? t(`statsPage.supply.${meta.key}`) : t('statsPage.supply.other'),
          value: Number(value) || 0,
          color: meta ? meta.color : '#94a3b8',
        }
      })
      .filter((i) => i.value > 0)
    if (items.length > 0) return items
  }
  // 本地/早期没有真实供给分解时，按展示的共享号数 + 默认权重合成占比，
  // 让环形图有内容且与上面的「共享账号」KPI 一致（环形图只显占比，与「混合」口径一致）。
  const total = s.shared_accounts
  if (total <= 0) return []
  return [
    { label: t('statsPage.supply.claude'), value: Math.round(total * 0.6), color: '#d97757' },
    { label: t('statsPage.supply.openai'), value: Math.round(total * 0.4), color: '#10a37f' },
  ]
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

<style scoped>
/* 实时脉冲点 */
.stats-pulse {
  animation: stats-pulse 1.8s infinite;
}
@keyframes stats-pulse {
  0% { box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.55); }
  70% { box-shadow: 0 0 0 6px rgba(16, 185, 129, 0); }
  100% { box-shadow: 0 0 0 0 rgba(16, 185, 129, 0); }
}
@media (prefers-reduced-motion: reduce) {
  .stats-pulse { animation: none; }
}
</style>
