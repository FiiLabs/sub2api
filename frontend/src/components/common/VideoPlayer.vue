<template>
  <div
    ref="rootRef"
    class="group relative w-full select-none overflow-hidden rounded-xl bg-gray-950"
    :style="{ aspectRatio: aspect }"
    @pointerenter="revealControls"
    @pointermove="revealControls"
    @pointerleave="hideControls"
    @focusin="revealControls"
  >
    <!-- 封面：视频就位前一直垫在底层，避免出现黑块闪烁 -->
    <img
      v-if="poster"
      :src="poster"
      alt=""
      aria-hidden="true"
      class="absolute inset-0 h-full w-full transition-opacity duration-500"
      :class="[fitClass, showVideo ? 'opacity-0' : 'opacity-100']"
    />
    <div
      v-else
      aria-hidden="true"
      class="absolute inset-0 bg-gradient-to-br from-dark-900 via-dark-950 to-primary-950/40"
    ></div>

    <!-- 视频本体：进入视口后才挂载，首屏不产生请求 -->
    <video
      v-if="attached"
      ref="videoRef"
      class="absolute inset-0 h-full w-full transition-opacity duration-500"
      :class="[fitClass, showVideo ? 'opacity-100' : 'opacity-0']"
      :poster="poster || undefined"
      :loop="loop"
      :muted="muted"
      :controls="controls"
      :preload="preload"
      playsinline
      v-bind="nativeVideoAttrs"
      @loadstart="onLoadStart"
      @loadedmetadata="onDurationChange"
      @durationchange="onDurationChange"
      @loadeddata="onLoadedData"
      @canplay="onCanPlay"
      @progress="onBufferProgress"
      @seeked="onSeeked"
      @playing="onPlaying"
      @play="onPlay"
      @pause="onPause"
      @waiting="onWaiting"
      @stalled="onWaiting"
      @suspend="clearStallTimer"
      @timeupdate="onTimeUpdate"
      @ended="onEnded"
      @error="onError"
      @volumechange="onVolumeChange"
      @click="onVideoClick"
    >
      <source v-for="item in sources" :key="item.src" :src="item.src" :type="item.type" @error="onError" />
    </video>

    <!-- 缓冲指示：首次加载与中途卡顿共用 -->
    <div v-if="showSpinner" class="pointer-events-none absolute inset-0 grid place-items-center">
      <span class="h-10 w-10 animate-spin rounded-full border-2 border-white/25 border-t-white/90"></span>
    </div>

    <!-- 播放入口：未开播 / 被浏览器拦截 / 用户暂停时覆盖整块，点哪都能播 -->
    <button
      v-if="showPlayOverlay"
      type="button"
      class="absolute inset-0 grid place-items-center bg-gray-950/35 transition-colors hover:bg-gray-950/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-400"
      :aria-label="t('common.videoPlayer.play')"
      @click="playByUser"
    >
      <span
        class="grid h-16 w-16 place-items-center rounded-full bg-white/90 shadow-lg transition-transform duration-200 group-hover:scale-105"
      >
        <svg viewBox="0 0 24 24" class="h-7 w-7 fill-gray-900" aria-hidden="true">
          <path d="M8 5.14v13.72a1 1 0 0 0 1.54.84l10.79-6.86a1 1 0 0 0 0-1.68L9.54 4.3A1 1 0 0 0 8 5.14Z" />
        </svg>
      </span>
    </button>

    <!-- 失败态：自动重试用尽后交回给用户 -->
    <div
      v-if="status === 'error'"
      class="absolute inset-0 grid place-items-center bg-gray-950/80 px-6 text-center backdrop-blur-sm"
    >
      <div>
        <div class="text-fluid-sm font-medium text-white">{{ t('common.videoPlayer.error') }}</div>
        <div class="mt-1 text-fluid-2xs text-dark-300">
          {{ t('common.videoPlayer.errorHint', { count: attempts }) }}
        </div>
        <div class="mt-4 flex flex-wrap justify-center gap-2">
          <button
            type="button"
            class="rounded-lg bg-primary-500 px-4 py-2 text-fluid-xs font-semibold text-white transition-colors hover:bg-primary-600"
            @click="retryByUser"
          >
            {{ t('common.videoPlayer.retry') }}
          </button>
          <a
            v-if="externalHref"
            :href="externalHref"
            target="_blank"
            rel="noopener noreferrer"
            class="rounded-lg border border-white/25 px-4 py-2 text-fluid-xs font-medium text-white transition-colors hover:border-white/60"
          >
            {{ t('common.videoPlayer.openExternal') }}
          </a>
        </div>
      </div>
    </div>

    <!-- 轻控件：原生 controls 关闭时才出现；桌面移入显形，触屏点画面显形 -->
    <div
      v-if="!controls && status !== 'error' && showVideo"
      class="absolute inset-x-0 bottom-0 transition-opacity duration-200"
      :class="controlsVisible ? 'opacity-100' : 'pointer-events-none opacity-0'"
    >
      <div class="bg-gradient-to-t from-gray-950/80 to-transparent px-3 pb-2.5 pt-10">
        <!-- 进度条：点击跳转，按住拖动，方向键微调 -->
        <div
          ref="trackRef"
          role="slider"
          :tabindex="canSeek ? 0 : -1"
          :aria-label="t('common.videoPlayer.seek')"
          :aria-valuemin="0"
          :aria-valuemax="Math.round(duration)"
          :aria-valuenow="Math.round(displayTime)"
          :aria-valuetext="`${formatTime(displayTime)} / ${formatTime(duration)}`"
          :aria-disabled="!canSeek"
          class="group/track relative -my-2 touch-none rounded py-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-400"
          :class="canSeek ? 'cursor-pointer' : 'cursor-default'"
          @pointerdown="onTrackPointerDown"
          @keydown="onTrackKeydown"
        >
          <div
            class="relative w-full overflow-hidden rounded-full bg-white/25 transition-[height] duration-150"
            :class="scrubbing ? 'h-1.5' : 'h-1 group-hover/track:h-1.5'"
          >
            <!-- 已缓冲 -->
            <div class="absolute inset-y-0 left-0 rounded-full bg-white/30" :style="{ width: `${bufferedPercent}%` }"></div>
            <!-- 已播放：拖动时跟手，不做过渡 -->
            <div
              class="absolute inset-y-0 left-0 rounded-full bg-primary-400"
              :class="scrubbing ? '' : 'transition-[width] duration-200'"
              :style="{ width: `${progress}%` }"
            ></div>
          </div>
          <span
            v-if="canSeek"
            aria-hidden="true"
            class="pointer-events-none absolute top-1/2 h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-full bg-white shadow transition-opacity duration-150"
            :class="scrubbing ? 'opacity-100' : 'opacity-0 group-hover/track:opacity-100'"
            :style="{ left: `${progress}%` }"
          ></span>
        </div>

        <div class="mt-2 flex items-center gap-2">
          <button
            type="button"
            class="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-white/15 text-white backdrop-blur-sm transition-colors hover:bg-white/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-400"
            :aria-label="playing ? t('common.videoPlayer.pause') : t('common.videoPlayer.play')"
            @click="togglePlay"
          >
            <svg v-if="playing" viewBox="0 0 24 24" class="h-4 w-4 fill-current" aria-hidden="true">
              <path d="M7 4h3.5v16H7zM13.5 4H17v16h-3.5z" />
            </svg>
            <svg v-else viewBox="0 0 24 24" class="h-4 w-4 fill-current" aria-hidden="true">
              <path d="M8 5.14v13.72a1 1 0 0 0 1.54.84l10.79-6.86a1 1 0 0 0 0-1.68L9.54 4.3A1 1 0 0 0 8 5.14Z" />
            </svg>
          </button>

          <span v-if="canSeek" class="shrink-0 font-mono text-fluid-2xs tabular-nums text-white/80">
            {{ formatTime(displayTime) }} / {{ formatTime(duration) }}
          </span>

          <span class="flex-1"></span>

          <button
            type="button"
            class="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-white/15 text-white backdrop-blur-sm transition-colors hover:bg-white/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-400"
            :aria-label="muted ? t('common.videoPlayer.unmute') : t('common.videoPlayer.mute')"
            @click="toggleMute"
          >
            <svg v-if="muted" viewBox="0 0 24 24" class="h-4 w-4 fill-current" aria-hidden="true">
              <path d="M4 9v6h4l5 4V5L8 9H4Zm12.5 3 2.8-2.8-1-1L15.5 11l-2.8-2.8-1 1L14.5 12l-2.8 2.8 1 1 2.8-2.8 2.8 2.8 1-1L16.5 12Z" />
            </svg>
            <svg v-else viewBox="0 0 24 24" class="h-4 w-4 fill-current" aria-hidden="true">
              <path d="M4 9v6h4l5 4V5L8 9H4Zm11.5-.9v7.8a4.5 4.5 0 0 0 0-7.8Zm0-3.4v1.6a6 6 0 0 1 0 11.4v1.6a7.5 7.5 0 0 0 0-14.6Z" />
            </svg>
          </button>

          <button
            v-if="allowFullscreen"
            type="button"
            class="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-white/15 text-white backdrop-blur-sm transition-colors hover:bg-white/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-400"
            :aria-label="t('common.videoPlayer.fullscreen')"
            @click="toggleFullscreen"
          >
            <svg viewBox="0 0 24 24" class="h-4 w-4 fill-current" aria-hidden="true">
              <path d="M4 9V4h5v2H6v3H4Zm11-5h5v5h-2V6h-3V4ZM4 15h2v3h3v2H4v-5Zm14 0h2v5h-5v-2h3v-3Z" />
            </svg>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDocumentVisibility, useEventListener, useIntersectionObserver, usePreferredReducedMotion } from '@vueuse/core'

