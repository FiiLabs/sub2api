<script setup lang="ts">
// 轻量 SVG 迷你走势线（KPI 卡里用）。纯展示，无依赖、无 chart 库。
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    data: number[]
    color?: string
    width?: number
    height?: number
  }>(),
  { color: '#7b61ff', width: 120, height: 34 }
)

const geom = computed(() => {
  const d = props.data
  if (d.length < 2) return { line: '', area: '' }
  const max = Math.max(...d)
  const min = Math.min(...d)
  const range = max - min || 1
  const w = props.width
  const h = props.height
  const step = w / (d.length - 1)
  const pts = d.map((v, i): [number, number] => [i * step, h - ((v - min) / range) * (h - 4) - 2])
  const line = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${p[0].toFixed(1)},${p[1].toFixed(1)}`).join(' ')
  const area = `${line} L${w.toFixed(1)},${h} L0,${h} Z`
  return { line, area }
})

// 渐变 id 按颜色确定性生成，避免同页多个 sparkline 的 defs 冲突（无随机）。
const gradId = computed(
  () => `spark-${Math.abs(props.color.split('').reduce((a, c) => a + c.charCodeAt(0), 0))}`
)
</script>

<template>
  <svg
    :width="width"
    :height="height"
    :viewBox="`0 0 ${width} ${height}`"
    preserveAspectRatio="none"
    class="w-full"
    aria-hidden="true"
  >
    <defs>
      <linearGradient :id="gradId" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0%" :stop-color="color" stop-opacity="0.3" />
        <stop offset="100%" :stop-color="color" stop-opacity="0" />
      </linearGradient>
    </defs>
    <path :d="geom.area" :fill="`url(#${gradId})`" />
    <path
      :d="geom.line"
      :stroke="color"
      fill="none"
      stroke-width="1.6"
      stroke-linejoin="round"
      stroke-linecap="round"
    />
  </svg>
</template>
