<template>
  <!-- /stats 恒为深色「指挥中心」风：网格底纹 + 辉光 + 玻璃卡片，与站点主题无关。 -->
  <div class="relative min-h-screen overflow-hidden bg-dark-950 pt-16 text-white md:pt-[72px]">
    <Header />

    <!-- 背景装饰 -->
    <div class="stats-grid pointer-events-none absolute inset-0" aria-hidden="true"></div>
    <div class="pointer-events-none absolute -top-24 left-1/4 h-96 w-96 -translate-x-1/2 rounded-full bg-primary-500/20 blur-[130px]" aria-hidden="true"></div>
    <div class="pointer-events-none absolute top-52 right-0 h-80 w-80 rounded-full bg-cyan-500/10 blur-[130px]" aria-hidden="true"></div>

    <main class="relative mx-auto max-w-6xl px-4 py-12 sm:px-6 lg:py-16">
      <!-- 未开启 / 拉取失败：优雅空状态 -->
      <section
        v-if="!ready"
        class="flex min-h-[60vh] flex-col items-center justify-center text-center"
        data-testid="stats-empty"
      >
        <span class="mb-4 flex h-16 w-16 items-center justify-center rounded-2xl border border-white/10 bg-white/5 text-3xl backdrop-blur">📊</span>
        <h1 class="text-fluid-xl font-bold tracking-tight text-white">{{ t('statsPage.empty.title') }}</h1>
        <p class="mt-2 max-w-md text-fluid-sm text-slate-400">{{ t('statsPage.empty.desc') }}</p>
        <router-link to="/home" class="mt-6 rounded-lg border border-white/15 px-5 py-2.5 text-fluid-sm font-semibold text-slate-200 transition-colors hover:bg-white/5">
          {{ t('statsPage.cta.home') }}
        </router-link>
      </section>

      <template v-else>
        <!-- HERO -->
        <section class="mb-14 text-center">
          <span class="inline-flex items-center gap-2 rounded-full border border-primary-400/30 bg-primary-500/10 px-3 py-1 font-mono text-fluid-2xs uppercase tracking-[0.18em] text-primary-300">
            <span class="stats-pulse h-1.5 w-1.5 rounded-full bg-emerald-400"></span>
            {{ t('statsPage.hero.live') }}
          </span>
          <h1 class="mt-5 text-fluid-3xl font-bold tracking-tight">
            <span class="stats-gradient-text">{{ t('statsPage.title') }}</span>
          </h1>
          <p class="mx-auto mt-3 max-w-xl text-fluid-sm text-slate-400">{{ t('statsPage.subtitle') }}</p>
        </section>

        <!-- KPI -->
        <section data-testid="stats-kpis" class="mb-12 grid grid-cols-2 gap-4 md:grid-cols-4">
          <div v-for="kpi in kpis" :key="kpi.label" class="stats-card p-6">
            <div class="font-mono text-fluid-2xl font-bold tracking-tight text-white">
              <CountUp :value="kpi.value" :format="kpi.format" />
            </div>
            <div class="mt-2 text-fluid-2xs uppercase tracking-wide text-slate-400">{{ kpi.label }}</div>
          </div>
        </section>

        <!-- 图表 -->
        <section class="mb-12 grid gap-4 md:grid-cols-3">
          <div class="stats-card p-6 md:col-span-2">
            <h2 class="mb-4 font-mono text-fluid-xs uppercase tracking-wider text-slate-400">{{ t('statsPage.growth.title') }}</h2>
            <StatsGrowthChart :labels="growthLabels" :series="growthSeries" :dark="true" />
          </div>
          <div class="stats-card p-6">
            <h2 class="font-mono text-fluid-xs uppercase tracking-wider text-slate-400">{{ t('statsPage.supply.title') }}</h2>
            <p class="mb-2 text-fluid-2xs text-slate-500">{{ t('statsPage.supply.subtitle') }}</p>
            <StatsSupplyDonut v-if="supplyItems.length" :items="supplyItems" :dark="true" />
            <p v-else class="py-12 text-center text-fluid-sm text-slate-500">—</p>
          </div>
        </section>

        <!-- 支持的模型 + 可验证 -->
        <section class="mb-12 grid gap-4 md:grid-cols-2">
          <div class="stats-card p-6">
            <h2 class="font-mono text-fluid-xs uppercase tracking-wider text-slate-400">{{ t('statsPage.models.title') }}</h2>
            <p class="mb-4 text-fluid-2xs text-slate-500">{{ t('statsPage.models.subtitle') }}</p>
            <div class="flex flex-wrap gap-2">
              <span
                v-for="m in models"
                :key="m.name"
                class="inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-fluid-xs font-medium"
                :class="m.live
                  ? 'border-emerald-400/25 bg-emerald-400/10 text-emerald-300'
                  : 'border-slate-600/40 bg-slate-700/20 text-slate-400'"
              >
                <span class="h-1.5 w-1.5 rounded-full" :class="m.live ? 'bg-emerald-400' : 'bg-slate-500'"></span>
                {{ m.name }}
                <span class="text-fluid-2xs opacity-70">{{ m.live ? t('statsPage.models.live') : t('statsPage.models.soon') }}</span>
              </span>
            </div>
          </div>
          <div class="stats-card relative overflow-hidden p-6">
            <span class="font-mono text-fluid-xs uppercase tracking-wider text-primary-300">{{ t('statsPage.verify.eyebrow') }}</span>
            <p class="mt-2 text-fluid-lg font-semibold text-white">{{ t('statsPage.verify.title') }}</p>
            <p class="mt-2 text-fluid-sm text-slate-400">{{ t('statsPage.verify.desc') }}</p>
            <router-link to="/proof" class="mt-4 inline-flex items-center gap-1 text-fluid-sm font-semibold text-primary-300 transition-colors hover:text-primary-200">
              {{ t('statsPage.verify.cta') }}
            </router-link>
          </div>
        </section>

        <!-- CTA -->
        <section class="flex flex-wrap items-center justify-center gap-3">
          <router-link to="/home" class="rounded-lg bg-primary-500 px-6 py-2.5 text-fluid-sm font-semibold text-white shadow-lg shadow-primary-500/20 transition-colors hover:bg-primary-400">
            {{ t('statsPage.cta.use') }}
          </router-link>
          <router-link to="/home#supply" class="rounded-lg border border-white/15 px-6 py-2.5 text-fluid-sm font-semibold text-slate-200 transition-colors hover:bg-white/5">
            {{ t('statsPage.cta.share') }}
          </router-link>
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
    { label: t('statsPage.growth.requests'), data: synthesize(s.total_requests), color: '#a78bfa' },
    { label: t('statsPage.growth.users'), data: synthesize(s.active_users), color: '#22d3ee' },
  ]
})

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