/**
 * 宣传视频播放器：只接一条（或一组同源多编码的）直链，剩下的都由组件兜住——
 * 进入视口才加载、静音自动循环、卡住/失败自动重试、离屏与切后台自动暂停。
 */
const props = withDefaults(
  defineProps<{
    /** 视频直链；传数组表示同一视频的多种编码，浏览器挑第一个能播的 */
    src: string | string[]
    /** 封面图，未开播前铺满容器 */
    poster?: string
    /** 容器宽高比，任何合法的 CSS aspect-ratio 值 */
    aspect?: string
    /** 无限循环（默认开），播放结束事件里还有一层兜底 */
    loop?: boolean
    /** 进入视口自动播放（默认开，会强制静音，否则浏览器拦截） */
    autoplay?: boolean
    /** 初始静音（默认开） */
    muted?: boolean
    /** 用原生控件替代内置轻控件 */
    controls?: boolean
    /** 画面填充方式 */
    fit?: 'cover' | 'contain'
    /** 自动重试次数上限（0 关闭自动重试） */
    maxRetries?: number
    /** 首次重试延迟，之后按指数退避 */
    retryDelay?: number
    /** 缓冲多久没进展就判定为失败并重试（0 关闭） */
    stallTimeout?: number
    /** 离开视口 / 切到后台时暂停 */
    pauseWhenHidden?: boolean
    /** 用户主动点播放时取消静音 */
    unmuteOnUserPlay?: boolean
    allowFullscreen?: boolean
    allowDownload?: boolean
    /** 失败兜底：在新窗口打开的地址，默认取第一条直链 */
    externalUrl?: string
  }>(),
  {
    poster: '',
    aspect: '16 / 9',
    loop: true,
    autoplay: true,
    muted: true,
    controls: false,
    fit: 'cover',
    maxRetries: 3,
    retryDelay: 1200,
    stallTimeout: 12000,
    pauseWhenHidden: true,
    unmuteOnUserPlay: true,
    allowFullscreen: true,
    allowDownload: false,
    externalUrl: ''
  }
)

