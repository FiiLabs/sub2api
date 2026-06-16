<template>
  <div
    class="relative flex min-h-screen flex-col overflow-hidden bg-gradient-to-br from-gray-50 via-primary-50/30 to-gray-100 dark:from-dark-950 dark:via-dark-900 dark:to-dark-950 pt-16 md:pt-[72px]">
    <!-- Background Decorations -->
    <div class="pointer-events-none absolute inset-0 overflow-hidden">
      <div class="absolute -right-40 -top-40 h-96 w-96 rounded-full bg-primary-400/20 blur-3xl"></div>
      <div class="absolute -bottom-40 -left-40 h-96 w-96 rounded-full bg-primary-500/15 blur-3xl"></div>
      <div class="absolute left-1/3 top-1/4 h-72 w-72 rounded-full bg-primary-300/10 blur-3xl"></div>
      <div class="absolute bottom-1/4 right-1/4 h-64 w-64 rounded-full bg-primary-400/10 blur-3xl"></div>
      <div
        class="absolute inset-0 bg-[linear-gradient(rgba(20,184,166,0.03)_1px,transparent_1px),linear-gradient(90deg,rgba(20,184,166,0.03)_1px,transparent_1px)] bg-[size:64px_64px]">
      </div>
    </div>

    <Header />

    <!-- Main Content -->
    <main class="relative z-10 flex-1 px-6 py-16">
      <div class="mx-auto max-w-6xl">
        <!-- Hero Section - Left/Right Layout -->
        <div class="mb-12 flex flex-col items-center justify-between gap-12 lg:flex-row lg:gap-16">
          <!-- Left: Text Content -->
          <div class="flex-1 text-center lg:text-left">
            <h1 class="mb-4 text-4xl font-bold text-gray-900 dark:text-white md:text-5xl lg:text-6xl">
              {{ t('home.landing.hero.titlePrefix') }} <span
                class="bg-gradient-to-r from-primary-500 to-primary-600 bg-clip-text [-webkit-text-fill-color:transparent]">{{ t('home.landing.hero.titleHighlight') }}</span>
              {{ t('home.landing.hero.titleSuffix') }}
            </h1>
            <p class="mb-8 text-lg text-gray-600 dark:text-dark-300 md:text-xl">
              {{ siteSubtitle }}
            </p>

            <!-- CTA Button -->
            <div>
              <router-link :to="isAuthenticated ? dashboardPath : '/login'"
                class="btn btn-primary px-8 py-3 text-base shadow-lg shadow-primary-500/30">
                {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
                <Icon name="arrowRight" size="md" class="ml-2" :stroke-width="2" />
              </router-link>
            </div>
          </div>

          <!-- Right: Terminal Animation -->
          <div class="flex flex-1 justify-center lg:justify-end">
            <img class="w-full h-auto rounded-xl" src="/publicai_hero_cascade.svg"
              alt="PublicAI Gateway routing requests from a developer node to Claude, GPT, and more LLMs">
          </div>
        </div>

        <!-- Feature Tags - Centered -->
        <div class="mb-12 flex flex-wrap items-center justify-center gap-4 md:gap-6">
          <div
            class="inline-flex items-center gap-2.5 rounded-full border border-gray-200/50 bg-white/80 px-5 py-2.5 shadow-sm backdrop-blur-sm dark:border-dark-700/50 dark:bg-dark-800/80">
            <Icon name="shield" size="sm" class="text-primary-500" />
            <span class="text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('home.landing.badges.uptime') }}</span>
          </div>
          <div
            class="inline-flex items-center gap-2.5 rounded-full border border-gray-200/50 bg-white/80 px-5 py-2.5 shadow-sm backdrop-blur-sm dark:border-dark-700/50 dark:bg-dark-800/80">
            <Icon name="dollar" size="sm" class="text-primary-500" />
            <span class="text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('home.landing.badges.lowerCost') }}</span>
          </div>
          <div
            class="inline-flex items-center gap-2.5 rounded-full border border-gray-200/50 bg-white/80 px-5 py-2.5 shadow-sm backdrop-blur-sm dark:border-dark-700/50 dark:bg-dark-800/80">
            <Icon name="code" size="sm" class="text-primary-500" />
            <span class="text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('home.landing.badges.compatible') }}</span>
          </div>
        </div>

        <!-- Statement -->
        <section
          v-reveal
          class="reveal-item mx-auto max-w-4xl py-10 text-center"
        >
          <h2 class="text-3xl font-bold leading-tight text-gray-900 dark:text-white md:text-5xl">
            {{ t('home.landing.statement.title') }}
          </h2>
          <p class="mx-auto mt-4 max-w-2xl text-lg leading-8 text-gray-600 dark:text-dark-300">
            {{ t('home.landing.statement.description') }}
          </p>
        </section>

        <!-- Features -->
        <section id="features" class="py-4">
          <div
            v-for="(feature, index) in featureRows"
            :key="feature.title"
            v-reveal
            class="reveal-item grid gap-8 border-t border-gray-200/70 py-10 dark:border-dark-700/70 lg:grid-cols-2 lg:items-center lg:gap-14"
            :style="{ transitionDelay: `${index * 80}ms` }"
          >
            <div :class="feature.reversed ? 'lg:order-2' : ''">
              <h3 class="mt-3 text-2xl font-semibold leading-tight text-gray-900 dark:text-white md:text-3xl">
                {{ feature.title }}
              </h3>
              <p class="mt-4 max-w-xl text-base leading-7 text-gray-600 dark:text-dark-300">
                {{ feature.description }}
              </p>
              <div class="mt-6 flex flex-wrap gap-2.5">
                <span v-for="tag in feature.tags" :key="tag"
                  class="inline-flex items-center gap-2 rounded-lg border border-gray-200 bg-white/70 px-3 py-2 font-mono text-xs font-medium text-gray-600 shadow-sm dark:border-dark-700 dark:bg-dark-800/70 dark:text-dark-300">
                  <span class="h-1.5 w-1.5 rounded-full bg-primary-500"></span>
                  {{ tag }}
                </span>
              </div>
            </div>

            <div
              class="relative overflow-hidden rounded-2xl border border-gray-200 bg-white/80 p-6 shadow-card backdrop-blur-sm dark:border-dark-700 dark:bg-dark-800/80"
              :class="feature.reversed ? 'lg:order-1' : ''">
              <div
                class="pointer-events-none absolute inset-0 bg-[radial-gradient(420px_200px_at_80%_-20%,rgba(123,97,255,0.16),transparent_70%)]">
              </div>

              <div v-if="feature.visual === 'uptime'" class="relative">
                <div class="mb-5 flex items-start justify-between gap-4">
                  <div>
                    <div class="text-4xl font-bold leading-none text-gray-900 dark:text-white">
                      99.99<span class="text-xl text-gray-400">%</span>
                    </div>
                    <div class="mt-1 font-mono text-xs text-gray-500 dark:text-dark-400">
                      {{ t('home.landing.reliability.uptimeWindow') }}
                    </div>
                  </div>
                  <span
                    class="inline-flex items-center gap-2 rounded-lg bg-emerald-500/10 px-3 py-1.5 font-mono text-xs font-medium text-emerald-500">
                    <span class="relative flex size-2">
                      <span
                        class="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75"></span>
                      <span class="relative inline-flex size-2 rounded-full bg-emerald-500"></span>
                    </span>
                    {{ t('home.landing.reliability.operational') }}
                  </span>
                </div>

                <div class="flex h-14 items-end gap-1">
                  <span v-for="(bar, barIndex) in uptimeBars" :key="barIndex"
                    class="reveal-bar flex-1 rounded-sm bg-emerald-400/85"
                    :class="bar.isDip ? 'bg-amber-400/90' : ''"
                    :style="{ height: `${bar.height}%`, transitionDelay: `${barIndex * 14}ms` }"></span>
                </div>

                <div v-for="service in healthyServices" :key="service"
                  class="mt-4 flex items-center justify-between border-t border-gray-100 pt-3 text-sm dark:border-dark-700">
                  <span class="font-mono text-gray-600 dark:text-dark-300">{{ service }}</span>
                  <span class="inline-flex items-center gap-2 font-mono text-xs text-emerald-500">
                    <span class="h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-400"></span>
                    {{ t('home.landing.reliability.healthy') }}
                  </span>
                </div>
              </div>

              <div v-else class="relative space-y-4">
                <div v-for="row in costRows" :key="row.label" class="flex items-center gap-4">
                  <span class="w-20 font-mono text-xs text-gray-500 dark:text-dark-400">
                    {{ row.label }}
                  </span>
                  <div class="h-8 flex-1 overflow-hidden rounded-lg bg-gray-100 dark:bg-dark-700">
                    <div
                      class="flex h-full items-center justify-end rounded-lg pr-3 font-mono text-xs font-bold text-white"
                      :class="row.fillClass" :style="{ width: row.width }">
                      {{ row.price }}
                    </div>
                  </div>
                </div>
                <div class="text-right font-mono text-xs text-gray-500 dark:text-dark-400">
                  {{ t('home.landing.costs.spendHint') }}
                </div>
              </div>
            </div>
          </div>
        </section>

        <!-- Pricing Comparison -->
        <section id="pricing" class="py-14">
          <div v-reveal class="reveal-item mx-auto mb-10 max-w-3xl text-center">
            <span
              class="font-mono text-xs font-medium uppercase tracking-[0.12em] text-primary-600 dark:text-primary-400">
              {{ t('home.landing.pricing.eyebrow') }}
            </span>
            <h2 class="mt-3 text-3xl font-bold leading-tight text-gray-900 dark:text-white md:text-4xl">
              {{ t('home.landing.pricing.title') }}
            </h2>
            <p class="mx-auto mt-4 max-w-2xl text-base leading-7 text-gray-600 dark:text-dark-300">
              {{ t('home.landing.pricing.subtitle') }}
            </p>
          </div>

          <div v-reveal class="reveal-item mx-auto max-w-5xl">
            <div class="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
              <div v-for="(model, index) in pricingCards" :key="model.name"
                class="relative overflow-hidden rounded-2xl border border-gray-200 bg-white/80 p-6 shadow-card backdrop-blur-sm transition hover:-translate-y-1 hover:shadow-lg dark:border-dark-700 dark:bg-dark-800/80"
                :style="{ transitionDelay: `${index * 60}ms` }">
                <!-- Logo decoration -->
                <div class="pointer-events-none absolute -right-3 -top-3 opacity-[0.07]">
                  <svg v-if="model.brand === 'gpt'" viewBox="0 0 24 24" class="size-24" fill="currentColor">
                    <path d="M22.282 9.821a5.985 5.985 0 0 0-.516-4.91 6.046 6.046 0 0 0-6.51-2.9A6.065 6.065 0 0 0 4.981 4.18a5.985 5.985 0 0 0-3.998 2.9 6.046 6.046 0 0 0 .743 7.097 5.98 5.98 0 0 0 .51 4.911 6.051 6.051 0 0 0 6.515 2.9A5.985 5.985 0 0 0 13.26 24a6.056 6.056 0 0 0 5.772-4.206 5.99 5.99 0 0 0 3.997-2.9 6.056 6.056 0 0 0-.747-7.073zM13.26 22.43a4.476 4.476 0 0 1-2.876-1.04l.141-.081 4.779-2.758a.795.795 0 0 0 .392-.681v-6.737l2.02 1.168a.071.071 0 0 1 .038.052v5.583a4.504 4.504 0 0 1-4.494 4.494zM3.6 18.304a4.47 4.47 0 0 1-.535-3.014l.142.085 4.783 2.759a.771.771 0 0 0 .78 0l5.843-3.369v2.332a.08.08 0 0 1-.033.062L9.74 19.95a4.5 4.5 0 0 1-6.14-1.646zM2.34 7.896a4.485 4.485 0 0 1 2.366-1.973V11.6a.766.766 0 0 0 .388.676l5.815 3.355-2.02 1.168a.076.076 0 0 1-.071 0l-4.83-2.786A4.504 4.504 0 0 1 2.34 7.872zm16.597 3.855l-5.833-3.387L15.119 7.2a.076.076 0 0 1 .071 0l4.83 2.791a4.494 4.494 0 0 1-.676 8.105v-5.678a.79.79 0 0 0-.407-.667zm2.01-3.023l-.141-.085-4.774-2.782a.776.776 0 0 0-.785 0L9.409 9.23V6.897a.066.066 0 0 1 .028-.061l4.83-2.787a4.5 4.5 0 0 1 6.68 4.66zm-12.64 4.135l-2.02-1.164a.08.08 0 0 1-.038-.057V6.075a4.5 4.5 0 0 1 7.375-3.453l-.142.08L8.704 5.46a.795.795 0 0 0-.393.681zm1.097-2.365l2.602-1.5 2.607 1.5v2.999l-2.597 1.5-2.607-1.5z" />
                  </svg>
                  <svg v-else viewBox="0 0 100 100" class="size-24" fill="#d97757">
                    <path d="m19.6 66.5 19.7-11 .3-1-.3-.5h-1l-3.3-.2-11.2-.3L14 53l-9.5-.5-2.4-.5L0 49l.2-1.5 2-1.3 2.9.2 6.3.5 9.5.6 6.9.4L38 49.1h1.6l.2-.7-.5-.4-.4-.4L29 41l-10.6-7-5.6-4.1-3-2-1.5-2-.6-4.2 2.7-3 3.7.3.9.2 3.7 2.9 8 6.1L37 36l1.5 1.2.6-.4.1-.3-.7-1.1L33 25l-6-10.4-2.7-4.3-.7-2.6c-.3-1-.4-2-.4-3l3-4.2L28 0l4.2.6L33.8 2l2.6 6 4.1 9.3L47 29.9l2 3.8 1 3.4.3 1h.7v-.5l.5-7.2 1-8.7 1-11.2.3-3.2 1.6-3.8 3-2L61 2.6l2 2.9-.3 1.8-1.1 7.7L59 27.1l-1.5 8.2h.9l1-1.1 4.1-5.4 6.9-8.6 3-3.5L77 13l2.3-1.8h4.3l3.1 4.7-1.4 4.9-4.4 5.6-3.7 4.7-5.3 7.1-3.2 5.7.3.4h.7l12-2.6 6.4-1.1 7.6-1.3 3.5 1.6.4 1.6-1.4 3.4-8.2 2-9.6 2-14.3 3.3-.2.1.2.3 6.4.6 2.8.2h6.8l12.6 1 3.3 2 1.9 2.7-.3 2-5.1 2.6-6.8-1.6-16-3.8-5.4-1.3h-.8v.4l4.6 4.5 8.3 7.5L89 80.1l.5 2.4-1.3 2-1.4-.2-9.2-7-3.6-3-8-6.8h-.5v.7l1.8 2.7 9.8 14.7.5 4.5-.7 1.4-2.6 1-2.7-.6-5.8-8-6-9-4.7-8.2-.5.4-2.9 30.2-1.3 1.5-3 1.2-2.5-2-1.4-3 1.4-6.2 1.6-8 1.3-6.4 1.2-7.9.7-2.6v-.2H49L43 72l-9 12.3-7.2 7.6-1.7.7-3-1.5.3-2.8L24 86l10-12.8 6-7.9 4-4.6-.1-.5h-.3L17.2 77.4l-4.7.6-2-2 .2-3 1-1 8-5.5Z" />
                  </svg>
                </div>

                <!-- Model info -->
                <div class="relative">
                  <div class="mb-1 flex items-center gap-2">
                    <span class="text-sm font-semibold text-gray-500 dark:text-dark-400">{{ model.provider }}</span>
                  </div>
                  <h3 class="text-xl font-bold text-gray-900 dark:text-white">{{ model.name }}</h3>

                  <!-- Input price -->
                  <div class="mt-4 space-y-1.5">
                    <div class="flex items-center justify-between text-xs">
                      <span class="text-gray-500 dark:text-dark-400">{{ t('home.landing.pricing.col.input') }}</span>
                      <span class="font-mono text-gray-400 line-through dark:text-dark-500">${{ model.officialInput }}/M</span>
                    </div>
                    <div class="flex items-center justify-between">
                      <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('home.landing.pricing.col.input') }}</span>
                      <span class="font-mono text-lg font-bold text-primary-600 dark:text-primary-400">${{ model.gatewayInput }}/M</span>
                    </div>

                  </div>

                  <!-- Divider -->
                  <div class="my-4 border-t border-gray-100 dark:border-dark-700"></div>

                  <!-- Output price -->
                  <div class="space-y-1.5">
                    <div class="flex items-center justify-between text-xs">
                      <span class="text-gray-500 dark:text-dark-400">{{ t('home.landing.pricing.col.output') }}</span>
                      <span class="font-mono text-gray-400 line-through dark:text-dark-500">${{ model.officialOutput }}/M</span>
                    </div>
                    <div class="flex items-center justify-between">
                      <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('home.landing.pricing.col.output') }}</span>
                      <span class="font-mono text-lg font-bold text-primary-600 dark:text-primary-400">${{ model.gatewayOutput }}/M</span>
                    </div>
                  </div>

                  <!-- badge -->
                  <div class="mt-4 flex justify-end">
                    <span
                      class="inline-flex items-center rounded-lg bg-emerald-500/10 px-2.5 py-1 font-mono text-xs font-bold text-emerald-600 dark:text-emerald-400">
                      {{ t('home.tags.realtimeBilling') }}
                    </span>
                  </div>
                </div>
              </div>
            </div>

            <div class="mt-6 flex justify-center">
              <span
                class="inline-flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-2 font-mono text-sm font-medium text-emerald-600 dark:text-emerald-400">
                <Icon name="dollar" size="sm" :stroke-width="2" />
                {{ t('home.landing.pricing.save') }}
              </span>
            </div>
          </div>
        </section>

        <!-- Models -->
        <section id="models" class="py-14">
          <div v-reveal class="reveal-item mx-auto mb-10 max-w-3xl text-center">
            <span
              class="font-mono text-xs font-medium uppercase tracking-[0.12em] text-primary-600 dark:text-primary-400">
              {{ t('home.landing.models.eyebrow') }}
            </span>
            <h2 class="mt-3 text-3xl font-bold leading-tight text-gray-900 dark:text-white md:text-4xl">
              {{ t('home.landing.models.title') }}
            </h2>
          </div>

          <div class="mx-auto grid max-w-4xl gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <div v-for="(model, index) in modelCards" :key="model.name"
              v-reveal
              class="reveal-item flex items-center gap-4 rounded-2xl border bg-white/80 p-4 shadow-card transition hover:-translate-y-1 hover:border-primary-300 dark:bg-dark-800/80"
              :class="[
                model.available
                  ? 'border-gray-200 dark:border-dark-700'
                  : 'border-dashed border-gray-300 opacity-75 dark:border-dark-600'
              ]" :style="{ transitionDelay: `${index * 60}ms` }">
              <div class="flex h-11 w-11 flex-none items-center justify-center rounded-xl text-lg font-bold text-white"
                :class="model.iconClass">
                {{ model.initial }}
              </div>
              <div class="min-w-0">
                <div class="font-semibold text-gray-900 dark:text-white">{{ model.name }}</div>
                <div class="mt-0.5 font-mono text-[11px] uppercase tracking-wider"
                  :class="model.available ? 'text-emerald-500' : 'text-gray-400'">
                  {{ model.available ? t('home.landing.models.available') : t('home.landing.models.comingSoon') }}
                </div>
              </div>
            </div>
          </div>

          <p class="mt-8 text-center text-base text-gray-600 dark:text-dark-300">
            <b class="font-semibold text-gray-900 dark:text-white">{{ t('home.landing.models.noLockIn') }}</b>
            {{ t('home.landing.models.switchHint') }}
          </p>
        </section>

        <!-- Production -->
        <section class="py-14">
          <div v-reveal class="reveal-item mx-auto mb-10 max-w-3xl text-center">
            <span
              class="font-mono text-xs font-medium uppercase tracking-[0.12em] text-primary-600 dark:text-primary-400">
              {{ t('home.landing.production.eyebrow') }}
            </span>
            <h2 class="mt-3 text-3xl font-bold leading-tight text-gray-900 dark:text-white md:text-4xl">
              {{ t('home.landing.production.title') }}
            </h2>
          </div>

          <div class="mx-auto grid max-w-4xl gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <div v-for="(capability, index) in productionCapabilities" :key="capability.label"
              v-reveal
              class="reveal-item reveal-zoom flex items-center gap-3 rounded-2xl border border-gray-200 bg-white/80 p-4 shadow-card dark:border-dark-700 dark:bg-dark-800/80"
              :style="{ transitionDelay: `${index * 55}ms` }">
              <span
                class="flex h-10 w-10 flex-none items-center justify-center rounded-xl bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
                <Icon :name="capability.icon" size="sm" />
              </span>
              <span class="font-medium text-gray-800 dark:text-dark-100">{{ capability.label }}</span>
            </div>
          </div>
        </section>

        <!-- Security -->
        <section id="security" class="py-14">
          <div v-reveal class="reveal-item mx-auto mb-8 max-w-3xl text-center">
            <span
              class="font-mono text-xs font-medium uppercase tracking-[0.12em] text-primary-600 dark:text-primary-400">
              {{ t('home.landing.security.eyebrow') }}
            </span>
            <h2 class="mt-3 text-3xl font-bold leading-tight text-gray-900 dark:text-white md:text-4xl">
              {{ t('home.landing.security.title') }}
            </h2>
          </div>

          <p v-reveal class="reveal-item mx-auto mb-10 max-w-2xl text-center text-base leading-7 text-gray-600 dark:text-dark-300">
            {{ t('home.landing.security.description') }}
            <b class="font-semibold text-gray-900 dark:text-white">
              {{ t('home.landing.security.highlight') }}
            </b>
          </p>

          <div class="mx-auto grid max-w-3xl gap-4 sm:grid-cols-2">
            <div v-for="(item, index) in securityCapabilities" :key="item.label"
              v-reveal
              class="reveal-item reveal-zoom flex items-center gap-3 rounded-2xl border border-gray-200 bg-white/80 p-4 shadow-card dark:border-dark-700 dark:bg-dark-800/80"
              :style="{ transitionDelay: `${index * 65}ms` }">
              <span
                class="flex h-10 w-10 flex-none items-center justify-center rounded-xl bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
                <Icon :name="item.icon" size="sm" />
              </span>
              <span class="font-medium text-gray-800 dark:text-dark-100">{{ item.label }}</span>
            </div>
          </div>
        </section>

        <!-- Developers -->
        <section id="developers" class="py-14">
          <div
            v-reveal
            class="reveal-item grid gap-8 border-t grid-cols-1 border-gray-200/70 pt-10 dark:border-dark-700/70 lg:grid-cols-2 lg:items-center lg:gap-14">
            <div class="reveal-child-left">
              <h3 class="text-2xl font-semibold leading-tight text-gray-900 dark:text-white md:text-3xl">
                {{ t('home.landing.developers.title') }}
              </h3>
              <p class="mt-4 max-w-xl text-base leading-7 text-gray-600 dark:text-dark-300">
                {{ t('home.landing.developers.description') }}
              </p>
              <span
                class="mt-5 inline-flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-2 font-mono text-sm font-medium text-emerald-500">
                <Icon name="check" size="sm" :stroke-width="2" />
                {{ t('home.landing.developers.compatibility') }}
              </span>
            </div>

            <div class="reveal-child-right flex justify-center lg:justify-end">
              <div class="terminal-container">
                <div class="terminal-window w-full lg:w-[420px]">
                  <!-- Window header -->
                  <div class="terminal-header">
                    <div class="terminal-buttons">
                      <span class="btn-close"></span>
                      <span class="btn-minimize"></span>
                      <span class="btn-maximize"></span>
                    </div>
                    <span class="terminal-title">terminal</span>
                  </div>
                  <!-- Terminal content -->
                  <div class="terminal-body">
                    <div class="code-line line-1">
                      <span class="code-prompt">$</span>
                      <span class="code-cmd">curl</span>
                      <span class="code-flag">-X POST</span>
                      <span class="code-url">/v1/messages</span>
                    </div>
                    <div class="code-line line-2">
                      <span class="code-comment"># Routing to upstream...</span>
                    </div>
                    <div class="code-line line-3">
                      <span class="code-success">200 OK</span>
                      <span class="code-response">{ "content": "Hello!" }</span>
                    </div>
                    <div class="code-line line-4">
                      <span class="code-prompt">$</span>
                      <span class="cursor"></span>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </section>

        <!-- Social Proof -->
        <section class="py-14 text-center">
          <div v-reveal class="reveal-item mx-auto mb-8 max-w-3xl">
            <span
              class="font-mono text-xs font-medium uppercase tracking-[0.12em] text-primary-600 dark:text-primary-400">
              {{ t('home.landing.socialProof.eyebrow') }}
            </span>
            <h2 class="mt-3 text-3xl font-bold leading-tight text-gray-900 dark:text-white md:text-4xl">
              {{ t('home.landing.socialProof.title') }}
            </h2>
            <p class="mx-auto mt-4 max-w-2xl text-base leading-7 text-gray-600 dark:text-dark-300">
              {{ t('home.landing.socialProof.description') }}
            </p>
          </div>

          <div class="mx-auto grid max-w-4xl gap-5 md:grid-cols-3">
            <div v-for="(stat, index) in proofStats" :key="stat.label"
              v-reveal
              class="reveal-item reveal-zoom rounded-2xl border border-gray-200 bg-white/80 p-7 shadow-card dark:border-dark-700 dark:bg-dark-800/80"
              :style="{ transitionDelay: `${index * 70}ms` }">
              <div
                class="bg-gradient-to-r from-primary-500 to-primary-600 bg-clip-text text-4xl font-bold leading-none [-webkit-text-fill-color:transparent]">
                {{ stat.value }}
              </div>
              <div class="mt-3 font-mono text-xs uppercase tracking-wider text-gray-500 dark:text-dark-400">
                {{ stat.label }}
              </div>
            </div>
          </div>
        </section>

        <!-- Closing -->
        <section class="py-14 text-center">
          <div
            v-reveal
            class="reveal-item reveal-zoom relative overflow-hidden rounded-[1.75rem] border border-gray-200 bg-white/85 px-6 py-16 shadow-2xl dark:border-dark-700 dark:bg-dark-800/85 md:px-8">
            <div
              class="pointer-events-none absolute inset-0 bg-[radial-gradient(600px_280px_at_50%_-10%,rgba(123,97,255,0.18),transparent_60%)]">
            </div>
            <div class="relative">
              <h2 class="text-4xl font-bold leading-tight text-gray-900 dark:text-white md:text-5xl">
                {{ t('home.landing.closing.titleFirst') }}<br />{{ t('home.landing.closing.titleSecond') }}
              </h2>
              <p class="mx-auto mt-5 max-w-xl text-lg leading-8 text-gray-600 dark:text-dark-300">
                {{ t('home.landing.closing.description') }}
              </p>
              <router-link :to="isAuthenticated ? dashboardPath : '/login'"
                class="btn btn-primary mt-8 px-8 py-3 text-base shadow-lg shadow-primary-500/30">
                {{ isAuthenticated ? t('home.goToDashboard') : t('home.landing.closing.button') }}
                <Icon name="arrowRight" size="md" class="ml-2" :stroke-width="2" />
              </router-link>
            </div>
          </div>
        </section>
      </div>
    </main>

    <!-- Footer -->
    <app-footer />
  </div>
