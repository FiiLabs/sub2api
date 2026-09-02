<template>
  <footer class="w-full pt-10 md:pt-20">
    <div class="relative container mx-auto flex flex-col px-4 sm:px-6 lg:px-8">
      <h3 class="text-center text-sm font-semibold text-gray-900 md:text-lg dark:text-white">
        {{ t('home.footer.contactUs') }}
      </h3>

      <!--
        联系方式做成**一条轨**，而不是左右分开的两张卡。

        原来邮箱在最左、社交在最右，中间被 justify-between 撑开一整个容器宽度：
        在宽屏上那是两块彼此无关的东西，读者要横扫过去才发现右边还有内容。
        联系方式是同一件事的几个入口，挨在一起才读得出「这是一组」。

        视觉重量靠三层叠出来：底下一层渐变光晕给它在页面上「亮一下」，
        中间是玻璃质感的容器，最上面每个入口自己有 hover 态。
      -->
      <section class="relative mt-4 flex justify-center md:mt-5">
        <div class="group/rail relative">
          <!-- 光晕。纯装饰，不参与布局也不吃指针事件。 -->
          <div
            class="pointer-events-none absolute -inset-1 rounded-[20px] bg-gradient-to-r from-primary-500/25 via-primary-400/10 to-primary-500/25 opacity-70 blur-lg transition-opacity duration-500 group-hover/rail:opacity-100"
            aria-hidden="true"
          ></div>

          <div
            class="relative flex flex-wrap items-center justify-center gap-1 rounded-2xl border border-gray-200 bg-white/85 p-1.5 shadow-card backdrop-blur-sm dark:border-dark-700 dark:bg-dark-900/85"
          >
            <a
              :href="`mailto:${supportEmail}`"
              class="flex items-center gap-2 rounded-xl px-3 py-2 text-gray-700 transition-colors hover:bg-primary-50 hover:text-primary-700 dark:text-dark-200 dark:hover:bg-primary-500/10 dark:hover:text-primary-300"
              :aria-label="`Email ${supportEmail}`"
              data-testid="footer-email"
            >
              <Icon name="mail" size="sm" class="flex-shrink-0 text-primary-600 dark:text-primary-400" />
              <span class="whitespace-nowrap text-sm md:text-base">{{ supportEmail }}</span>
            </a>

            <!-- 分隔线只在同一行时才有意义；换行后两段各自成组，竖线会变成孤零零的一竖。 -->
            <span
              class="mx-1 hidden h-6 w-px bg-gray-200 sm:block dark:bg-dark-700"
              aria-hidden="true"
            ></span>

            <!-- 「保持联系」不再占版面：图标本身已经说明是什么，但读屏器需要这个组名。 -->
            <span class="sr-only">{{ t('home.footer.stayConnected') }}</span>

            <a
              v-for="item in socialLinks"
              :key="item.label"
              :href="item.href"
              target="_blank"
              rel="external noreferrer"
              class="flex h-10 w-10 items-center justify-center rounded-xl text-gray-500 transition-all hover:-translate-y-0.5 hover:bg-primary-50 hover:text-primary-600 dark:text-dark-300 dark:hover:bg-primary-500/10 dark:hover:text-primary-400"
              :aria-label="item.name"
              :title="item.name"
              :data-testid="`footer-social-${item.label}`"
            >
              <svg class="size-[18px]" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <path fill="currentColor" :d="item.path" />
              </svg>
            </a>
          </div>
        </div>
      </section>

      <span class="mt-6 text-center text-sm text-gray-500 md:mt-8 md:text-base dark:text-dark-400">
        © {{ currentYear }} ApexOne. {{ t('home.footer.allRightsReserved') }}
      </span>
      <p class="mb-2.5 mt-2 text-center text-[11px] text-gray-400 dark:text-dark-500">
        {{ t('home.footer.trademarkNotice') }}
      </p>
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