const emit = defineEmits<{
  /** 首帧就绪 */
  (e: 'ready'): void
  (e: 'play'): void
  (e: 'pause'): void
  /** 每次失败都会抛，attempt 为已用掉的重试次数 */
  (e: 'error', payload: { attempt: number; code: number | null; recovering: boolean }): void
}>()

const { t } = useI18n()

const MIME_BY_EXT: Record<string, string> = {
  mp4: 'video/mp4',
  m4v: 'video/mp4',
  mov: 'video/quicktime',
  webm: 'video/webm',
  ogv: 'video/ogg',
  ogg: 'video/ogg'
}

const rootRef = ref<HTMLElement | null>(null)
const videoRef = ref<HTMLVideoElement | null>(null)
const trackRef = ref<HTMLElement | null>(null)

/** 是否已把 <video> 挂进 DOM（懒加载开关） */
const attached = ref(false)
const status = ref<'idle' | 'loading' | 'ready' | 'error'>('idle')
const playing = ref(false)
const buffering = ref(false)
const muted = ref(props.muted)
const duration = ref(0)
const currentTime = ref(0)
const bufferedRatio = ref(0)
/** 正在拖动进度条 */
const scrubbing = ref(false)
/** 拖动中的目标比例，松手前进度条跟手柄走而不是跟播放头走 */
const scrubRatio = ref(0)
/** 控件是否显形：桌面靠指针移入，触屏靠点画面 */
const controlsVisible = ref(true)
/** 已消耗的自动重试次数 */
const attempts = ref(0)
/** 元素在视口内（真正可见，非预加载范围） */
const inView = ref(false)
/** 用户手动暂停过：此后不再自动接管播放 */
const pausedByUser = ref(false)