</template>

<script setup lang="ts">
import type { Directive } from 'vue'
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import Icon from '@/components/icons/Icon.vue'
import AppFooter from '@/components/layout/AppFooter.vue'
import Header from '@/components/layout/Header.vue'

const { t } = useI18n()

const appStore = useAppStore()
const authStore = useAuthStore()

const featureRows = computed(() => [
  {
    title: t('home.landing.features.reliability.title'),
    description: t('home.landing.features.reliability.description'),
    tags: [
      t('home.landing.features.reliability.tags.uptime'),
      t('home.landing.features.reliability.tags.failover'),
      t('home.landing.features.reliability.tags.productionReady')
    ],
    visual: 'uptime',
    reversed: false
  },
  {
    title: t('home.landing.features.costs.title'),
    description: t('home.landing.features.costs.description'),
    tags: [
      t('home.landing.features.costs.tags.save'),
      t('home.landing.features.costs.tags.transparent'),
      t('home.landing.features.costs.tags.noHiddenFees')
    ],
    visual: 'cost',
    reversed: true
  }
])

const uptimeBars = [
  88, 80, 82, 81, 88, 91, 95, 80, 93, 89, 81, 94, 99, 92, 90, 82, 94, 92, 83, 89,
  82, 84, 85, 83, 92, 99, 60, 93, 86, 88, 90, 94, 92, 96, 83, 98, 82, 84, 95, 81
].map((height, index) => ({ height, isDip: index === 26 }))

