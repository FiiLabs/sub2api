<!--
  APEXONE-EXT: 首页活动横幅。

  显示在首页顶部，给**未登录访客**看。内容全部来自后端 settings（见 api/banner.ts），
  所以换一期活动不需要重新构建镜像、重新部署 CVM。

  三条自我约束：

    1. fail-soft。读不到、超时、后端返回 enabled=false —— 一律什么都不渲染，
       绝不显示占位或错误。这是首页上的一个装饰件，不该因为它出问题而让首页出问题。
    2. 关掉之后要记住。一块关不掉、或者关掉刷新又回来的横幅，比没有横幅更烦人。
    3. 换了文案要重新出现。记住「关掉了」不能记成「这个位置永久关掉」——
       否则下一期活动对老访客永远不可见。所以记的是**这条文案**被关掉了。
-->
<template>
  <Transition name="banner-fade">
    <div v-if="visible" :class="wrapperClass" data-testid="home-banner">
      <div class="mx-auto flex max-w-5xl items-center gap-3 px-4 py-2.5 sm:px-6">
        <p class="min-w-0 flex-1 text-sm leading-snug" data-testid="home-banner-text">
          {{ banner.text }}
        </p>

        <a
          v-if="banner.cta_text && banner.cta_url"
          :href="banner.cta_url"
          :class="ctaClass"
          rel="noopener noreferrer"
          data-testid="home-banner-cta"
        >
          {{ banner.cta_text }}
        </a>

        <button
          type="button"
          :class="closeClass"
          :aria-label="t('common.close')"
          data-testid="home-banner-close"
          @click="dismiss"
        >
          <svg class="h-4 w-4" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M5 5l10 10M15 5L5 15" stroke-linecap="round" />
          </svg>
        </button>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getPublicBanner, type PublicHomepageBanner } from '@/api/banner'

const { t, locale } = useI18n()

const banner = ref<PublicHomepageBanner>({
  enabled: false,
  version: '',
  text: '',
  cta_text: '',
  cta_url: '',
  variant: 'info',
})
const dismissed = ref(false)
let controller: AbortController | null = null

const visible = computed(() => banner.value.enabled && !!banner.value.text && !dismissed.value)

/**
 * 关闭记忆的键用后端下发的 version，**不是**渲染出来的文案。
 *
 * 要记的是「这一期活动被关过」，不是「这一句话被关过」。最初那一版拿 text 当键，
 * 在单语站上等价，双语站上就错了：文案随语言变，键也跟着变——关掉中文横幅之后
 * 切到英文它又冒出来，切回中文又消失，用户看到的是横幅忽隐忽现。
 *
 * version 由后端对两种语言的文案、两个链接与样式一起哈希得到，切语言不改变它，
 * 换一期内容才会变。
 */
function storageKey(): string {
  return `apexone.home-banner.dismissed.${banner.value.version}`
}

function dismiss(): void {
  dismissed.value = true
  // 没有 version 就只关这一次，不落盘：否则所有缺 version 的情况会共用同一个键，
  // 关掉任意一期就等于关掉了以后所有期。
  if (!banner.value.version) {
    return
  }
  try {
    localStorage.setItem(storageKey(), '1')
  } catch {
    // 无痕模式 / 禁用存储：关掉这一次就够了，下次再显示不是错误。
  }
}

function restoreDismissed(): void {
  if (!banner.value.version) {
    dismissed.value = false
    return
  }
  try {
    dismissed.value = localStorage.getItem(storageKey()) === '1'
  } catch {
    dismissed.value = false
  }
}

async function load(): Promise<void> {
  controller?.abort()
  controller = new AbortController()
  try {
    const data = await getPublicBanner(locale.value, { signal: controller.signal })
    banner.value = data
    if (data.enabled && data.text) {
      restoreDismissed()
    }
  } catch {
    // fail-soft：读不到就当没有横幅，不打日志、不提示、不占位。
    banner.value = { enabled: false, version: '', text: '', cta_text: '', cta_url: '', variant: 'info' }
  }
}

onMounted(load)
// 站内切语言要重新拉：后端按语言挑文案，不重拉就会停在上一种语言。
watch(locale, load)
onBeforeUnmount(() => controller?.abort())

/**
 * z-10 而不是 z-20，这一位数字是有约束的。
 *
 * Header 是 `fixed ... z-20`，而语言切换、用户菜单这些下拉是在它**内部**展开的，
 * 会向下延伸到横幅所占的这块区域。横幅如果也取 z-20，同级之下 DOM 靠后的画在上面
 * ——横幅就会盖住下拉菜单，点「English」实际点在横幅上，菜单收不到点击。
 * 现象是「切语言没反应」，而不是「有东西被挡住了」，所以很难往层级上想。
 *
 * z-10 与主内容区（main 上的 relative z-10）同级：在背景装饰之上、在 Header 之下。
 * 改这个值之前，先确认顶栏所有下拉仍然可点。
 */
const wrapperClass = computed(() =>
  banner.value.variant === 'promo'
    ? 'relative z-10 border-b border-primary-500/30 bg-primary-600 text-white dark:bg-primary-700'
    : 'relative z-10 border-b border-gray-200 bg-gray-100 text-gray-800 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-200'
)

const ctaClass = computed(() =>
  banner.value.variant === 'promo'
    ? 'shrink-0 rounded-md bg-white/15 px-3 py-1.5 text-sm font-semibold text-white underline-offset-2 transition hover:bg-white/25'
    : 'shrink-0 rounded-md bg-primary-600 px-3 py-1.5 text-sm font-semibold text-white transition hover:bg-primary-700'
)

const closeClass = computed(() =>
  banner.value.variant === 'promo'
    ? 'shrink-0 rounded p-1 text-white/70 transition hover:bg-white/15 hover:text-white'
    : 'shrink-0 rounded p-1 text-gray-500 transition hover:bg-gray-200 hover:text-gray-700 dark:hover:bg-dark-700 dark:hover:text-gray-200'
)
</script>

<style scoped>
.banner-fade-enter-active,
.banner-fade-leave-active {
  transition: opacity 0.2s ease;
}
.banner-fade-enter-from,
.banner-fade-leave-to {
  opacity: 0;
}
</style>