let retryTimer: ReturnType<typeof setTimeout> | null = null
let stallTimer: ReturnType<typeof setTimeout> | null = null
let spinnerTimer: ReturnType<typeof setTimeout> | null = null
let hideControlsTimer: ReturnType<typeof setTimeout> | null = null
/** 拖动时的 seek 用 rAF 合并，避免每个 pointermove 都打一次 range 请求 */
let seekFrame: number | null = null
/** load() 会把进度清零，重试后接回原位 */
let resumeTime = 0
/** 组件正在卸载：此后媒体元素抛出的错误都是清理副作用，别再当失败处理 */
let disposed = false

const documentVisibility = useDocumentVisibility()
const reducedMotion = usePreferredReducedMotion()

const sources = computed(() => {
  const list = Array.isArray(props.src) ? props.src : [props.src]
  return list
    .map((item) => item.trim())
    .filter(Boolean)
    .map((item) => {
      const ext = (item.split(/[?#]/)[0] ?? '').split('.').pop()?.toLowerCase() ?? ''
      return { src: item, type: MIME_BY_EXT[ext] }
    })
})

const externalHref = computed(() => props.externalUrl || sources.value[0]?.src || '')
const fitClass = computed(() => (props.fit === 'cover' ? 'object-cover' : 'object-contain'))
const preload = computed(() => (props.autoplay ? 'auto' : props.poster ? 'none' : 'metadata'))
/** 降低动效偏好下不自动播放，交给用户点 */
const autoplayEnabled = computed(() => props.autoplay && reducedMotion.value !== 'reduce')
const showVideo = computed(() => status.value === 'ready')
const showPlayOverlay = computed(
  () =>
    status.value !== 'error' &&
    !playing.value &&
    !buffering.value &&
    (!autoplayEnabled.value || pausedByUser.value || status.value === 'ready')
)
/** 加载中的真实状态；短暂卡顿不该让 spinner 闪一下，交给下面的延迟显示 */
const loadingPending = computed(
  () =>
    status.value !== 'error' &&
    (buffering.value || (attached.value && status.value === 'loading' && !showPlayOverlay.value))
)
const showSpinner = ref(false)

/** 时长未知（直播流或元数据还没到）时进度条不可交互 */
const canSeek = computed(() => status.value !== 'error' && duration.value > 0 && Number.isFinite(duration.value))
const progressRatio = computed(() => {
  if (scrubbing.value) return scrubRatio.value
  return duration.value > 0 ? Math.min(1, currentTime.value / duration.value) : 0
})
const progress = computed(() => progressRatio.value * 100)
const bufferedPercent = computed(() => bufferedRatio.value * 100)
/** 拖动过程中时间码显示目标位置，松手后回到真实播放位置 */
const displayTime = computed(() => (scrubbing.value ? scrubRatio.value * duration.value : currentTime.value))

function formatTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '0:00'
  const total = Math.floor(seconds)
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor(total / 60) % 60
  const secs = total % 60
  const mm = hours ? String(minutes).padStart(2, '0') : String(minutes)
  return `${hours ? `${hours}:` : ''}${mm}:${String(secs).padStart(2, '0')}`
}

/**
 * 走 v-bind 对象而不是逐个写在标签上：这几个是浏览器私有属性，
 * 直接写在模板里过不了模板类型检查，语义上也只是「尽力而为」的增强。
 */
const nativeVideoAttrs = computed(() => ({
  controlslist: props.allowDownload ? undefined : 'nodownload',
  disablepictureinpicture: true,
  // 微信/QQ 等 X5 内核默认劫持为全屏播放，声明后才肯内联
  'webkit-playsinline': 'true',
  'x5-playsinline': 'true',
  'x5-video-player-type': 'h5-page'
}))

function clearRetryTimer() {
  if (retryTimer) {
    clearTimeout(retryTimer)
    retryTimer = null
  }
}

// 卡顿 400ms 以上才显示 spinner，秒开的视频不闪
watch(loadingPending, (pending) => {
  if (spinnerTimer) {
    clearTimeout(spinnerTimer)
    spinnerTimer = null
  }
  if (!pending) {
    showSpinner.value = false
    return
  }
  spinnerTimer = setTimeout(() => {
    spinnerTimer = null
    showSpinner.value = loadingPending.value
  }, 400)
})

function clearStallTimer() {
  if (stallTimer) {
    clearTimeout(stallTimer)
    stallTimer = null
  }
  buffering.value = false
}

/** 卡住计时：到点仍无进展就当作一次失败 */
function armStallTimer() {
  if (!props.stallTimeout || status.value === 'error') return
  buffering.value = true
  if (stallTimer) return
  stallTimer = setTimeout(() => {
    stallTimer = null
    fail(null)
  }, props.stallTimeout)
}

async function tryPlay() {
  const el = videoRef.value
  if (!el) return
  try {
    await el.play()
    pausedByUser.value = false
  } catch {
    // 浏览器拒绝自动播放（多为未静音）：退回到点击播放，不算错误
    playing.value = false
  }
}

/** 重新拉流；resetAttempts 用于用户手动重试和网络恢复 */
/**
 * @param resetAttempts 清空已用掉的重试次数
 * @param preserveTime  记住当前进度，新的源就绪后跳回去。
 *
 *   **换源时必须传 false。** 这个参数存在的理由只有一个：同一支片子卡住了
 *   重试，用户不该被拽回开头。但换源走的是同一个函数，而下面那句
 *   「currentTime > 0 就记下来」会把**旧片子**的秒数捞回来——即使调用方
 *   刚刚把 resumeTime 归零。首页的共享者视频跟着语言切中英版，症状就是
 *   切完语言新片子从旧片子的进度开始；两支长度不同的话还会直接落在片尾。
 */
function reload(resetAttempts = false, preserveTime = true) {
  clearRetryTimer()
  clearStallTimer()
  if (resetAttempts) attempts.value = 0

  const el = videoRef.value
  if (!el) {
    attached.value = true
    status.value = 'loading'
    return
  }

  if (preserveTime && Number.isFinite(el.currentTime) && el.currentTime > 0) {
    resumeTime = el.currentTime
  } else {
    resumeTime = 0
  }
  status.value = 'loading'
  el.load()
  if (autoplayEnabled.value && inView.value && !pausedByUser.value) {
    void tryPlay()
  }
}

/**
 * 统一的失败入口：先按指数退避自动重试，配额耗尽才落到错误态。
 * 编解码/格式类错误重试无意义，最多再试一次。
 */
function fail(code: number | null) {
  if (disposed) return
  clearStallTimer()
  const formatIssue = code === 3 || code === 4
  const budget = formatIssue ? Math.min(props.maxRetries, 1) : props.maxRetries

  if (attempts.value >= budget) {
    status.value = 'error'
    playing.value = false
    emit('error', { attempt: attempts.value, code, recovering: false })
    return
  }

  attempts.value += 1
  status.value = 'loading'
  emit('error', { attempt: attempts.value, code, recovering: true })

  clearRetryTimer()
  retryTimer = setTimeout(() => {
    retryTimer = null
    reload()
  }, props.retryDelay * 2 ** (attempts.value - 1))
}

function onLoadStart() {
  if (status.value !== 'error') status.value = 'loading'
}

function onLoadedData() {
  const el = videoRef.value
  if (!el) return
  // 接回重试前的进度（末尾附近就重头播，避免立刻 ended）
  if (resumeTime > 0 && Number.isFinite(el.duration) && resumeTime < el.duration - 0.5) {
    try {
      el.currentTime = resumeTime
    } catch {
      // seek 失败无所谓，从头播
    }
  }
  resumeTime = 0
}

function onCanPlay() {
  clearStallTimer()
  const first = status.value !== 'ready'
  status.value = 'ready'
  attempts.value = 0
  if (first) emit('ready')
}

function onPlay() {
  playing.value = true
  emit('play')
}

function onPlaying() {
  clearStallTimer()
  playing.value = true
  if (status.value !== 'ready') {
    status.value = 'ready'
  }
}

function onPause() {
  playing.value = false
  clearStallTimer()
  emit('pause')
}

function onWaiting() {
  if (!playing.value && status.value === 'ready') return
  armStallTimer()
}

function onTimeUpdate() {
  const el = videoRef.value
  if (!el) return
  clearStallTimer()
  currentTime.value = el.currentTime
}

function onDurationChange() {
  const el = videoRef.value
  if (!el) return
  duration.value = Number.isFinite(el.duration) ? el.duration : 0
}

/** 已缓冲长度：取覆盖当前播放头的那一段，seek 之后才不会显示成上一段的进度 */
function onBufferProgress() {
  const el = videoRef.value
  if (!el || !duration.value) return
  const ranges = el.buffered
  if (!ranges) return
  let end = 0
  for (let i = 0; i < ranges.length; i += 1) {
    if (ranges.start(i) <= el.currentTime && ranges.end(i) >= el.currentTime) {
      end = ranges.end(i)
      break
    }
    end = Math.max(end, ranges.end(i))
  }
  bufferedRatio.value = Math.min(1, end / duration.value)
}

function onSeeked() {
  const el = videoRef.value
  if (!el) return
  clearStallTimer()
  currentTime.value = el.currentTime
  onBufferProgress()
}

/** loop 属性在部分浏览器/流上失灵，这里再兜一层 */
function onEnded() {
  const el = videoRef.value
  if (!el) return
  currentTime.value = duration.value
  if (!props.loop) {
    playing.value = false
    return
  }
  currentTime.value = 0
  try {
    el.currentTime = 0
  } catch {
    // 忽略：下面的 play() 会从头开始
  }
  void tryPlay()
}

function onError() {
  if (disposed || status.value === 'error') return
  fail(videoRef.value?.error?.code ?? null)
}

function onVolumeChange() {
  const el = videoRef.value
  if (el) muted.value = el.muted
}

function onVideoClick() {
  if (props.controls) return
  // 触屏上第一下只是把控件叫出来，别一点就暂停
  if (!controlsVisible.value) {
    revealControls()
    return
  }
  togglePlay()
}

/** 控件显形，并在播放中安排自动隐藏 */
function revealControls() {
  controlsVisible.value = true
  if (hideControlsTimer) {
    clearTimeout(hideControlsTimer)
    hideControlsTimer = null
  }
  if (!playing.value || scrubbing.value) return
  hideControlsTimer = setTimeout(() => {
    hideControlsTimer = null
    if (playing.value && !scrubbing.value) controlsVisible.value = false
  }, 2500)
}

function hideControls() {
  if (scrubbing.value) return
  if (hideControlsTimer) {
    clearTimeout(hideControlsTimer)
    hideControlsTimer = null
  }
  if (playing.value) controlsVisible.value = false
}

/** 指针位置 → 进度比例 */
function ratioFromPointer(event: PointerEvent): number {
  const track = trackRef.value
  if (!track) return 0
  const rect = track.getBoundingClientRect()
  if (!rect.width) return 0
  return Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width))
}