const healthyServices = ['claude-opus', 'gpt-4o'] as const

const costRows = computed(() => [
  {
    label: t('home.landing.costs.direct'),
    price: '$1.00',
    width: '100%',
    fillClass: 'bg-gray-400'
  },
  {
    label: 'publicai',
    price: '$0.30',
    width: '30%',
    fillClass: 'bg-gradient-to-r from-primary-500 to-primary-600'
  }
])

// Official model pricing (per 1M tokens)
const officialPricing = [
  { name: 'GPT-5.4', provider: 'OpenAI', brand: 'gpt', officialInput: 2.50, officialOutput: 15.00 },
  { name: 'GPT-5.5', provider: 'OpenAI', brand: 'gpt', officialInput: 5.00, officialOutput: 30.00 },
  { name: 'Claude 4.6', provider: 'Anthropic', brand: 'claude', officialInput: 5.00, officialOutput: 25.00 },
  { name: 'Claude 4.7', provider: 'Anthropic', brand: 'claude', officialInput: 5.00, officialOutput: 25.00 },
  { name: 'Claude 4.8', provider: 'Anthropic', brand: 'claude', officialInput: 5.00, officialOutput: 25.00 },
]

// Gateway price = 30% of official price
const GATEWAY_DISCOUNT = 0.3

