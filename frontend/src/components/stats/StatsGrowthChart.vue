<script setup lang="ts">
// 公开数据看板的增长面积线。数据由父组件传入（看板页按展示大数合成的曲线）。
import { computed } from 'vue'
import { Line } from 'vue-chartjs'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler,
  type ChartData,
  type ChartOptions,
} from 'chart.js'
import { formatCompactNumber } from '@/utils/format'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend, Filler)

const props = defineProps<{
  labels: string[]
  series: { label: string; data: number[]; color: string }[]
  // 强制深色调色板（/stats 页恒为深色，与站点主题无关）；不传则跟随站点 dark class。
  dark?: boolean
}>()

function isDark(): boolean {
  if (props.dark !== undefined) return props.dark
  return typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
}

const chartData = computed<ChartData<'line'>>(() => ({
  labels: props.labels,
  datasets: props.series.map((s) => ({
    label: s.label,
    data: s.data,
    borderColor: s.color,
    backgroundColor: `${s.color}22`,
    fill: true,
    tension: 0.35,
    pointRadius: 0,
    borderWidth: 2,
  })),
}))

const options = computed<ChartOptions<'line'>>(() => {
  const grid = isDark() ? 'rgba(148,163,184,0.12)' : 'rgba(100,116,139,0.12)'
  const tick = isDark() ? '#94a3b8' : '#64748b'
  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { intersect: false, mode: 'index' },
    plugins: {
      legend: { display: props.series.length > 1, labels: { color: tick, boxWidth: 12 } },
    },
    scales: {
      x: { grid: { display: false }, ticks: { color: tick, maxTicksLimit: 6 } },
      y: {
        grid: { color: grid },
        ticks: {
          color: tick,
          maxTicksLimit: 5,
          callback: (value) => formatCompactNumber(Number(value)),
        },
        beginAtZero: true,
      },
    },
  }
})
</script>

<template>
  <div class="h-64">
    <Line :data="chartData" :options="options" />
  </div>
</template>