function seekToRatio(ratio: number) {
  const el = videoRef.value
  if (!el || !canSeek.value) return
  const target = Math.min(duration.value, Math.max(0, ratio * duration.value))
  try {
    el.currentTime = target
    currentTime.value = target
  } catch {
    // 还没到可 seek 状态，忽略即可
  }
}

function seekBy(seconds: number) {
  if (!canSeek.value) return
  const target = Math.min(duration.value, Math.max(0, currentTime.value + seconds))
  seekToRatio(duration.value ? target / duration.value : 0)
}

function onTrackPointerDown(event: PointerEvent) {
  if (!canSeek.value) return
  event.preventDefault()
  scrubbing.value = true
  scrubRatio.value = ratioFromPointer(event)
  seekToRatio(scrubRatio.value)
  revealControls()
}

// 拖动过程监听在 window 上：指针甩出播放器范围也不会断，比 setPointerCapture 稳
useEventListener(window, 'pointermove', (event: PointerEvent) => {
  if (!scrubbing.value) return
  scrubRatio.value = ratioFromPointer(event)
  // 进度条立刻跟手，真正的 seek 每帧最多一次
  if (seekFrame !== null) return
  seekFrame = requestAnimationFrame(() => {
    seekFrame = null
    seekToRatio(scrubRatio.value)
  })
})

