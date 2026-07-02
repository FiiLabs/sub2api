<template>
  <header
    class="fixed inset-x-0 top-0 z-20 h-16 border-b border-gray-200/80 bg-white/80 px-4 py-3 backdrop-blur-sm backdrop-saturate-100 dark:border-dark-700/80 dark:bg-dark-900/80 md:h-18 md:px-6"
  >
    <nav class="mx-auto flex max-w-6xl items-center justify-between">
      <div class="flex items-center">
        <router-link to="/" aria-label="ApexOne homepage">
          <svg class="h-6 w-auto md:h-8" viewBox="0 0 514.00 116.25">
            <g fill="none" stroke="currentColor" stroke-width="3.4" stroke-linecap="square"><path d="M33.0 19.0 H20.0 V81.8 H33.0"/><path d="M468.0 19.0 H481.0 V81.8 H468.0"/></g>
            <g fill="currentColor" transform="translate(53.00,72.75) scale(0.085,-0.085)"><g transform="translate(0,0)"><path d="M229 -10Q147 -10 99.0 37.5Q51 85 51 162Q51 245 110.0 293.0Q169 341 276 341H384V376Q384 407 362.5 424.5Q341 442 301 442Q263 442 240.5 427.0Q218 412 216 383H74Q77 464 138.0 512.0Q199 560 303 560Q412 560 473.0 510.0Q534 460 534 370V0H389V95Q379 46 336.5 18.0Q294 -10 229 -10ZM282 110Q329 110 356.5 133.5Q384 157 384 195V251H279Q239 251 217.5 231.5Q196 212 196 181Q196 150 218.5 130.0Q241 110 282 110Z"/></g><g transform="translate(600,0)"><path d="M66 -180V550H211V450Q229 502 268.0 531.0Q307 560 362 560Q415 560 455.0 532.5Q495 505 517.5 457.0Q540 409 540 345V205Q540 142 517.5 93.5Q495 45 455.0 17.5Q415 -10 362 -10Q308 -10 269.0 18.5Q230 47 212 98L216 -30V-180ZM304 120Q345 120 367.5 143.5Q390 167 390 210V340Q390 384 367.5 407.0Q345 430 304 430Q261 430 238.5 405.5Q216 381 216 335V215Q216 169 238.5 144.5Q261 120 304 120Z"/></g><g transform="translate(1200,0)"><path d="M301 -10Q228 -10 173.5 17.5Q119 45 89.5 94.5Q60 144 60 210V340Q60 406 89.5 455.5Q119 505 173.5 532.5Q228 560 301 560Q374 560 427.5 532.5Q481 505 510.5 455.5Q540 406 540 340V238H203V210Q203 158 227.5 134.0Q252 110 301 110Q333 110 357.0 120.0Q381 130 385 150H532Q516 78 453.0 34.0Q390 -10 301 -10ZM203 340V327L397 329V342Q397 393 373.5 421.0Q350 449 301 449Q252 449 227.5 420.0Q203 391 203 340Z"/></g><g transform="translate(1800,0)"><path d="M26 0 216 285 39 550H206L274 440Q282 427 289.0 411.0Q296 395 300 385Q304 395 311.0 411.0Q318 427 326 440L394 550H561L385 285L574 0H407L327 130Q319 144 311.5 159.5Q304 175 300 185Q296 175 289.0 159.5Q282 144 274 130L193 0Z"/></g><g transform="translate(2400,0)"><path d="M300 252Q255 252 227.0 280.0Q199 308 199 353Q199 398 227.0 426.0Q255 454 300 454Q345 454 373.0 426.0Q401 398 401 353Q401 308 373.0 280.0Q345 252 300 252Z"/></g><g transform="translate(3000,0)"><path d="M300 -10Q227 -10 173.0 17.5Q119 45 89.5 94.5Q60 144 60 210V340Q60 406 89.5 455.5Q119 505 173.0 532.5Q227 560 300 560Q374 560 427.5 532.5Q481 505 510.5 455.5Q540 406 540 340V210Q540 144 510.5 94.5Q481 45 427.5 17.5Q374 -10 300 -10ZM300 120Q344 120 367.0 143.5Q390 167 390 210V340Q390 384 367.0 407.0Q344 430 300 430Q257 430 233.5 407.0Q210 384 210 340V210Q210 167 233.5 143.5Q257 120 300 120Z"/></g><g transform="translate(3600,0)"><path d="M66 0V550H211V450Q223 500 262.5 530.0Q302 560 359 560Q439 560 487.5 504.0Q536 448 536 355V0H386V340Q386 383 363.5 406.5Q341 430 301 430Q261 430 238.5 406.5Q216 383 216 340V0Z"/></g><g transform="translate(4200,0)"><path d="M301 -10Q228 -10 173.5 17.5Q119 45 89.5 94.5Q60 144 60 210V340Q60 406 89.5 455.5Q119 505 173.5 532.5Q228 560 301 560Q374 560 427.5 532.5Q481 505 510.5 455.5Q540 406 540 340V238H203V210Q203 158 227.5 134.0Q252 110 301 110Q333 110 357.0 120.0Q381 130 385 150H532Q516 78 453.0 34.0Q390 -10 301 -10ZM203 340V327L397 329V342Q397 393 373.5 421.0Q350 449 301 449Q252 449 227.5 420.0Q203 391 203 340Z"/></g></g>
          </svg>
        </router-link>
      </div>

      <!-- Desktop Navigation -->
      <div class="hidden items-center gap-8 md:flex">
        <template v-for="item in navItems" :key="item.key">
          <a
            v-if="item.type === 'anchor'"
            :href="`#${item.target}`"
            class="text-base font-medium text-gray-600 transition-colors hover:text-gray-900 dark:text-dark-300 dark:hover:text-white"
            @click.prevent="scrollToSection(item.target)"
          >
            {{ t(item.label) }}
          </a>
          <a
            v-else-if="item.type === 'external' && item.href"
            :href="item.href"
            target="_blank"
            rel="noopener noreferrer"
            class="text-base font-medium text-gray-600 transition-colors hover:text-gray-900 dark:text-dark-300 dark:hover:text-white"
          >
            {{ t(item.label) }}
          </a>
        </template>
      </div>

      <div class="flex items-center gap-3">
        <LocaleSwitcher />

        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
          :title="t('home.viewDocs')"
        >
          <Icon name="book" size="md" />
        </a>

        <button
          class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
          :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          @click="toggleTheme"
        >
          <Icon v-if="isDark" name="sun" size="md" />
          <Icon v-else name="moon" size="md" />
        </button>

        <!-- Mobile Hamburger Menu -->
        <button
          class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white md:hidden"
          :title="t('home.nav.menu')"
          @click="toggleMobileMenu"
        >
          <svg
            class="h-6 w-6"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="2"
          >
            <path
              v-if="!mobileMenuOpen"
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M4 6h16M4 12h16M4 18h16"
            />
            <path
              v-else
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M6 18L18 6M6 6l12 12"
            />
          </svg>
        </button>

        <router-link
          v-if="isAuthenticated"
          :to="dashboardPath"
          class="hidden md:inline-flex items-center gap-1.5 rounded-xl border-2 border-gray-200 py-2 pl-2.5 pr-3 transition-colors hover:border-gray-400 dark:border-dark-700 dark:hover:border-dark-400 md:pl-2 md:pr-3.5"
        >
          <span
            class="flex h-5 w-5 items-center justify-center rounded-full bg-gradient-to-br from-primary-400 to-primary-600 text-[10px] font-semibold text-white"
          >
            {{ userInitial }}
          </span>
          <span class="text-xs font-medium text-gray-900 dark:text-white">{{ t('home.dashboard') }}</span>
          <Icon name="externalLink" size="xs" class="text-gray-400" :stroke-width="2" />
        </router-link>
        <router-link
          v-else
          to="/login"
          class="hidden md:inline-flex items-center rounded-xl border-2 border-gray-200 px-3 py-2 text-sm font-medium transition-colors hover:border-gray-400 dark:border-dark-700 dark:hover:border-dark-400 md:px-4 md:text-base"
        >
          {{ t('home.login') }}
        </router-link>
      </div>
    </nav>
  </header>

  <!-- Mobile Sidebar Overlay -->
  <transition name="fade">
    <div
      v-if="mobileMenuOpen"
      class="fixed inset-0 z-40 bg-black/50 md:hidden"
      @click="closeMobileMenu"
    ></div>
  </transition>

  <!-- Mobile Sidebar -->
  <transition name="slide">
    <aside
      v-if="mobileMenuOpen"
      class="fixed inset-y-0 right-0 z-50 w-64 bg-white shadow-xl dark:bg-dark-900 md:hidden"
    >
      <div class="flex h-full flex-col">
        <!-- Sidebar Header -->
        <div class="flex items-center justify-between border-b border-gray-200 px-4 py-4 dark:border-dark-700">
          <span class="text-lg font-semibold text-gray-900 dark:text-white">Menu</span>
          <button
            class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            @click="closeMobileMenu"
          >
            <svg
              class="h-6 w-6"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <!-- Navigation Links -->
        <nav class="flex-1 overflow-y-auto px-4 py-6">
          <div class="space-y-2">
            <template v-for="item in navItems" :key="item.key">
              <a
                v-if="item.type === 'anchor'"
                :href="`#${item.target}`"
                class="flex items-center rounded-lg px-4 py-3 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-100 dark:text-dark-200 dark:hover:bg-dark-800"
                @click.prevent="scrollToSection(item.target)"
              >
                <svg
                  class="mr-3 h-5 w-5 text-gray-400"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <path stroke-linecap="round" stroke-linejoin="round" :d="item.icon" />
                </svg>
                {{ t(item.label) }}
              </a>
              <a
                v-else-if="item.type === 'external' && item.href"
                :href="item.href"
                target="_blank"
                rel="noopener noreferrer"
                class="flex items-center rounded-lg px-4 py-3 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-100 dark:text-dark-200 dark:hover:bg-dark-800"
                @click="closeMobileMenu"
              >
                <svg
                  class="mr-3 h-5 w-5 text-gray-400"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <path stroke-linecap="round" stroke-linejoin="round" :d="item.icon" />
                </svg>
                {{ t(item.label) }}
              </a>
            </template>
          </div>

          <!-- Divider -->
          <div class="my-6 border-t border-gray-200 dark:border-dark-700"></div>

          <!-- Additional Actions -->
          <div class="space-y-2">
            <button
              class="flex w-full items-center rounded-lg px-4 py-3 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-100 dark:text-dark-200 dark:hover:bg-dark-800"
              @click="toggleTheme"
            >
              <svg class="mr-3 h-5 w-5 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path v-if="isDark" stroke-linecap="round" stroke-linejoin="round" d="M12 3v2.25m6.364.386l-1.591 1.591M21 12h-2.25m-.386 6.364l-1.591-1.591M12 18.75V21m-4.773-4.227l-1.591 1.591M5.25 12H3m4.227-4.773L5.636 5.636M15.75 12a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0z" />
                <path v-else stroke-linecap="round" stroke-linejoin="round" d="M21.752 15.002A9.718 9.718 0 0118 15.75c-5.385 0-9.75-4.365-9.75-9.75 0-1.33.266-2.597.748-3.752A9.753 9.753 0 003 11.25C3 16.635 7.365 21 12.75 21a9.753 9.753 0 009.002-5.998z" />
              </svg>
              {{ isDark ? t('home.switchToLight') : t('home.switchToDark') }}
            </button>
          </div>
        </nav>

        <!-- Sidebar Footer -->
        <div class="border-t border-gray-200 px-4 py-4 dark:border-dark-700">
          <router-link
            v-if="isAuthenticated"
            :to="dashboardPath"
            class="flex w-full items-center justify-center gap-2 rounded-xl bg-primary-500 px-4 py-3 text-sm font-medium text-white transition-colors hover:bg-primary-600"
            @click="closeMobileMenu"
          >
            <span
              class="flex h-5 w-5 items-center justify-center rounded-full bg-white/20 text-[10px] font-semibold"
            >
              {{ userInitial }}
            </span>
            {{ t('home.dashboard') }}
          </router-link>
          <router-link
            v-else
            to="/login"
            class="flex w-full items-center justify-center rounded-xl bg-primary-500 px-4 py-3 text-sm font-medium text-white transition-colors hover:bg-primary-600"
            @click="closeMobileMenu"
          >
            {{ t('home.login') }}
          </router-link>
        </div>
      </div>
    </aside>
  </transition>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore, useAuthStore } from '@/stores'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const docUrl = computed(() => appStore.cachedPublicSettings?.doc_url || appStore.docUrl || '')
