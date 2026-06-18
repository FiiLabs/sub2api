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
        class="absolute inset-0 bg-[linear-gradient(rgba(123,97,255,0.03)_1px,transparent_1px),linear-gradient(90deg,rgba(123,97,255,0.03)_1px,transparent_1px)] bg-[size:64px_64px]">
      </div>
    </div>

    <Header />

    <!-- Main Content -->
    <main class="relative z-10 flex-1 px-6 py-14">
      <!-- HERO (above the fold — rendered instantly, no reveal) -->
      <section class="mx-auto max-w-3xl text-center">
        <div
          class="mb-6 inline-flex items-center gap-2 rounded-full border border-primary-500/30 bg-primary-500/10 px-3 py-1.5 text-fluid-xs font-medium text-primary-600 dark:bg-primary-500/20 dark:text-primary-400">
          <span class="h-[7px] w-[7px] shrink-0 animate-pulse rounded-full bg-primary-500 dark:bg-primary-400"></span>
          {{ t('home.landing.hero.badge') }}
        </div>
        <h1 class="mb-5 text-fluid-3xl font-bold leading-[1.1] tracking-tight text-gray-900 dark:text-white">
          {{ t('home.landing.hero.title.line1') }}<br />
          <span class="text-primary-600 dark:text-primary-400">{{ t('home.landing.hero.title.line2') }}</span><br />
          {{ t('home.landing.hero.title.line3') }}
        </h1>
        <p class="mx-auto mb-8 max-w-[460px] text-fluid-base text-gray-500 dark:text-dark-300">
          {{ siteSubtitle }}
        </p>
        <div class="mb-10 flex flex-col gap-2 md:flex-row md:justify-center">
          <router-link :to="ctaTarget" class="btn btn-primary px-6 py-3 text-fluid-base shadow-lg shadow-primary-500/30">
            {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
          </router-link>
          <a :href="docUrl || '#personal'" :target="docUrl ? '_blank' : undefined"
            :rel="docUrl ? 'noopener noreferrer' : undefined" :class="outlineBtn">
            {{ t('home.landing.hero.viewDocs') }}
          </a>
        </div>
        <div class="mx-auto grid max-w-3xl grid-cols-2 gap-2 md:grid-cols-4">
          <div v-for="stat in heroStats" :key="stat.label" :class="cardClass" class="p-4 text-center">
            <div class="text-fluid-lg font-bold text-primary-600 dark:text-primary-400">{{ stat.value }}</div>
            <div class="mt-0.5 text-fluid-2xs text-gray-400 dark:text-dark-500">{{ stat.label }}</div>
          </div>
        </div>
      </section>

      <!-- ROUTING: TEE architecture -->
      <div class="mx-auto mt-14 max-w-5xl">
        <div v-reveal :class="cardClass" class="reveal-item overflow-hidden p-6 md:p-8">
          <div class="mb-3 text-center">
            <span :class="eyebrowClass">{{ t('home.landing.routing.eyebrow') }}</span>
          </div>
          <svg id="routing-svg" viewBox="0 0 720 300" xmlns="http://www.w3.org/2000/svg"
            class="block h-auto w-full">
            <defs>
              <marker id="arr" markerWidth="7" markerHeight="7" refX="5.5" refY="3" orient="auto">
                <path d="M0,0 L0,6 L6,3 z" class="arr-fill" />
              </marker>
              <filter id="glow" x="-40%" y="-40%" width="180%" height="180%">
                <feGaussianBlur stdDeviation="5" result="b" />
                <feMerge>
                  <feMergeNode in="b" />
                  <feMergeNode in="SourceGraphic" />
                </feMerge>
              </filter>
              <linearGradient id="teeGrad" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0" stop-color="#7b61ff" />
                <stop offset="1" stop-color="#5d30f7" />
              </linearGradient>
            </defs>

            <!-- 1. User API Client -->
            <rect x="12" y="116" width="132" height="68" rx="11" class="surf" stroke-width="1" />
            <text x="78" y="138" text-anchor="middle" font-size="11.5" class="label"
              font-family="system-ui,sans-serif" font-weight="600">User API Client</text>
            <rect x="24" y="148" width="108" height="16" rx="4" class="chip" />
            <text x="78" y="159.5" text-anchor="middle" font-size="9" class="chipt"
              font-family="monospace">Encrypted prompt</text>
            <text x="78" y="176" text-anchor="middle" font-size="8.5" class="label2"
              font-family="monospace">↓ TLS / E2E ↓</text>

            <!-- flow: client -> gateway -->
            <line x1="144" y1="150" x2="226" y2="150" class="flow" stroke-width="1.3" stroke-dasharray="5,4"
              marker-end="url(#arr)">
              <animate attributeName="stroke-dashoffset" from="0" to="-18" dur="1.2s" repeatCount="indefinite" />
            </line>
            <text x="185" y="142" text-anchor="middle" font-size="8.5" class="label2"
              font-family="system-ui">encrypted</text>

            <!-- 2. TEE boundary box -->
            <rect x="228" y="40" width="256" height="222" rx="16" fill="url(#teeGrad)" opacity="0.06"
              stroke="#7b61ff" stroke-width="1.3" stroke-dasharray="6,5">
              <animate attributeName="stroke-dashoffset" from="0" to="-22" dur="3s" repeatCount="indefinite" />
            </rect>
            <text x="356" y="60" text-anchor="middle" font-size="10.5" class="chipt"
              font-family="system-ui,sans-serif" font-weight="700">🔒 Intel TDX Confidential VM (TEE)</text>
            <text x="356" y="74" text-anchor="middle" font-size="8.5" class="label2"
              font-family="system-ui">Hardware-isolated · memory encrypted</text>

            <!-- Gateway core -->
            <rect x="252" y="92" width="208" height="104" rx="14" fill="url(#teeGrad)" filter="url(#glow)" />
            <rect x="256" y="96" width="200" height="42" rx="10" fill="rgba(255,255,255,0.10)" />
            <text x="356" y="116" text-anchor="middle" font-size="13" fill="white" font-weight="700"
              font-family="system-ui,sans-serif">TDX Secure Gateway</text>
            <text x="356" y="131" text-anchor="middle" font-size="9.5" fill="rgba(255,255,255,0.8)"
              font-family="system-ui">Nginx + PublicAI Gateway</text>
            <!-- attestation badge -->
            <rect x="284" y="148" width="144" height="34" rx="8" fill="rgba(255,255,255,0.14)"
              stroke="rgba(255,255,255,0.35)" stroke-width="1" />
            <circle cx="302" cy="165" r="5" fill="#7CE3B5">
              <animate attributeName="opacity" values="1;0.4;1" dur="1.8s" repeatCount="indefinite" />
            </circle>
            <text x="356" y="163" text-anchor="middle" font-size="9" fill="white" font-weight="600"
              font-family="system-ui">Verified by</text>
            <text x="356" y="174" text-anchor="middle" font-size="9" fill="white" font-weight="600"
              font-family="system-ui">Remote Attestation</text>

            <!-- flow: gateway -> providers -->
            <path d="M484,118 C540,118 548,72 596,72" class="flow" stroke-width="1.3" fill="none"
              stroke-dasharray="5,4" marker-end="url(#arr)">
              <animate attributeName="stroke-dashoffset" from="0" to="-18" dur="1s" repeatCount="indefinite" />
            </path>
            <line x1="484" y1="150" x2="596" y2="150" class="flow" stroke-width="1.3" stroke-dasharray="5,4"
              marker-end="url(#arr)">
              <animate attributeName="stroke-dashoffset" from="0" to="-18" dur="1.3s" repeatCount="indefinite" />
            </line>
            <path d="M484,182 C540,182 548,228 596,228" class="flow" stroke-width="1.3" fill="none"
              stroke-dasharray="5,4" marker-end="url(#arr)">
              <animate attributeName="stroke-dashoffset" from="0" to="-18" dur="0.9s" repeatCount="indefinite" />
            </path>
            <text x="540" y="143" text-anchor="middle" font-size="8.5" class="label2"
              font-family="system-ui">Secure Routing</text>

            <!-- 3. Providers -->
            <rect x="596" y="52" width="116" height="40" rx="8" class="surf" stroke-width="1" />
            <circle cx="612" cy="72" r="6.5" fill="#10a37f" />
            <text x="623" y="69" font-size="10.5" font-weight="600" class="strong" font-family="system-ui">OpenAI</text>
            <text x="623" y="80" font-size="9" class="label2" font-family="system-ui">GPT-5.x</text>

            <rect x="596" y="130" width="116" height="40" rx="8" class="surf" stroke-width="1" />
            <circle cx="612" cy="150" r="6.5" fill="#7b61ff" />
            <text x="623" y="147" font-size="10.5" font-weight="600" class="strong" font-family="system-ui">Claude</text>
            <text x="623" y="158" font-size="9" class="label2" font-family="system-ui">Anthropic 4.x</text>

            <rect x="596" y="208" width="116" height="40" rx="8" class="surf" stroke-width="1" />
            <circle cx="612" cy="228" r="6.5" fill="#4285F4" />
            <text x="623" y="225" font-size="10.5" font-weight="600" class="strong" font-family="system-ui">Gemini</text>
            <text x="623" y="236" font-size="9" class="label2" font-family="system-ui">Google</text>

            <!-- packets -->
            <circle r="4" fill="#7b61ff">
              <animateMotion dur="1.6s" repeatCount="indefinite" path="M144,150 L226,150" />
              <animate attributeName="opacity" values="0;0.9;0" dur="1.6s" repeatCount="indefinite" />
            </circle>
            <circle r="3.5" fill="#10a37f">
              <animateMotion dur="1.2s" repeatCount="indefinite" begin="0.2s"
                path="M484,118 C540,118 548,72 596,72" />
              <animate attributeName="opacity" values="0;0.9;0" dur="1.2s" repeatCount="indefinite" begin="0.2s" />
            </circle>
            <circle r="3.5" fill="#7b61ff">
              <animateMotion dur="1s" repeatCount="indefinite" begin="0.5s" path="M484,150 L596,150" />
              <animate attributeName="opacity" values="0;0.9;0" dur="1s" repeatCount="indefinite" begin="0.5s" />
            </circle>
            <circle r="3.5" fill="#4285F4">
              <animateMotion dur="1.4s" repeatCount="indefinite" begin="0.8s"
                path="M484,182 C540,182 548,228 596,228" />
              <animate attributeName="opacity" values="0;0.9;0" dur="1.4s" repeatCount="indefinite" begin="0.8s" />
            </circle>
          </svg>
          <p class="mt-3 text-center text-fluid-2xs text-gray-400 dark:text-dark-500">
            {{ t('home.landing.routing.caption') }}
          </p>
        </div>
      </div>

      <!-- PERSONAL -->
      <section id="personal" class="mx-auto mt-14 max-w-5xl">
        <div v-reveal class="reveal-item">
          <span :class="eyebrowClass">{{ t('home.landing.personal.eyebrow') }}</span>
          <h2 :class="headingClass" class="mt-1.5">{{ t('home.landing.personal.title') }}</h2>
          <p :class="subClass" class="mb-6 mt-2 max-w-3xl">{{ t('home.landing.personal.subtitle') }}</p>
        </div>

        <div class="grid gap-4 md:grid-cols-2">
          <div v-for="(feature, index) in personalFeatures" :key="feature.visual" v-reveal :class="cardClass"
            class="reveal-item p-5" :style="{ transitionDelay: `${index * 70}ms` }">
            <div class="mb-3 text-xl">{{ feature.icon }}</div>
            <h3 class="mb-1 text-fluid-base font-semibold text-gray-900 dark:text-white">{{ feature.title }}</h3>
            <p class="text-fluid-sm text-gray-500 dark:text-dark-400">{{ feature.desc }}</p>

            <!-- cost bars -->
            <div v-if="feature.visual === 'cost'" class="mt-3.5 rounded-lg bg-gray-50 p-3 dark:bg-dark-900/50">
              <div v-for="bar in costBars" :key="bar.label" class="mb-1.5 flex items-center gap-2 last:mb-0">
                <span class="w-[72px] shrink-0 text-fluid-2xs text-gray-500 dark:text-dark-400">{{ bar.label }}</span>
                <div class="h-2 flex-1 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
                  <div class="h-full rounded-full" :class="bar.fill" :style="{ width: `${bar.pct}%` }"></div>
                </div>
                <span class="min-w-[40px] text-right text-fluid-xs font-semibold" :class="bar.valueClass">{{ bar.value }}</span>
              </div>
              <div class="mt-1.5 text-right text-fluid-2xs text-gray-400 dark:text-dark-500">
                {{ t('home.landing.personal.cost.note') }}
              </div>
            </div>

            <!-- uptime grid -->
            <div v-else-if="feature.visual === 'uptime'">
              <div class="mt-3 flex items-stretch gap-[2px] sm:gap-[3px]">
                <div v-for="(cell, i) in uptimeCells" :key="i" class="h-5 min-w-0 flex-1 rounded-[2px] sm:rounded-[3px]"
                  :class="cell.warn ? 'bg-amber-400' : 'bg-primary-500 opacity-85'"></div>
              </div>
              <div class="mt-2.5 text-fluid-xl font-bold text-primary-600 dark:text-primary-400">99.99%</div>
              <div class="text-fluid-2xs text-gray-400 dark:text-dark-500">
                {{ t('home.landing.personal.uptime.window') }}
              </div>
            </div>

            <!-- model pills -->
            <div v-else-if="feature.visual === 'models'" class="mt-3 flex flex-wrap gap-1.5">
              <span v-for="model in modelPills" :key="model" :class="pillClass">{{ model }}</span>
            </div>

            <!-- failover -->
            <div v-else class="mt-3 flex flex-wrap items-center gap-1.5">
              <div
                class="rounded-lg border border-gray-200 bg-gray-50 px-2.5 py-1 text-fluid-xs line-through opacity-45 dark:border-dark-700 dark:bg-dark-900/50">
                {{ t('home.landing.personal.failover.from') }}
              </div>
              <span class="text-gray-400">→</span>
              <div
                class="rounded-lg border border-primary-500 bg-primary-500/10 px-2.5 py-1 text-fluid-xs text-primary-600 dark:text-primary-400">
                {{ t('home.landing.personal.failover.to') }}
              </div>
            </div>
          </div>
        </div>
      </section>

      <div class="mx-auto my-12 h-px max-w-5xl bg-gray-200 dark:bg-dark-800"></div>

      <!-- TEAM -->
      <section id="team" class="mx-auto max-w-5xl">
        <div v-reveal class="reveal-item">
          <span :class="eyebrowClass">{{ t('home.landing.team.eyebrow') }}</span>
          <h2 :class="headingClass" class="mt-1.5">{{ t('home.landing.team.title') }}</h2>
          <p :class="subClass" class="mb-6 mt-2 max-w-3xl">{{ t('home.landing.team.subtitle') }}</p>
        </div>

        <div v-reveal :class="cardClass" class="reveal-item overflow-hidden">
          <div
            class="flex flex-col gap-2 bg-primary-500 p-5 text-white md:flex-row md:items-center md:justify-between">
            <div>
              <div class="text-fluid-sm font-semibold">{{ t('home.landing.team.header.title') }}</div>
              <div class="text-fluid-xs opacity-75">{{ t('home.landing.team.header.subtitle') }}</div>
            </div>
            <span
              class="inline-flex items-center gap-1.5 self-start rounded-full border border-white/30 bg-white/20 px-3 py-1 text-fluid-2xs font-medium">
              {{ t('home.landing.team.header.badge') }}
            </span>
          </div>

          <div class="grid md:grid-cols-2">
            <!-- Member governance -->
            <div class="border-b border-gray-200 p-5 dark:border-dark-700 md:border-r">
              <h3 class="mb-1.5 text-fluid-sm font-semibold text-gray-900 dark:text-white">
                {{ t('home.landing.team.members.title') }}
              </h3>
              <p class="mb-3 text-fluid-sm text-gray-500 dark:text-dark-400">
                {{ t('home.landing.team.members.desc') }}
              </p>
              <div>
                <div v-for="(member, i) in teamMembers" :key="member.initials"
                  class="flex items-center justify-between py-1.5"
                  :class="i < teamMembers.length - 1 ? 'border-b border-gray-200 dark:border-dark-700' : ''">
                  <div class="flex items-center gap-2">
                    <div class="flex h-[26px] w-[26px] items-center justify-center rounded-full text-fluid-2xs font-bold"
                      :class="member.avatarClass">{{ member.initials }}</div>
                    <div>
                      <div class="text-fluid-xs font-semibold text-gray-900 dark:text-white">{{ member.name }}</div>
                      <div class="text-fluid-2xs text-gray-400">{{ member.role }}</div>
                    </div>
                  </div>
                  <span class="rounded-full px-2 py-0.5 text-fluid-2xs" :class="member.budgetClass">{{ member.budget }}</span>
                </div>
              </div>
            </div>

            <!-- BYOK -->
            <div class="border-b border-gray-200 p-5 dark:border-dark-700">
              <h3 class="mb-1.5 text-fluid-sm font-semibold text-gray-900 dark:text-white">
                {{ t('home.landing.team.byok.title') }}
              </h3>
              <p class="mb-3 text-fluid-sm text-gray-500 dark:text-dark-400">
                {{ t('home.landing.team.byok.desc') }}
              </p>
              <div>
                <div v-for="(key, i) in byokKeys" :key="key.provider"
                  class="flex items-center gap-2 py-1.5 text-fluid-xs"
                  :class="i < byokKeys.length - 1 ? 'border-b border-gray-200 dark:border-dark-700' : ''">
                  <span class="min-w-[65px] shrink-0 text-gray-500 dark:text-dark-400">{{ key.provider }}</span>
                  <span class="flex-1 truncate font-mono text-fluid-2xs text-gray-400">{{ key.masked }}</span>
                  <span class="shrink-0 rounded-full px-2 py-0.5 text-fluid-2xs" :class="key.statusClass">{{ key.status }}</span>
                </div>
              </div>
            </div>

            <!-- Usage visibility -->
            <div class="border-b border-gray-200 p-5 dark:border-dark-700 md:border-b-0 md:border-r">
              <h3 class="mb-1.5 text-fluid-sm font-semibold text-gray-900 dark:text-white">
                {{ t('home.landing.team.usage.title') }}
              </h3>
              <p class="mb-3 text-fluid-sm text-gray-500 dark:text-dark-400">
                {{ t('home.landing.team.usage.desc') }}
              </p>
              <div>
                <div v-for="row in usageRows" :key="row.who"
                  class="flex items-center justify-between border-b border-gray-200 py-1.5 text-fluid-xs dark:border-dark-700">
                  <span class="text-gray-500 dark:text-dark-400">{{ row.who }}</span>
                  <span class="font-semibold text-gray-900 dark:text-white">{{ row.amount }}</span>
                </div>
                <div class="flex items-center justify-between py-1.5 text-fluid-xs">
                  <span class="font-semibold text-gray-900 dark:text-white">{{ t('home.landing.team.usage.total') }}</span>
                  <span class="font-semibold text-primary-600 dark:text-primary-400">{{ usageTotal }}</span>
                </div>
              </div>
            </div>

            <!-- Verifiable privacy -->
            <div class="p-5">
              <h3 class="mb-1.5 text-fluid-sm font-semibold text-gray-900 dark:text-white">
                {{ t('home.landing.team.privacy.title') }}
              </h3>
              <p class="mb-3 text-fluid-sm text-gray-500 dark:text-dark-400">
                {{ t('home.landing.team.privacy.desc') }}
              </p>
              <div class="space-y-0.5">
                <div v-for="point in privacyPoints" :key="point"
                  class="flex items-center gap-2 py-0.5 text-fluid-sm text-gray-700 dark:text-dark-200">
                  <span class="font-bold text-primary-600 dark:text-primary-400">✓</span> {{ point }}
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      <div class="mx-auto my-12 h-px max-w-5xl bg-gray-200 dark:bg-dark-800"></div>

      <!-- PRICING -->
      <section id="pricing" class="mx-auto max-w-5xl">
        <div v-reveal class="reveal-item">
          <span :class="eyebrowClass">{{ t('home.landing.pricing.eyebrow') }}</span>
          <h2 :class="headingClass" class="mt-1.5">{{ t('home.landing.pricing.title') }}</h2>
          <p :class="subClass" class="mb-6 mt-2 max-w-3xl">{{ t('home.landing.pricing.subtitle') }}</p>
        </div>

        <div class="flex flex-col gap-5 md:flex-row">
          <div v-for="(plan, index) in pricingPlans" :key="plan.name" v-reveal
            class="reveal-item relative flex-1 rounded-xl bg-white/80 p-6 pb-16 shadow-card backdrop-blur-sm dark:bg-dark-800/60"
            :style="{ transitionDelay: `${index * 90}ms` }"
            :class="plan.featured ? 'border-2 border-primary-500' : 'border border-gray-200 dark:border-dark-700'">
            <div class="mb-1.5 text-fluid-2xs font-semibold uppercase tracking-wider text-primary-600 dark:text-primary-400">
              {{ plan.name }}
            </div>
            <div class="mb-1 text-fluid-lg font-bold text-gray-900 dark:text-white">{{ plan.tagline }}</div>
            <div class="mb-1 text-fluid-sm text-gray-500 dark:text-dark-400">
              {{ t('home.landing.pricing.from') }}
              <span class="text-fluid-base font-bold text-primary-600 dark:text-primary-400">{{ plan.pct }}</span>
              {{ t('home.landing.pricing.perTokens') }}
            </div>
            <p class="mb-5 text-fluid-sm text-gray-500 dark:text-dark-400">{{ plan.desc }}</p>
            <ul class="mb-6 space-y-2">
              <li v-for="feat in plan.features" :key="feat"
                class="flex items-start gap-1.5 text-fluid-sm text-gray-700 dark:text-dark-200">
                <span class="mt-px shrink-0 font-bold text-primary-600 dark:text-primary-400">✓</span> {{ feat }}
              </li>
            </ul>
            <a v-if="plan.ctaHref" :href="plan.ctaHref"
              class="absolute inset-x-6 bottom-6 rounded-lg py-3 text-center text-fluid-sm font-semibold transition-colors"
              :class="plan.featured
                ? 'bg-primary-500 text-white hover:bg-primary-600'
                : 'border border-gray-300 text-gray-700 hover:border-primary-500 dark:border-dark-600 dark:text-dark-200'"
            >
              {{ plan.cta }}
            </a>
            <router-link v-else :to="ctaTarget"
              class="absolute inset-x-6 bottom-6 rounded-lg py-3 text-center text-fluid-sm font-semibold transition-colors"
              :class="plan.featured
                ? 'bg-primary-500 text-white hover:bg-primary-600'
                : 'border border-gray-300 text-gray-700 hover:border-primary-500 dark:border-dark-600 dark:text-dark-200'">
              {{ plan.cta }}
            </router-link>
          </div>
        </div>
      </section>

      <!-- TRUST -->
      <section class="mx-auto mt-14 max-w-5xl text-center">
        <div v-reveal class="reveal-item">
          <span :class="eyebrowClass">{{ t('home.landing.trust.eyebrow') }}</span>
          <h2 :class="headingClass" class="mt-1.5">{{ t('home.landing.trust.title') }}</h2>
        </div>

        <div class="my-6 grid grid-cols-3 gap-2">
          <div v-for="(stat, index) in trustStats" :key="stat.label" v-reveal :class="cardClass"
            class="reveal-item p-4" :style="{ transitionDelay: `${index * 70}ms` }">
            <div class="text-fluid-xl font-bold text-primary-600 dark:text-primary-400">{{ stat.value }}</div>
            <div class="mt-0.5 text-fluid-2xs text-gray-500 dark:text-dark-400">{{ stat.label }}</div>
          </div>
        </div>

        <div v-reveal class="reveal-item flex flex-wrap justify-center gap-2">
          <span v-for="badge in trustPills" :key="badge"
            class="inline-flex items-center gap-1.5 rounded-full border border-gray-200 bg-white/80 px-3 py-1.5 text-fluid-xs text-gray-500 dark:border-dark-700 dark:bg-dark-800/60 dark:text-dark-300">
            {{ badge }}
          </span>
        </div>
      </section>

      <!-- CTA -->
      <section class="mx-auto mt-14 max-w-5xl">
        <div v-reveal
          class="reveal-item rounded-xl border border-primary-500/30 bg-primary-500/10 px-6 py-14 text-center dark:bg-primary-500/15">
          <h2 class="mb-2 text-fluid-2xl font-bold tracking-tight text-gray-900 dark:text-white">
            {{ t('home.landing.cta.titleFirst') }}<br />{{ t('home.landing.cta.titleSecond') }}
          </h2>
          <p class="mb-7 text-fluid-sm text-gray-500 dark:text-dark-400">
            {{ t('home.landing.cta.description') }}
          </p>
          <div class="flex flex-col gap-2 md:flex-row md:justify-center">
            <router-link :to="ctaTarget" class="btn btn-primary px-6 py-3 text-fluid-base shadow-lg shadow-primary-500/30">
              {{ t('home.landing.cta.primary') }}
            </router-link>
            <a href="#team" :class="outlineBtn">{{ t('home.landing.cta.secondary') }}</a>
          </div>
        </div>
      </section>
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
import AppFooter from '@/components/layout/AppFooter.vue'
import Header from '@/components/layout/Header.vue'

const { t } = useI18n()

const appStore = useAppStore()
const authStore = useAuthStore()

// Shared utility class fragments (kept here so the template stays declarative)
const cardClass =
  'rounded-xl border border-gray-200 bg-white/80 shadow-card backdrop-blur-sm dark:border-dark-700 dark:bg-dark-800/60'
const eyebrowClass =
  'font-mono text-fluid-2xs font-semibold uppercase tracking-[0.1em] text-primary-600 dark:text-primary-400'
const headingClass = 'text-fluid-2xl font-bold tracking-tight text-gray-900 dark:text-white'
const subClass = 'text-fluid-sm text-gray-500 dark:text-dark-400'
const pillClass =
  'rounded-full border border-gray-200 bg-gray-100 px-2.5 py-1 text-fluid-xs text-gray-500 dark:border-dark-600 dark:bg-dark-700/50 dark:text-dark-300'
const outlineBtn =
  'inline-flex items-center justify-center rounded-lg border border-gray-300 px-6 py-3 text-fluid-base font-medium text-gray-700 transition-colors hover:border-primary-500 dark:border-dark-600 dark:text-dark-200'

// HERO stats
const heroStats = computed(() => [
  { value: '99.99%', label: t('home.landing.hero.stats.uptime') },
  { value: '−90%', label: t('home.landing.hero.stats.savings') },
  { value: 'Millions', label: t('home.landing.hero.stats.requests') },
  { value: 'TDX', label: t('home.landing.hero.stats.vm') }
])

// PERSONAL feature cards
const personalFeatures = computed(() => [
  {
    icon: '💰',
    title: t('home.landing.personal.cost.title'),
    desc: t('home.landing.personal.cost.desc'),
    visual: 'cost'
  },
  {
    icon: '📈',
    title: t('home.landing.personal.uptime.title'),
    desc: t('home.landing.personal.uptime.desc'),
    visual: 'uptime'
  },
  {
    icon: '🔀',
    title: t('home.landing.personal.aggregation.title'),
    desc: t('home.landing.personal.aggregation.desc'),
    visual: 'models'
  },
  {
    icon: '⚡',
    title: t('home.landing.personal.failover.title'),
    desc: t('home.landing.personal.failover.desc'),
    visual: 'failover'
  }
])

const costBars = computed(() => [
  {
    label: t('home.landing.personal.cost.direct'),
    pct: 100,
    value: '$10.00',
    fill: 'bg-gray-400 dark:bg-dark-500',
    valueClass: 'text-gray-400'
  },
  {
    label: t('home.landing.personal.cost.publicai'),
    pct: 10,
    value: '$1.00',
    fill: 'bg-primary-500',
    valueClass: 'text-primary-600 dark:text-primary-400'
  }
])

const uptimeCells = Array.from({ length: 25 }, (_, i) => ({ warn: i === 11 }))

const modelPills = ['GPT-5.4', 'GPT-5.5', 'Claude 4.8', 'Claude 4.6', 'Gemini', '+ more']

// TEAM panels
const teamMembers = computed(() => [
  {
    initials: 'AJ',
    name: 'Alex J.',
    role: t('home.landing.team.members.roles.admin'),
    budget: '$500 / mo',
    avatarClass: 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900 dark:text-indigo-300',
    budgetClass: 'bg-green-100 text-green-700 dark:bg-green-900/50 dark:text-green-300'
  },
  {
    initials: 'SR',
    name: 'Sara R.',
    role: t('home.landing.team.members.roles.developer'),
    budget: '$89 / $100',
    avatarClass: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900 dark:text-emerald-300',
    budgetClass: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300'
  },
  {
    initials: 'MK',
    name: 'Mike K.',
    role: t('home.landing.team.members.roles.analyst'),
    budget: '$42 / $150',
    avatarClass: 'bg-orange-100 text-orange-700 dark:bg-orange-900 dark:text-orange-300',
    budgetClass: 'bg-green-100 text-green-700 dark:bg-green-900/50 dark:text-green-300'
  }
])

const byokKeys = computed(() => [
  {
    provider: 'OpenAI',
    masked: 'sk-••••••••jK92',
    status: t('home.landing.team.byok.active'),
    statusClass: 'bg-green-100 text-green-700 dark:bg-green-900/50 dark:text-green-300'
  },
  {
    provider: 'Anthropic',
    masked: 'sk-ant-••••xP4T',
    status: t('home.landing.team.byok.governed'),
    statusClass: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300'
  },
  {
    provider: 'Gemini',
    masked: 'AIza••••••7Rq',
    status: t('home.landing.team.byok.governed'),
    statusClass: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300'
  }
])

const usageRows = [
  { who: 'Alex · claude-opus-4-8', amount: '142K · $2.13' },
  { who: 'Sara · gpt-5-5', amount: '89K · $0.80' },
  { who: 'Mike · claude-sonnet-4-6', amount: '61K · $0.46' }
]
const usageTotal = '$3.39'

const privacyPoints = computed(() => [
  t('home.landing.team.privacy.points.tdx'),
  t('home.landing.team.privacy.points.noLog'),
  t('home.landing.team.privacy.points.attestation'),
  t('home.landing.team.privacy.points.metadata')
])

// PRICING plans
const pricingPlans = computed(() => [
  {
    name: t('home.landing.pricing.personal.name'),
    tagline: t('home.landing.pricing.personal.tagline'),
    pct: t('home.landing.pricing.personal.pct'),
    desc: t('home.landing.pricing.personal.desc'),
    cta: t('home.landing.pricing.personal.cta'),
    featured: false,
    features: [1, 2, 3, 4, 5].map((i) => t(`home.landing.pricing.personal.features.f${i}`))
  },
  {
    name: t('home.landing.pricing.team.name'),
    tagline: t('home.landing.pricing.team.tagline'),
    pct: t('home.landing.pricing.team.pct'),
    desc: t('home.landing.pricing.team.desc'),
    cta: t('home.landing.pricing.team.cta'),
    ctaHref: 'mailto:Support@publicai.io',
    featured: true,
    features: [1, 2, 3, 4, 5, 6].map((i) => t(`home.landing.pricing.team.features.f${i}`))
  }
])

// TRUST
const trustStats = computed(() => [
  { value: 'M+', label: t('home.landing.trust.stats.requests') },
  { value: '99.99%', label: t('home.landing.trust.stats.availability') },
  { value: 'Global', label: t('home.landing.trust.stats.infrastructure') }
])

const trustPills = computed(() => [
  t('home.landing.trust.pills.encrypted'),
  t('home.landing.trust.pills.noTraining'),
  t('home.landing.trust.pills.audit'),
  t('home.landing.trust.pills.tee'),
  t('home.landing.trust.pills.compatible')
])

// Scroll reveal — hysteresis avoids the enter/exit flicker when an element sits
// right on the trigger line: reveal once it is comfortably in view (ratio ≥ 0.12),
// and only reset after it has fully left the viewport (ratio === 0). The dead band
// in between keeps the current state, so partial scrolls never toggle the animation.
const revealObserver =
  typeof window === 'undefined'
    ? null
    : new IntersectionObserver(
        (entries) => {
          entries.forEach((entry) => {
            if (entry.isIntersecting && entry.intersectionRatio >= 0.12) {
              entry.target.classList.add('is-visible')
            } else if (entry.intersectionRatio === 0) {
              entry.target.classList.remove('is-visible')
            }
          })
        },
        { threshold: [0, 0.12, 0.5] }
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

// Remote documentation link (admin-configurable, falls back to in-page anchor)
const docUrl = computed(() => appStore.cachedPublicSettings?.doc_url || appStore.docUrl || '')

// Site settings - directly from appStore (already initialized from injected config)
const siteSubtitle = computed(
  () => appStore.cachedPublicSettings?.site_subtitle || t('home.landing.hero.subtitle')
)

// Auth state
const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))
const ctaTarget = computed(() => (isAuthenticated.value ? dashboardPath.value : '/login'))

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