function endScrub(event: PointerEvent) {
  if (!scrubbing.value) return
  scrubbing.value = false
  if (seekFrame !== null) {
    cancelAnimationFrame(seekFrame)
    seekFrame = null
  }
  seekToRatio(ratioFromPointer(event))
  revealControls()
}

useEventListener(window, 'pointerup', endScrub)
useEventListener(window, 'pointercancel', endScrub)

function onTrackKeydown(event: KeyboardEvent) {
  if (!canSeek.value) return
  const step = event.shiftKey ? 10 : 5
  switch (event.key) {
    case 'ArrowLeft':
      seekBy(-step)
      break
    case 'ArrowRight':
      seekBy(step)
      break
    case 'Home':
      seekToRatio(0)
      break
    case 'End':
      seekToRatio(0.999)
      break
    case ' ':
    case 'Enter':
      togglePlay()
      break
    default:
      return
  }
  event.preventDefault()
  revealControls()
}

function togglePlay() {
  const el = videoRef.value
  if (!el) return
  if (el.paused) {
    pausedByUser.value = false
    void tryPlay()
  } else {
    pausedByUser.value = true
    el.pause()
  }
}

/** 用户主动点播：这是一次用户手势，可以顺带开声音 */
async function playByUser() {
  pausedByUser.value = false
  // 降低动效偏好下视频可能还没挂载，先挂上再播（仍在同一次手势的微任务链里）
  if (!attached.value) {
    attached.value = true
    await nextTick()
  }
  const el = videoRef.value
  if (!el) return
  if (props.unmuteOnUserPlay && el.muted) {
    el.muted = false
    muted.value = false
  }
  await tryPlay()
}

