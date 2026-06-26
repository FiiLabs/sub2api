<template>
  <footer class="w-full pt-10 md:pt-20">
    <div class="relative container mx-auto flex flex-col px-4 sm:px-6 lg:px-8">
      <h3 class="text-sm font-semibold text-gray-900 md:text-lg dark:text-white">
        {{ t('home.footer.contactUs') }}
      </h3>

      <section class="relative my-2.5 flex w-full flex-col justify-between gap-2.5 xl:flex-row xl:items-center">
        <a
          :href="`mailto:${supportEmail}`"
          class="flex w-min items-center self-stretch rounded-lg border border-gray-200 bg-white/80 p-2 shadow-sm backdrop-blur md:px-4 dark:border-dark-700 dark:bg-dark-900/80"
        >
          <Icon name="mail" size="sm" class="flex-shrink-0 text-primary-600 dark:text-primary-400 md:h-6 md:w-6" />
          <p class="ml-2 whitespace-nowrap text-sm text-gray-700 md:text-base dark:text-dark-200">
            {{ supportEmail }}
          </p>
        </a>

        <address class="flex w-max items-center gap-2 rounded-lg border border-gray-200 bg-white/80 p-2 shadow-sm backdrop-blur md:px-4 dark:border-dark-700 dark:bg-dark-900/80">
          <p class="text-sm font-normal not-italic text-gray-700 md:mr-2 md:text-lg dark:text-dark-200">
            {{ t('home.footer.stayConnected') }}
          </p>
          <a
            v-for="item in socialLinks"
            :key="item.label"
            :href="item.href"
            target="_blank"
            rel="external noreferrer"
            class="text-gray-700 transition hover:text-primary-600 dark:text-dark-200 dark:hover:text-primary-400"
            :aria-label="`${item.label} link`"
          >
            <svg
              class="size-4 md:size-6"
              viewBox="0 0 44 44"
              aria-hidden="true"
              focusable="false"
            >
              <path fill="currentColor" :d="item.path" />
            </svg>
          </a>
        </address>
      </section>

      <span class="mb-2.5 mt-5 text-center text-sm text-gray-500 md:mt-7 md:text-base dark:text-dark-400">
        © {{ currentYear }} PublicAI Foundation, Cayman Islands All Rights Reserved
      </span>
    </div>
  </footer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { supportEmail, socialLinks } from './footerLinks'

const { t } = useI18n()

const currentYear = computed(() => new Date().getFullYear())
</script>