/* Scroll reveal — symmetric enter/exit. Paired with the hysteresis observer so the
   exit only plays once the element is fully out of view (no edge flicker). */
.reveal-item {
  opacity: 0;
  transform: translate3d(0, 26px, 0) scale(.99);
  transition:
    opacity .6s cubic-bezier(.2, .7, .2, 1),
    transform .6s cubic-bezier(.2, .7, .2, 1);
  will-change: opacity, transform;
}

.is-visible {
  opacity: 1;
  transform: translate3d(0, 0, 0) scale(1);
}

@media (prefers-reduced-motion: reduce) {
  .reveal-item {
    opacity: 1;
    transform: none;
    transition: none;
  }
}
</style>

<!-- Routing diagram: theme-aware SVG token fills (id-scoped, intentionally not `scoped`) -->
<style>
#routing-svg .surf {
  fill: #ffffff;
  stroke: #e5e4ef
}

#routing-svg .label {
  fill: #6b6b80
}

#routing-svg .label2 {
  fill: #9898a8
}

#routing-svg .strong {
  fill: #1a1a2e
}

#routing-svg .chip {
  fill: #e9e8ff
}

#routing-svg .chipt {
  fill: #7b61ff
}

#routing-svg .flow {
  stroke: #b6b1ff
}

#routing-svg .arr-fill {
  fill: #9385ff
}

.dark #routing-svg .surf {
  fill: #1e293b;
  stroke: #334155
}

.dark #routing-svg .label {
  fill: #94a3b8
}

.dark #routing-svg .label2 {
  fill: #64748b
}

.dark #routing-svg .strong {
  fill: #f1f5f9
}

.dark #routing-svg .chip {
  fill: #312e81
}

.dark #routing-svg .chipt {
  fill: #a5b4fc
}

.dark #routing-svg .flow {
  stroke: #7b61ff
}

.dark #routing-svg .arr-fill {
  fill: #7b61ff
}
</style>