function retryByUser() {
  status.value = 'loading'
  reload(true)
}

function toggleMute() {
  const el = videoRef.value
  if (!el) return
  el.muted = !el.muted
  muted.value = el.muted
}

async function toggleFullscreen() {
  const el = rootRef.value
  const video = videoRef.value as (HTMLVideoElement & { webkitEnterFullscreen?: () => void }) | null
  try {
    if (document.fullscreenElement) {
      await document.exitFullscreen()
    } else if (el?.requestFullscreen) {
      await el.requestFullscreen()
    } else if (video?.webkitEnterFullscreen) {
      // iOS Safari 只允许 video 元素自己全屏
      video.webkitEnterFullscreen()
    }
  } catch {
    // 用户拒绝或浏览器不支持，忽略
  }
}

// 提前一屏挂载，滚到时首帧已就绪
const { stop: stopPreloadObserver, isSupported: observerSupported } = useIntersectionObserver(
  rootRef,
  ([entry]) => {
    if (entry?.isIntersecting) {
      attached.value = true
      stopPreloadObserver()
    }
  },
  { rootMargin: '300px' }
)

// 真正可见才播；滚走就暂停，省电也省流量
useIntersectionObserver(
  rootRef,
  ([entry]) => {
    inView.value = !!entry?.isIntersecting
    const el = videoRef.value
    if (!el) return
    if (inView.value) {
      if (autoplayEnabled.value && !pausedByUser.value && status.value !== 'error') void tryPlay()
    } else if (props.pauseWhenHidden && !el.paused) {
      el.pause()
    }
  },
  { threshold: 0.25 }
)