const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))
const userInitial = computed(() => {
  const user = authStore.user
  if (!user?.email) return ''
  return user.email.charAt(0).toUpperCase()
})

// Navigation items - single source of truth
const navItems = computed(() => [
  {
    key: 'architecture',
    label: 'home.nav.architecture',
    type: 'anchor',
    target: 'architecture',
    icon: 'M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z'
  },
  {
    key: 'features',
    label: 'home.nav.features',
    type: 'anchor',
    target: 'personal',
    icon: 'M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z'
  },
  {
    key: 'price',
    label: 'home.nav.price',
    type: 'anchor',
    target: 'pricing',
    icon: 'M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z'
  },
  {
    key: 'trust',
    label: 'home.nav.trust',
    type: 'anchor',
    target: 'trust',
    icon: 'M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z'
  },
  {
    key: 'document',
    label: 'home.nav.document',
    type: 'external',
    href: docUrl.value || undefined,
    icon: 'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z'
  }
] as const)

const isDark = ref(document.documentElement.classList.contains('dark'))
const mobileMenuOpen = ref(false)

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  ) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

function toggleMobileMenu() {
  mobileMenuOpen.value = !mobileMenuOpen.value
  document.body.style.overflow = mobileMenuOpen.value ? 'hidden' : ''
}

function closeMobileMenu() {
  mobileMenuOpen.value = false
  document.body.style.overflow = ''
}

function scrollToSection(sectionId: string) {
  closeMobileMenu()
  const element = document.getElementById(sectionId)
  if (element) {
    element.scrollIntoView({ behavior: 'smooth' })
  }
}

onMounted(() => {
  initTheme()
  authStore.checkAuth()
})
</script>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

.slide-enter-active,
.slide-leave-active {
  transition: transform 0.3s ease;
}

.slide-enter-from,
.slide-leave-to {
  transform: translateX(100%);
}
</style>
