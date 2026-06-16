<template>
  <header
    class="fixed inset-x-0 top-0 z-20 h-16 border-b border-gray-200/80 bg-white/80 px-4 py-3 backdrop-blur-sm backdrop-saturate-100 dark:border-dark-700/80 dark:bg-dark-900/80 md:h-18 md:px-6"
  >
    <nav class="mx-auto flex max-w-6xl items-center justify-between">
      <div class="flex items-center">
        <router-link to="/" aria-label="PublicAI home">
          <svg class="h-6 w-auto md:h-8" viewBox="0 0 261 64" fill="none">
            <g fill="currentColor">
              <path
                fill-rule="evenodd"
                clip-rule="evenodd"
                d="M63.7469 0.18735C65.6406 2.08312 52.9476 17.8629 35.3961 35.4325C17.8446 53.0023 2.08098 65.7084 0.187157 63.8127C-0.887694 62.7366 2.73622 57.1882 9.2803 49.3788C8.57334 48.4358 7.92843 47.4514 7.34827 46.4332C4.95312 42.2284 3.66165 37.4366 3.66165 32.5115C3.66165 25.058 6.61951 17.9098 11.8845 12.6394C17.1495 7.36893 24.2904 4.40803 31.7363 4.40803C36.6563 4.40803 41.4431 5.70082 45.6436 8.09848C46.6346 8.66421 47.5931 9.29146 48.5131 9.97777C56.7419 2.99477 62.6351 -0.925704 63.7469 0.18735ZM18.9909 38.4507C21.9458 35.2808 25.1515 31.9574 28.538 28.5674C31.6491 25.4531 34.7039 22.4917 37.6354 19.7373C35.8035 18.8896 33.7931 18.4384 31.7363 18.4384C28.0077 18.4384 24.4318 19.9211 21.7953 22.5603C19.1588 25.1995 17.6776 28.7791 17.6776 32.5115C17.6776 34.5832 18.1339 36.6079 18.9909 38.4507ZM16.1186 55.8631C16.2538 55.9535 16.3897 56.0431 16.5263 56.1311C21.0265 59.0349 26.301 60.6139 31.7381 60.6139C39.184 60.6139 46.3247 57.6531 51.5897 52.3827C56.8547 47.1123 59.8129 39.9639 59.8129 32.5104C59.8129 27.0677 58.2355 21.7878 55.3346 17.2829C55.2728 17.1873 55.211 17.0921 55.148 16.9973C53.9346 18.5738 52.6128 20.2201 51.1939 21.9206C52.9436 25.1418 53.884 28.7776 53.884 32.5104C53.884 38.3899 51.5508 44.0285 47.3976 48.1859C43.2445 52.3433 37.6116 54.679 31.7381 54.679C27.9738 54.679 24.3083 53.7195 21.0679 51.9363C19.3537 53.3522 17.6983 54.6649 16.1186 55.8631ZM23.0833 50.2467C25.7537 51.5522 28.71 52.2497 31.7381 52.2497C36.9679 52.2497 41.9833 50.17 45.6813 46.4682C49.3793 42.7664 51.4569 37.7455 51.4569 32.5104C51.4569 29.5096 50.7745 26.5793 49.4953 23.9275C48.0952 25.5595 46.6129 27.2337 45.0583 28.9372C45.3678 30.0928 45.5284 31.2933 45.5284 32.5104C45.5284 36.1715 44.0754 39.6827 41.4892 42.2714C38.9031 44.8602 35.3955 46.3147 31.7381 46.3147C30.5023 46.3147 29.2835 46.1488 28.1117 45.8289C26.3991 47.3807 24.7185 48.8561 23.0833 50.2467Z"
              />
              <path
                fill-rule="evenodd"
                clip-rule="evenodd"
                d="M135.766 50.3855V13.6145H141.537V24.298H149.973C155.137 24.298 157.718 26.915 157.718 32.1491V42.5344C157.718 47.7685 155.137 50.3855 149.973 50.3855H135.766ZM141.537 45.3171H149.677C151.19 45.3171 151.947 44.5551 151.947 43.0313V31.6522C151.947 30.1283 151.19 29.3664 149.677 29.3664H141.537V45.3171Z"
              />
              <path
                fill-rule="evenodd"
                clip-rule="evenodd"
                d="M75.7981 16.099V50.3855H81.8166V38.8573H92.1764C97.5371 38.8573 100.218 36.174 100.218 30.8074V24.1986C100.218 18.7989 97.5371 16.099 92.1764 16.099H75.7981ZM92.0284 33.3416H81.8166V21.6147H92.0284C93.4754 21.6147 94.199 22.3269 94.199 23.7514V31.1553C94.199 32.6129 93.4754 33.3416 92.0284 33.3416Z"
              />
              <path d="M106.422 24.298V42.5344C106.422 47.7685 109.02 50.3855 114.216 50.3855H128.375V24.298H122.603V45.3171H114.463C112.95 45.3171 112.194 44.5551 112.194 43.0313V24.298H106.422Z" />
              <path d="M164.522 13.6145V50.3855H170.294V13.6145H164.522Z" />
              <path d="M177.581 19.7761V13.8629H183.648V19.7761H177.581Z" />
              <path d="M177.729 50.3855V24.298H183.5V50.3855H177.729Z" />
              <path d="M190.486 32.1491V42.5344C190.486 47.7685 193.084 50.3855 198.28 50.3855H210.219V45.3171H198.527C197.014 45.3171 196.258 44.5551 196.258 43.0313V31.6522C196.258 30.1283 197.014 29.3664 198.527 29.3664H210.219V24.298H198.28C193.084 24.298 190.486 26.915 190.486 32.1491Z" />
              <path
                fill-rule="evenodd"
                clip-rule="evenodd"
                d="M227.883 13.7895L215.954 50.2105H222.824L225.353 42.1873H240.375L242.852 50.2105H249.622L237.898 13.7895H227.883ZM238.636 36.3283H227.145L232.363 19.6485H233.365L238.636 36.3283Z"
              />
              <path d="M254.57 13.7895V50.2105H261V13.7895H254.57Z" />
            </g>
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
    key: 'price',
    label: 'home.nav.price',
    type: 'anchor',
    target: 'pricing',
    icon: 'M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z'
  },
  {
    key: 'personal',
    label: 'home.nav.personal',
    type: 'anchor',
    target: 'features',
    icon: 'M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z'
  },
  {
    key: 'team',
    label: 'home.nav.team',
    type: 'anchor',
    target: 'models',
    icon: 'M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z'
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