const pricingCards = computed(() =>
  officialPricing.map((m) => ({
    ...m,
    gatewayInput: (m.officialInput * GATEWAY_DISCOUNT).toFixed(2),
    gatewayOutput: (m.officialOutput * GATEWAY_DISCOUNT).toFixed(2),
  }))
)

const modelCards = [
  {
    name: 'Claude',
    initial: 'C',
    available: true,
    iconClass: 'bg-gradient-to-br from-orange-500 to-orange-600'
  },
  {
    name: 'GPT',
    initial: 'G',
    available: true,
    iconClass: 'bg-gradient-to-br from-emerald-500 to-emerald-600'
  },
  {
    name: 'Gemini',
    initial: 'G',
    available: false,
    iconClass: 'bg-gradient-to-br from-blue-500 to-violet-500'
  },
  // {
  //   name: 'DeepSeek',
  //   initial: 'D',
  //   available: false,
  //   iconClass: 'bg-gradient-to-br from-indigo-500 to-blue-700'
  // },
  // {
  //   name: 'Qwen',
  //   initial: 'Q',
  //   available: false,
  //   iconClass: 'bg-gradient-to-br from-violet-600 to-purple-800'
  // },
  // {
  //   name: 'Grok',
  //   initial: 'X',
  //   available: false,
  //   iconClass: 'bg-gradient-to-br from-gray-700 to-black'
  // }
] as const

