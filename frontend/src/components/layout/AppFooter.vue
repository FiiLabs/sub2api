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

const { t } = useI18n()

const supportEmail = 'Support@publicai.io';

const socialLinks = [
  {
    label: 'x',
    href: 'https://x.com/PublicAI_',
    path: 'M22 0c12.15 0 22 9.85 22 22s-9.85 22-22 22S0 34.15 0 22 9.85 0 22 0m7.771 10a.273.273 0 0 0-.23.105q-3.047 3.488-6.097 6.975l-.514.585-.083-.105q-2.821-3.725-5.64-7.455a.237.237 0 0 0-.214-.105q-4.055.004-8.109.003H8.73l10.034 13.123-9.502 10.862c.035.005.05.009.066.009q1.962.001 3.925-.002c.044 0 .1-.033.131-.067q.23-.256.455-.517l5.105-5.833 1.721-1.966.106.137q3.11 4.064 6.216 8.129c.07.092.141.122.254.122q3.932-.004 7.863-.003h.168l-10.46-13.83 8.891-10.165H31.66ZM15.77 12.305a.236.236 0 0 1 .213.107q3.599 4.764 7.201 9.526l7.142 9.447.131.175h-1.14l-.99.002a.216.216 0 0 1-.194-.096 55796 55796 0 0 0-14.534-19l-.115-.151c.057-.003.095-.007.133-.007.717 0 1.435.002 2.153-.003Z'
  },
  {
    label: 'telegram',
    href: 'https://t.me/public_ai01',
    path: 'M22 0c12.15 0 22 9.85 22 22s-9.85 22-22 22S0 34.15 0 22 9.85 0 22 0m9.28 13.357-21.213 8.176c-1.45.564-1.424 1.375-.246 1.743l5.427 1.694 12.596-7.931c.59-.393 1.13-.171.688.222l-10.189 9.207-.393 5.599c.565 0 .811-.246 1.105-.541l2.651-2.554 5.5 4.052c1.007.564 1.72.27 1.99-.934l3.608-17.014c.319-1.497-.541-2.087-1.523-1.719Z'
  },
  {
    label: 'discord',
    href: 'https://discord.gg/publicai',
    path: 'M22 0c12.15 0 22 9.85 22 22s-9.85 22-22 22S0 34.15 0 22 9.85 0 22 0m4.268 9.634a.094.094 0 0 0-.042.04 18.12 18.12 0 0 0-.766 1.597 22.7 22.7 0 0 0-6.916 0q-.346-.821-.778-1.597a.098.098 0 0 0-.1-.047 24.56 24.56 0 0 0-6.157 1.936.088.088 0 0 0-.04.034c-3.921 5.94-4.996 11.734-4.468 17.454a.108.108 0 0 0 .04.073 24.95 24.95 0 0 0 7.554 3.872.095.095 0 0 0 .106-.036 18.18 18.18 0 0 0 1.546-2.548.099.099 0 0 0-.018-.114.095.095 0 0 0-.035-.021 16.4 16.4 0 0 1-2.36-1.14.1.1 0 0 1-.04-.126.1.1 0 0 1 .029-.038q.241-.18.469-.373a.093.093 0 0 1 .097-.013c4.95 2.292 10.31 2.292 15.203.001a.094.094 0 0 1 .098.013c.151.124.31.252.47.372a.096.096 0 0 1 .038.083.102.102 0 0 1-.046.08 15.4 15.4 0 0 1-2.36 1.137.097.097 0 0 0-.063.097.109.109 0 0 0 .011.041c.454.893.973 1.741 1.544 2.548a.096.096 0 0 0 .106.036 24.9 24.9 0 0 0 7.565-3.872.096.096 0 0 0 .04-.07c.63-6.614-1.056-12.36-4.473-17.453a.076.076 0 0 0-.039-.038 24.75 24.75 0 0 0-6.157-1.934.092.092 0 0 0-.058.007m-9.284 9.751c1.525 0 2.742 1.4 2.718 3.092 0 1.704-1.204 3.09-2.718 3.09-1.49 0-2.719-1.386-2.719-3.091 0-1.703 1.205-3.09 2.719-3.09Zm10.051 0c1.525 0 2.743 1.4 2.72 3.092 0 1.704-1.193 3.09-2.72 3.09-1.49 0-2.718-1.386-2.718-3.091 0-1.703 1.203-3.09 2.718-3.09Z'
  }
] as const

const currentYear = computed(() => new Date().getFullYear())
</script>