// 切到后台暂停，切回来接着播（用户自己暂停过的不打扰）
watch(documentVisibility, (state) => {
  const el = videoRef.value
  if (!el || !props.pauseWhenHidden) return
  if (state === 'hidden') {
    if (!el.paused) el.pause()
  } else if (autoplayEnabled.value && inView.value && !pausedByUser.value && status.value !== 'error') {
    void tryPlay()
  }
})

// 断网期间的失败不该是终点：网络回来立刻重来一次
useEventListener(window, 'online', () => {
  if (!attached.value) return
  if (status.value === 'error') {
    reload(true)
    return
  }
  if (!playing.value && autoplayEnabled.value && inView.value && !pausedByUser.value) {
    reload(true)
  }
})

// 换链接等于换一个视频，状态整体重置。
//
// reload 的第二个参数必须是 false：这里把 resumeTime 归零之后，reload 内部
// 还会「currentTime > 0 就记下来」，把旧片子的进度又捞回去——归零就白做了。
// 首页共享者视频按语言切中英版走的正是这条路。
watch(
  () => sources.value.map((item) => item.src).join('|'),
  () => {
    attempts.value = 0
    resumeTime = 0
    currentTime.value = 0
    duration.value = 0
    bufferedRatio.value = 0
    scrubbing.value = false
    pausedByUser.value = false
    status.value = 'idle'
    if (attached.value) reload(true, false)
  }
)

// 播放中控件自动隐藏，暂停时常驻——暂停了还藏起来会让人找不到播放键
watch(playing, (isPlaying) => {
  if (isPlaying) {
    revealControls()
  } else {
    controlsVisible.value = true
    if (hideControlsTimer) {
      clearTimeout(hideControlsTimer)
      hideControlsTimer = null
    }
  }
})

onMounted(() => {
  const el = videoRef.value
  // muted 作为 attribute 在部分浏览器初次渲染不生效，这里按 prop 再落一次
  if (el) el.muted = props.muted || props.autoplay
  muted.value = props.muted || props.autoplay

  // 没有 IntersectionObserver 的浏览器不能把视频永远锁在懒加载里
  if (!observerSupported.value) {
    attached.value = true
    inView.value = true
  }
})

watch(videoRef, (el) => {
  if (!el) return
  el.muted = muted.value
  if (autoplayEnabled.value && inView.value && !pausedByUser.value) void tryPlay()
})

onBeforeUnmount(() => {
  disposed = true
  clearRetryTimer()
  clearStallTimer()
  if (spinnerTimer) clearTimeout(spinnerTimer)
  if (hideControlsTimer) clearTimeout(hideControlsTimer)
  if (seekFrame !== null) cancelAnimationFrame(seekFrame)

  const el = videoRef.value
  if (!el) return
  el.pause()
  // 清空源再 load()：立刻掐断仍在进行的下载，不让它拖着页面跑
  el.removeAttribute('src')
  while (el.firstChild) el.removeChild(el.firstChild)
  el.load()
})

defineExpose({ play: playByUser, pause: () => videoRef.value?.pause(), reload: () => reload(true) })
</script>