<style scoped>
/* 网格底纹，向边缘淡出 */
.stats-grid {
  background-image:
    linear-gradient(rgba(148, 163, 184, 0.08) 1px, transparent 1px),
    linear-gradient(90deg, rgba(148, 163, 184, 0.08) 1px, transparent 1px);
  background-size: 44px 44px;
  -webkit-mask-image: radial-gradient(ellipse 80% 60% at 50% 0%, #000 40%, transparent 85%);
  mask-image: radial-gradient(ellipse 80% 60% at 50% 0%, #000 40%, transparent 85%);
}

/* 玻璃卡片 */
.stats-card {
  border-radius: 1rem;
  border: 1px solid rgba(255, 255, 255, 0.08);
  background: rgba(255, 255, 255, 0.03);
  backdrop-filter: blur(8px);
  transition: border-color 0.25s, box-shadow 0.25s, transform 0.25s;
}
.stats-card:hover {
  border-color: rgba(139, 130, 246, 0.35);
  box-shadow: 0 0 0 1px rgba(139, 130, 246, 0.15), 0 12px 40px -12px rgba(124, 58, 237, 0.35);
}

/* 渐变标题 */
.stats-gradient-text {
  background: linear-gradient(90deg, #a78bfa 0%, #60a5fa 45%, #22d3ee 100%);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

/* 实时脉冲 */
.stats-pulse {
  box-shadow: 0 0 0 0 rgba(52, 211, 153, 0.7);
  animation: stats-pulse 1.8s infinite;
}
@keyframes stats-pulse {
  0% { box-shadow: 0 0 0 0 rgba(52, 211, 153, 0.6); }
  70% { box-shadow: 0 0 0 6px rgba(52, 211, 153, 0); }
  100% { box-shadow: 0 0 0 0 rgba(52, 211, 153, 0); }
}
@media (prefers-reduced-motion: reduce) {
  .stats-pulse { animation: none; }
}
</style>