const productionCapabilities = computed(() => [
  { label: t('home.landing.production.capabilities.streaming'), icon: 'bolt' },
  { label: t('home.landing.production.capabilities.toolCalling'), icon: 'link' },
  { label: t('home.landing.production.capabilities.structuredOutput'), icon: 'grid' },
  { label: t('home.landing.production.capabilities.visionModels'), icon: 'eye' },
  { label: t('home.landing.production.capabilities.longContext'), icon: 'document' },
  { label: t('home.landing.production.capabilities.highThroughput'), icon: 'trendingUp' }
] as const)

const securityCapabilities = computed(() => [
  { label: t('home.landing.security.capabilities.encryptedTraffic'), icon: 'lock' },
  { label: t('home.landing.security.capabilities.noTraining'), icon: 'shield' },
  { label: t('home.landing.security.capabilities.auditReady'), icon: 'checkCircle' },
  { label: t('home.landing.security.capabilities.accessControl'), icon: 'userCircle' }
] as const)

const proofStats = computed(() => [
  { value: t('home.landing.socialProof.stats.requests.value'), label: t('home.landing.socialProof.stats.requests.label') },
  { value: '99.99%', label: t('home.landing.socialProof.stats.availability.label') },
  { value: t('home.landing.socialProof.stats.infrastructure.value'), label: t('home.landing.socialProof.stats.infrastructure.label') }
])

