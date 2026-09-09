<script setup lang="ts">
// 纯展示的数字滚动动画。进入视口才开始（给数据看板一点"活"的观感）；
// 无依赖。prefers-reduced-motion 或无 rAF 环境直接到位，不做动画。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = withDefaults(
  defineProps<{
    /** 目标值。 */
    value: number
    /** 动画时长(ms)。 */
    duration?: number
    /** 可选格式化，缺省用 toLocaleString()。 */
    format?: (n: number) => string
  }>(),
  { duration: 1200 }
)

const display = ref(0)
const el = ref<HTMLElement | null>(null)
let raf = 0
let started = false

function easeOutCubic(t: number): number {
  return 1 - Math.pow(1 - t, 3)
}

function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  )
}

function animate(to: number): void {
  if (typeof requestAnimationFrame === 'undefined' || prefersReducedMotion()) {
    display.value = to
    return
  }
  cancelAnimationFrame(raf)
  const start = performance.now()
  const dur = Math.max(1, props.duration)
  const step = (now: number): void => {
    const t = Math.min(1, (now - start) / dur)
    display.value = to * easeOutCubic(t)
    if (t < 1) raf = requestAnimationFrame(step)
    else display.value = to
  }
  raf = requestAnimationFrame(step)
}

function begin(): void {
  if (started) return
  started = true
  animate(props.value)
}

let observer: IntersectionObserver | null = null

onMounted(() => {
  if (typeof IntersectionObserver === 'undefined' || !el.value) {
    begin()
    return
  }
  observer = new IntersectionObserver(
    (entries) => {
      if (entries.some((e) => e.isIntersecting)) {
        begin()
        observer?.disconnect()
      }
    },
    { threshold: 0.2 }
  )
  observer.observe(el.value)
})

// 值变化（重新拉取数据）时再滚一遍。
watch(
  () => props.value,
  (v) => {
    if (started) animate(v)
  }
)

onBeforeUnmount(() => {
  cancelAnimationFrame(raf)
  observer?.disconnect()
})

const text = computed(() => {
  const n = Math.round(display.value)
  return props.format ? props.format(n) : n.toLocaleString()
})
</script>

<template>
  <span ref="el">{{ text }}</span>
</template>
