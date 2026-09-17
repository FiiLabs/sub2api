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
  text: '',
  cta_text: '',
  cta_url: '',
  variant: 'info',
})
const dismissed = ref(false)
let controller: AbortController | null = null

const visible = computed(() => banner.value.enabled && !!banner.value.text && !dismissed.value)

/**
 * 关闭记忆的键里带着**文案本身的指纹**。
 *
 * 只记「关过横幅」的话，下一期活动对所有关过的老访客永远不可见——而那批人恰恰是
 * 回访率最高的。带上指纹之后，文案一换键就变，横幅自然重新出现。
 *
 * 用一个简单的 32 位滚动哈希而不是 crypto：这不是安全用途，碰撞的后果只是某个
 * 访客少看一次横幅。crypto.subtle 是异步的，为这件事引入一个 await 不值得。
 */
function fingerprint(text: string): string {
  let h = 0
  for (let i = 0; i < text.length; i++) {
    h = (h << 5) - h + text.charCodeAt(i)
    h |= 0
  }
  return (h >>> 0).toString(36)
}

function storageKey(): string {
  return `apexone.home-banner.dismissed.${fingerprint(banner.value.text + '|' + banner.value.cta_url)}`
}

function dismiss(): void {
  dismissed.value = true
  try {
    localStorage.setItem(storageKey(), '1')
  } catch {
    // 无痕模式 / 禁用存储：关掉这一次就够了，下次再显示不是错误。
  }
}

function restoreDismissed(): void {
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
    banner.value = { enabled: false, text: '', cta_text: '', cta_url: '', variant: 'info' }
  }
}

onMounted(load)
// 站内切语言要重新拉：后端按语言挑文案，不重拉就会停在上一种语言。
watch(locale, load)
onBeforeUnmount(() => controller?.abort())

const wrapperClass = computed(() =>
  banner.value.variant === 'promo'
    ? 'relative z-20 border-b border-primary-500/30 bg-primary-600 text-white dark:bg-primary-700'
    : 'relative z-20 border-b border-gray-200 bg-gray-100 text-gray-800 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-200'
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