const revealObserver =
  typeof window === 'undefined'
    ? null
    : new IntersectionObserver(
        (entries) => {
          entries.forEach((entry) => {
            entry.target.classList.toggle('is-visible', entry.isIntersecting)
          })
        },
        {
          threshold: 0.18,
          rootMargin: '0px 0px -8% 0px'
        }
      )

const vReveal: Directive<HTMLElement> = {
  mounted(el) {
    if (!revealObserver) {
      el.classList.add('is-visible')
      return
    }
    revealObserver.observe(el)
  },
  unmounted(el) {
    revealObserver?.unobserve(el)
  }
}

// Site settings - directly from appStore (already initialized from injected config)
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || t('home.landing.hero.subtitleFallback'))

// Auth state
const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')

onMounted(() => {
  authStore.checkAuth()

  // Ensure public settings are loaded (will use cache if already loaded from injected config)
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>

<style scoped>
h1,
h2,
h3 {
  font-family: 'Space Grotesk', sans-serif;
  letter-spacing: -.02em
}

.reveal-item {
  opacity: 0;
  transform: translate3d(0, 34px, 0) scale(.985);
  transition:
    opacity .72s cubic-bezier(.2, .7, .2, 1),
    transform .72s cubic-bezier(.2, .7, .2, 1),
    filter .72s cubic-bezier(.2, .7, .2, 1);
  will-change: opacity, transform, filter;
}

.reveal-zoom {
  transform: translate3d(0, 28px, 0) scale(.955);
}

.reveal-child-left,
.reveal-child-right {
  opacity: 0;
  transition:
    opacity .75s cubic-bezier(.2, .7, .2, 1),
    transform .75s cubic-bezier(.2, .7, .2, 1);
}

.reveal-child-left {
  transform: translate3d(-28px, 0, 0);
}

.reveal-child-right {
  transform: translate3d(28px, 0, 0);
  transition-delay: .12s;
}

.is-visible {
  opacity: 1;
  transform: translate3d(0, 0, 0) scale(1);
}

.is-visible .reveal-child-left,
.is-visible .reveal-child-right {
  opacity: 1;
  transform: translate3d(0, 0, 0);
}

.reveal-bar {
  transform: scaleY(.18);
  transform-origin: bottom;
  transition: transform .58s cubic-bezier(.2, .85, .2, 1);
}

.is-visible .reveal-bar {
  transform: scaleY(1);
}

/* Terminal Container */
.terminal-container {
  position: relative;
  display: inline-block;
}

/* Terminal Window */
.terminal-window {
  background: linear-gradient(145deg, #1e293b 0%, #0f172a 100%);
  border-radius: 14px;
  box-shadow:
    0 25px 50px -12px rgba(0, 0, 0, 0.4),
    0 0 0 1px rgba(255, 255, 255, 0.1),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
  overflow: hidden;
  transform: perspective(1000px) rotateX(2deg) rotateY(-2deg);
  transition: transform 0.3s ease;
}

.terminal-window:hover {
  transform: perspective(1000px) rotateX(0deg) rotateY(0deg) translateY(-4px);
}

/* Terminal Header */
.terminal-header {
  display: flex;
  align-items: center;
  padding: 12px 16px;
  background: rgba(30, 41, 59, 0.8);
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}

.terminal-buttons {
  display: flex;
  gap: 8px;
}

.terminal-buttons span {
  width: 12px;
  height: 12px;
  border-radius: 50%;
}

.btn-close {
  background: #ef4444;
}

.btn-minimize {
  background: #eab308;
}

.btn-maximize {
  background: #22c55e;
}

.terminal-title {
  flex: 1;
  text-align: center;
  font-size: 12px;
  font-family: ui-monospace, monospace;
  color: #64748b;
  margin-right: 52px;
}

/* Terminal Body */
.terminal-body {
  padding: 20px 24px;
  font-family: ui-monospace, 'Fira Code', monospace;
  font-size: 14px;
  line-height: 2;
}

.code-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  opacity: 0;
  animation: line-appear 0.5s ease forwards;
}

.line-1 {
  animation-delay: 0.3s;
}

.line-2 {
  animation-delay: 1s;
}

.line-3 {
  animation-delay: 1.8s;
}

.line-4 {
  animation-delay: 2.5s;
}

@keyframes line-appear {
  from {
    opacity: 0;
    transform: translateY(5px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.code-prompt {
  color: #22c55e;
  font-weight: bold;
}

.code-cmd {
  color: #38bdf8;
}

.code-flag {
  color: #a78bfa;
}

.code-url {
  color: #14b8a6;
}

.code-comment {
  color: #64748b;
  font-style: italic;
}

.code-success {
  color: #22c55e;
  background: rgba(34, 197, 94, 0.15);
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 600;
}

.code-response {
  color: #fbbf24;
}

/* Blinking Cursor */
.cursor {
  display: inline-block;
  width: 8px;
  height: 16px;
  background: #22c55e;
  animation: blink 1s step-end infinite;
}

@keyframes blink {

  0%,
  50% {
    opacity: 1;
  }

  51%,
  100% {
    opacity: 0;
  }
}

/* Dark mode adjustments */
:deep(.dark) .terminal-window {
  box-shadow:
    0 25px 50px -12px rgba(0, 0, 0, 0.6),
    0 0 0 1px rgba(20, 184, 166, 0.2),
    0 0 40px rgba(20, 184, 166, 0.1),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
}

@media (prefers-reduced-motion: reduce) {
  .reveal-item,
  .reveal-child-left,
  .reveal-child-right,
  .reveal-bar {
    opacity: 1;
    transform: none;
    filter: none;
    transition: none;
  }
}
</style>
