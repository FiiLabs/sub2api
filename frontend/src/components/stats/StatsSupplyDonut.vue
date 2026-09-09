<script setup lang="ts">
// 公开数据看板的供给占比环形图。展示真实的按平台占比（不显绝对数，避免与带偏移的大卡冲突）。
import { computed } from 'vue'
import { Doughnut } from 'vue-chartjs'
import {
  Chart as ChartJS,
  ArcElement,
  Tooltip,
  Legend,
  type ChartData,
  type ChartOptions,
} from 'chart.js'

ChartJS.register(ArcElement, Tooltip, Legend)

const props = defineProps<{
  // 平台 → 号数（真实）。占比由此算。
  items: { label: string; value: number; color: string }[]
  // 强制深色调色板（/stats 页恒为深色）；不传则跟随站点 dark class。
  dark?: boolean
}>()

function isDark(): boolean {
  if (props.dark !== undefined) return props.dark
  return typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
}

const total = computed(() => props.items.reduce((s, i) => s + i.value, 0))

const chartData = computed<ChartData<'doughnut'>>(() => ({
  labels: props.items.map((i) => i.label),
  datasets: [
    {
      data: props.items.map((i) => i.value),
      backgroundColor: props.items.map((i) => i.color),
      borderWidth: 0,
    },
  ],
}))

const options = computed<ChartOptions<'doughnut'>>(() => {
  const tick = isDark() ? '#cbd5e1' : '#475569'
  return {
    responsive: true,
    maintainAspectRatio: false,
    cutout: '62%',
    plugins: {
      legend: { position: 'bottom', labels: { color: tick, boxWidth: 12, padding: 14 } },
      tooltip: {
        // 只显占比百分比，不泄露绝对号数。
        callbacks: {
          label: (ctx) => {
            const v = Number(ctx.parsed) || 0
            const pct = total.value > 0 ? Math.round((v / total.value) * 100) : 0
            return `${ctx.label}: ${pct}%`
          },
        },
      },
    },
  }
})
</script>

<template>
  <div class="h-64">
    <Doughnut :data="chartData" :options="options" />
  </div>
</template>
