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
          {{ t('home.landing.hero.title') }}
        </h1>
        <p class="mx-auto mb-8 max-w-[460px] text-fluid-base text-gray-500 dark:text-dark-300">
          {{ t('home.landing.hero.subtitle') }}
        </p>
        <!--
          双边市场的两个入口,视觉权重相当。

          原来供给入口是三个按钮里的第三个,和「验证隐私」并排——一个次要按钮的
          外形在说"这是个附加功能",而它是平台的另一半:供给不够,消费侧再多用户
          也接不住。两张同样大的卡片让访客在第一屏就面对一个真正的分岔:
          你是来花钱的,还是来赚钱的。
        -->
        <!--
          两张卡是「你来花钱还是来赚钱」的分岔口，也是分流的起点：点哪张就把控制台
          预设成对应模式（登录后由 supplyStore 消化，见 chooseUsage / chooseSharing），
          登录后直接落进对应那套体验，而不是笼统进 dashboard 再自己找。
        -->
        <div class="mb-6 grid gap-4 text-left md:grid-cols-2">
          <router-link :to="ctaTarget" :class="entryCardClass" data-testid="home-entry-usage" @click="chooseUsage">
            <div class="mb-2 text-2xl">🚀</div>
            <div class="mb-1.5 text-fluid-lg font-bold text-gray-900 dark:text-white">
              {{ t('home.landing.hero.useAI') }}
            </div>
            <p class="mb-4 text-fluid-sm leading-relaxed text-gray-500 dark:text-dark-400">
              {{ t('home.landing.hero.useAIDesc') }}
            </p>
            <span class="mt-auto inline-flex items-center gap-1 text-fluid-sm font-semibold text-primary-600 dark:text-primary-400">
              {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
              <span aria-hidden="true">→</span>
            </span>
          </router-link>

          <!--
            直接进入口：点了就去 supplyTarget——已登录进共享控制台(/supply)，未登录
            进登录页。旧版是滚动到下方介绍段，但供给者要的是「我要开始共享」，一个
            直达登录的入口比先读一屏介绍更符合意图。介绍段仍在下方（#supply）供想
            了解的人手动往下看。
          -->
          <router-link
            v-if="showSupplyEntry"
            :to="supplyTarget"
            :class="entryCardClass"
            data-testid="home-entry-supply"
            @click="chooseSharing"
          >
            <div class="mb-2 text-2xl">💰</div>
            <div class="mb-1.5 text-fluid-lg font-bold text-gray-900 dark:text-white">
              {{ t('home.landing.hero.shareSubscription') }}
            </div>
            <p class="mb-4 text-fluid-sm leading-relaxed text-gray-500 dark:text-dark-400">
              {{ t('home.landing.hero.shareSubscriptionDesc') }}
            </p>
            <span class="mt-auto inline-flex items-center gap-1 text-fluid-sm font-semibold text-primary-600 dark:text-primary-400">
              {{ isAuthenticated ? t('supply.navLabel') : t('home.getStarted') }}
              <span aria-hidden="true">→</span>
            </span>
          </router-link>
        </div>

        <!-- /proof 是这个产品的核心差异化,降级成文字链接而不是删掉:
             它要证明的东西对已经在读隐私文案的人才有意义,不该跟入口抢注意力。 -->
        <div class="mb-10">
          <router-link
            to="/proof"
            class="text-fluid-sm font-medium text-gray-500 underline-offset-4 transition-colors hover:text-primary-600 hover:underline dark:text-dark-400 dark:hover:text-primary-400"
          >
            {{ t('home.landing.hero.verifyPrivacy') }} →
          </router-link>
        </div>
        <div class="mx-auto grid max-w-3xl grid-cols-2 gap-2 md:grid-cols-4">
          <div v-for="stat in heroStats" :key="stat.label" :class="cardClass" class="p-4 text-center">
            <div class="text-fluid-lg font-bold text-primary-600 dark:text-primary-400">{{ stat.value }}</div>
            <div class="mt-0.5 text-fluid-2xs text-gray-400 dark:text-dark-500">{{ stat.label }}</div>
          </div>
        </div>
      </section>

      <!-- ROUTING: TEE architecture -->
      <div id="architecture" class="mx-auto mt-14 max-w-5xl">
        <div v-reveal :class="cardClass" class="reveal-item overflow-hidden p-6 md:p-8">
          <div class="mb-6 text-center">
            <span :class="eyebrowClass">{{ t('home.landing.routing.eyebrow') }}</span>
            <h2 :class="headingClass" class="mt-2">{{ t('home.landing.routing.title') }}</h2>
            <p :class="subClass" class="mx-auto mt-2 max-w-2xl">{{ t('home.landing.routing.subtitle') }}</p>
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
              font-family="monospace">↓ TLS / E2EE ↓</text>

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
              font-family="system-ui">dstack-ingress + ApexOne</text>
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
            <circle cx="612" cy="72" r="6.5" fill="#7b61ff" />
            <text x="623" y="69" font-size="10.5" font-weight="600" class="strong" font-family="system-ui">Claude</text>
            <text x="623" y="80" font-size="9" class="label2" font-family="system-ui">Fable 5.1 live</text>

            <rect x="596" y="130" width="116" height="40" rx="8" class="surf" stroke-width="1" />
            <circle cx="612" cy="150" r="6.5" fill="#10a37f" />
            <text x="623" y="147" font-size="10.5" font-weight="600" class="strong" font-family="system-ui">GPT</text>
            <text x="623" y="158" font-size="9" class="label2" font-family="system-ui">coming soon</text>

            <rect x="596" y="208" width="116" height="40" rx="8" class="surf" stroke-width="1" />
            <circle cx="612" cy="228" r="6.5" fill="#4285F4" />
            <text x="623" y="225" font-size="10.5" font-weight="600" class="strong" font-family="system-ui">Gemini</text>
            <text x="623" y="236" font-size="9" class="label2" font-family="system-ui">coming soon</text>

            <!-- packets -->
            <circle r="4" fill="#7b61ff">
              <animateMotion dur="1.6s" repeatCount="indefinite" path="M144,150 L226,150" />
              <animate attributeName="opacity" values="0;0.9;0" dur="1.6s" repeatCount="indefinite" />
            </circle>
            <circle r="3.5" fill="#7b61ff">
              <animateMotion dur="1.2s" repeatCount="indefinite" begin="0.2s"
                path="M484,118 C540,118 548,72 596,72" />
              <animate attributeName="opacity" values="0;0.9;0" dur="1.2s" repeatCount="indefinite" begin="0.2s" />
            </circle>
            <circle r="3.5" fill="#10a37f">
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

      <!-- APEXONE -->
      <section id="personal" class="mx-auto mt-14 max-w-5xl">
        <div v-reveal class="reveal-item">
          <span :class="eyebrowClass">{{ t('home.landing.personal.eyebrow') }}</span>
          <h2 :class="headingClass" class="mt-1.5">{{ t('home.landing.personal.title') }}</h2>
          <p :class="subClass" class="mb-6 mt-2 max-w-3xl">{{ t('home.landing.personal.subtitle') }}</p>
        </div>

        <!--
          消费者视角的片子。原来它单独占一节挂在页面最后（id="why"），
          位置上离「谁该看它」很远：读者已经走完消费侧、价格、供给侧三段，
          才撞见一个没有上下文的播放器。

          两个视频各自归到它说的那件事里之后，这一节的节奏是
          说明 → 片子 → 对照表 → 细节卡：先用 47 秒把话讲完，再给想核对的人证据。
        -->
        <div v-if="consumerVideoUrl" v-reveal class="reveal-item mb-6">
          <div :class="videoBadgeClass">
            <span aria-hidden="true">🚀</span>
            <span>{{ t('home.landing.video.audience') }}</span>
          </div>
          <h3 class="mb-1 mt-3 text-fluid-lg font-bold tracking-tight text-gray-900 dark:text-white">
            {{ t('home.landing.video.title') }}
          </h3>
          <p :class="subClass" class="mb-3">{{ t('home.landing.video.subtitle') }}</p>
          <div :class="cardClass" class="overflow-hidden p-2 md:p-3">
            <VideoPlayer :src="consumerVideoUrl" :poster="promoVideoPoster" fit="contain" />
          </div>
        </div>

        <!-- comparison table -->
        <div v-reveal :class="cardClass" class="reveal-item overflow-x-auto p-5">
          <table class="w-full min-w-[560px] border-collapse text-left">
            <thead>
              <tr class="border-b border-gray-200 dark:border-dark-700">
                <th class="py-3 pr-4"></th>
                <th class="px-4 py-3 text-fluid-sm font-semibold text-gray-500 dark:text-dark-300">
                  {{ t('home.landing.personal.compare.cols.opaque') }}
                </th>
                <th class="px-4 py-3 text-fluid-sm font-semibold text-gray-500 dark:text-dark-300">
                  {{ t('home.landing.personal.compare.cols.official') }}
                </th>
                <th
                  class="rounded-t-lg bg-primary-500/10 px-4 py-3 text-fluid-sm font-bold text-primary-600 dark:bg-primary-500/15 dark:text-primary-400">
                  {{ t('home.landing.personal.compare.cols.apexone') }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in compareRows" :key="row.label"
                class="border-b border-gray-100 last:border-0 dark:border-dark-800">
                <td class="py-3 pr-4 text-fluid-xs font-semibold text-gray-900 dark:text-white">{{ row.label }}</td>
                <td class="px-4 py-3 text-fluid-xs text-gray-500 dark:text-dark-400">
                  <span class="inline-flex items-center gap-1.5">
                    <StatusIcon v-if="row.opaque.icon" :type="row.opaque.icon" />{{ row.opaque.text }}
                  </span>
                </td>
                <td class="px-4 py-3 text-fluid-xs text-gray-500 dark:text-dark-400">
                  <span class="inline-flex items-center gap-1.5">
                    <StatusIcon v-if="row.official.icon" :type="row.official.icon" />{{ row.official.text }}
                  </span>
                </td>
                <td
                  class="bg-primary-500/10 px-4 py-3 text-fluid-xs font-semibold text-primary-700 dark:bg-primary-500/15 dark:text-primary-300">
                  <span class="inline-flex items-center gap-1.5">
                    <StatusIcon v-if="row.apexone.icon" :type="row.apexone.icon" />{{ row.apexone.text }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
          <div class="mt-2 text-right text-fluid-2xs text-gray-400 dark:text-dark-500">
            {{ t('home.landing.personal.compare.note') }}
          </div>
        </div>

        <!-- privacy + uptime -->
        <div class="mt-4 grid gap-4 md:grid-cols-2">
          <div v-reveal :class="cardClass" class="reveal-item p-5">
            <div class="mb-3 text-xl">🛡</div>
            <h3 class="mb-1 text-fluid-base font-semibold text-gray-900 dark:text-white">
              {{ t('home.landing.personal.privacy.title') }}
            </h3>
            <p class="text-fluid-sm text-gray-500 dark:text-dark-400">{{ t('home.landing.personal.privacy.desc') }}</p>
            <div class="mt-3 space-y-1.5">
              <div v-for="point in privacyPoints" :key="point.text" :class="checkItemClass">
                <span class="mt-px font-bold text-primary-600 dark:text-primary-400">{{ point.mark }}</span>
                <span>{{ point.text }}</span>
              </div>
            </div>
          </div>

          <div v-reveal :class="cardClass" class="reveal-item p-5" style="transition-delay: 70ms">
            <div class="mb-3 text-xl">⚡</div>
            <h3 class="mb-1 text-fluid-base font-semibold text-gray-900 dark:text-white">
              {{ t('home.landing.personal.uptime.title') }}
            </h3>
            <p class="text-fluid-sm text-gray-500 dark:text-dark-400">{{ t('home.landing.personal.uptime.desc') }}</p>
            <div class="mt-3 flex items-stretch gap-[2px] sm:gap-[3px]">
              <div v-for="(cell, i) in uptimeCells" :key="i" class="h-5 min-w-0 flex-1 rounded-[2px] sm:rounded-[3px]"
                :class="cell.warn ? 'bg-amber-400' : 'bg-primary-500 opacity-85'"></div>
            </div>
            <!-- 这里原来钉着一个 99.99% 的可用性数字。我们没有对应的 SLA 赔付条款,
                 印在首页上就是一句兑现不了的承诺,换成讲机制。 -->
            <div class="mt-2.5 text-fluid-xl font-bold text-primary-600 dark:text-primary-400">
              {{ t('home.landing.personal.uptime.headline') }}
            </div>
            <div class="text-fluid-2xs text-gray-400 dark:text-dark-500">
              {{ t('home.landing.personal.uptime.window') }}
            </div>
            <div class="mt-3 space-y-1.5">
              <div v-for="point in uptimePoints" :key="point" :class="checkItemClass">
                <span class="mt-px font-bold text-primary-600 dark:text-primary-400">✓</span>
                <span>{{ point }}</span>
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

        <div class="mx-auto flex w-full flex-col gap-5 md:w-3/5 md:flex-row">
          <div v-for="(plan, index) in pricingPlans" :key="plan.name" v-reveal
            class="reveal-item relative flex-1 rounded-xl bg-white/80 p-6 pb-16 shadow-card backdrop-blur-sm dark:bg-dark-800/60"
            :style="{ transitionDelay: `${index * 90}ms` }"
            :class="plan.featured ? 'border-2 border-primary-500' : 'border border-gray-200 dark:border-dark-700'">
            <div class="mb-1.5 text-fluid-2xs font-semibold uppercase tracking-wider text-primary-600 dark:text-primary-400">
              {{ plan.name }} — {{ plan.tagline }}
            </div>
            <div class="mb-1 text-fluid-sm font-semibold text-primary-600 dark:text-primary-400">{{ plan.priceLine }}</div>
            <p class="mb-5 text-fluid-sm text-gray-500 dark:text-dark-400">{{ plan.desc }}</p>
            <ul class="mb-6 space-y-2">
              <li v-for="feat in plan.features" :key="feat"
                class="flex items-start gap-1.5 text-fluid-sm text-gray-700 dark:text-dark-200">
                <span class="mt-px shrink-0 font-bold text-primary-600 dark:text-primary-400">✓</span> {{ feat }}
              </li>
            </ul>
            <!-- <a v-if="plan.ctaHref" :href="plan.ctaHref"
              class="absolute inset-x-6 bottom-6 rounded-lg py-3 text-center text-fluid-sm font-semibold transition-colors"
              :class="plan.featured
                ? 'bg-primary-500 text-white hover:bg-primary-600'
                : 'border border-gray-300 text-gray-700 hover:border-primary-500 dark:border-dark-600 dark:text-dark-200'"
            >
              {{ plan.cta }}
            </a> -->
            <router-link :to="ctaTarget"
              class="absolute inset-x-6 bottom-6 rounded-lg py-3 text-center text-fluid-sm font-semibold transition-colors"
              :class="plan.featured
                ? 'bg-primary-500 text-white hover:bg-primary-600'
                : 'border border-gray-300 text-gray-700 hover:border-primary-500 dark:border-dark-600 dark:text-dark-200'">
              {{ plan.cta }}
            </router-link>
          </div>
        </div>
      </section>

      <!-- SUPPLY — 供给侧入口。hero / 底部 CTA 的次要按钮都锚到这里 -->
      <section id="supply" class="mx-auto mt-14 max-w-5xl scroll-mt-24">
        <div v-reveal class="reveal-item">
          <span :class="eyebrowClass">{{ t('home.landing.supply.eyebrow') }}</span>
          <h2 :class="headingClass" class="mt-1.5">{{ t('home.landing.supply.title') }}</h2>
          <p :class="subClass" class="mb-6 mt-2 max-w-3xl">{{ t('home.landing.supply.subtitle') }}</p>
        </div>

        <!--
          共享者视角的片子，**跟着界面语言换中英版本**（见 contributorVideoUrl）。

          语言在这里不是锦上添花：共享者要把订阅凭证交出来，片子讲的是这件事
          安全在哪、钱怎么算。看不懂的语言等于没讲。消费侧那支是通用宣传片，
          没有对应的语言版本，所以只有这一支做了切换。
        -->
        <div v-if="contributorVideoUrl" v-reveal class="reveal-item mb-6">
          <div :class="videoBadgeClass">
            <span aria-hidden="true">💰</span>
            <span>{{ t('home.landing.supply.video.audience') }}</span>
          </div>
          <h3 class="mb-1 mt-3 text-fluid-lg font-bold tracking-tight text-gray-900 dark:text-white">
            {{ t('home.landing.supply.video.title') }}
          </h3>
          <p :class="subClass" class="mb-3">{{ t('home.landing.supply.video.subtitle') }}</p>
          <div :class="cardClass" class="overflow-hidden p-2 md:p-3">
            <!--
              没有 poster：这支片子没做封面图，VideoPlayer 会退到深色渐变垫底，
              比借用消费侧那张封面诚实——那张图讲的是另一件事。
            -->
            <VideoPlayer
              :src="contributorVideoUrl"
              fit="contain"
              data-testid="home-contributor-video"
            />
          </div>
        </div>

        <!-- 怎么运作 -->
        <h3 class="mb-3 text-fluid-base font-semibold text-gray-900 dark:text-white">
          {{ t('home.landing.supply.howItWorks.title') }}
        </h3>
        <div class="grid gap-4 md:grid-cols-3">
          <div v-for="(step, index) in supplySteps" :key="step.title" v-reveal :class="cardClass"
            class="reveal-item p-5" :style="{ transitionDelay: `${index * 70}ms` }">
            <div class="mb-2 font-mono text-fluid-2xs font-semibold text-primary-600 dark:text-primary-400">
              0{{ index + 1 }}
            </div>
            <h4 class="mb-1 text-fluid-base font-semibold text-gray-900 dark:text-white">{{ step.title }}</h4>
            <p class="text-fluid-sm text-gray-500 dark:text-dark-400">{{ step.desc }}</p>
          </div>
        </div>

        <!-- 收益怎么算 / 收益怎么拿 -->
        <div class="mt-4 grid gap-4 md:grid-cols-2">
          <div v-reveal :class="cardClass" class="reveal-item p-5">
            <div class="mb-3 text-xl">📈</div>
            <h3 class="mb-1 text-fluid-base font-semibold text-gray-900 dark:text-white">
              {{ t('home.landing.supply.earnings.title') }}
            </h3>
            <div :class="checkItemClass" class="mt-3 font-mono">
              <span>{{ t('home.landing.supply.earnings.formula') }}</span>
            </div>
            <div class="mt-2.5 text-fluid-xl font-bold text-primary-600 dark:text-primary-400">
              {{ t('home.landing.supply.earnings.ratio') }}
            </div>
            <!-- 早期供给者最容易受伤的就是收入预期。这句刻意不做成红色警告框——
                 它是如实说明,不是风险提示;但字号不缩,免得被当成小字免责条款。 -->
            <p class="mt-3 border-l-2 border-gray-200 pl-3 text-fluid-sm leading-relaxed text-gray-400 dark:border-dark-700 dark:text-dark-500">
              {{ t('home.landing.supply.earnings.disclaimer') }}
            </p>
          </div>

          <div v-reveal :class="cardClass" class="reveal-item p-5" style="transition-delay: 70ms">
            <div class="mb-3 text-xl">💸</div>
            <h3 class="mb-3 text-fluid-base font-semibold text-gray-900 dark:text-white">
              {{ t('home.landing.supply.payout.title') }}
            </h3>
            <div class="space-y-1.5">
              <div v-for="point in supplyPayoutPoints" :key="point" :class="checkItemClass">
                <span class="mt-px font-bold text-primary-600 dark:text-primary-400">✓</span>
                <span>{{ point }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- 供给者视角的隐私:他不经手数据,这也是共享订阅能成立的前提 -->
        <div v-reveal :class="cardClass" class="reveal-item mt-4 p-5">
          <div class="mb-3 text-xl">🛡</div>
          <h3 class="mb-1 text-fluid-base font-semibold text-gray-900 dark:text-white">
            {{ t('home.landing.supply.privacy.title') }}
          </h3>
          <p class="max-w-3xl text-fluid-sm text-gray-500 dark:text-dark-400">
            {{ t('home.landing.supply.privacy.desc') }}
          </p>
        </div>

        <!-- 与 hero 那张卡同一条判断:这个按钮真的会把人送进 /supply,
             功能关着时它通向的是一句"尚未开放"。介绍文字留着——那是产品说明,不是入口。 -->
        <div v-if="showSupplyEntry" class="mt-6 flex justify-center">
          <router-link :to="supplyTarget" :class="outlineBtn">
            {{ t('home.landing.supply.cta') }}
          </router-link>
        </div>
      </section>

      <!--
        这里原来是 id="why" 的独立视频段。片子已经搬进 #personal（消费侧那段），
        id 一起去掉：它不在 Header 的导航清单里（architecture / personal / pricing
        / /proof / 文档），没有任何入口锚到它，留着只是一个死锚点。
      -->

      <!-- CTA -->
      <section class="mx-auto mt-14 max-w-5xl">
        <div v-reveal
          class="reveal-item rounded-xl border border-primary-500/30 bg-primary-500/10 px-6 py-14 text-center dark:bg-primary-500/15">
          <h2 class="mb-2 text-fluid-2xl font-bold tracking-tight text-gray-900 dark:text-white">
            {{ t('home.landing.cta.title') }}
          </h2>
          <p class="mb-7 text-fluid-sm text-gray-500 dark:text-dark-400">
            {{ t('home.landing.cta.description') }}
          </p>
          <div class="flex flex-col gap-2 md:flex-row md:justify-center">
            <router-link :to="ctaTarget" class="btn btn-primary px-6 py-3 text-fluid-base shadow-lg shadow-primary-500/30">
              {{ t('home.landing.cta.primary') }}
            </router-link>
            <router-link to="/proof" :class="outlineBtn">
              {{ t('home.landing.cta.secondary') }}
            </router-link>
            <a v-if="showSupplyEntry" href="#supply" :class="outlineBtn" @click.prevent="scrollToSupply">
              {{ t('home.landing.cta.supply') }}
            </a>
          </div>
          <div class="mt-7 flex flex-wrap justify-center gap-2">
            <span v-for="pill in ctaPills" :key="pill"
              class="inline-flex items-center gap-1.5 rounded-full border border-gray-200 bg-white/80 px-3 py-1.5 text-fluid-xs text-gray-500 dark:border-dark-700 dark:bg-dark-800/60 dark:text-dark-300">
              {{ pill }}
            </span>
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
import { useSupplyStore } from '@/stores/supply'
import AppFooter from '@/components/layout/AppFooter.vue'
import Header from '@/components/layout/Header.vue'
import StatusIcon from '@/components/icons/StatusIcon.vue'
import VideoPlayer from '@/components/common/VideoPlayer.vue'
import promoPosterUrl from '@/assets/promo-poster.jpg'

// locale 用于按语言选共享者视频（见下方 contributorVideoUrl）
const { t, locale } = useI18n()

const appStore = useAppStore()
const authStore = useAuthStore()
const supplyStore = useSupplyStore()

// Shared utility class fragments (kept here so the template stays declarative)
const cardClass =
  'rounded-xl border border-gray-200 bg-white/80 shadow-card backdrop-blur-sm dark:border-dark-700 dark:bg-dark-800/60'
const eyebrowClass =
  'font-mono text-fluid-2xs font-semibold uppercase tracking-[0.1em] text-primary-600 dark:text-primary-400'
const headingClass = 'text-fluid-2xl font-bold tracking-tight text-gray-900 dark:text-white'
const subClass = 'text-fluid-sm text-gray-500 dark:text-dark-400'
const checkItemClass =
  'flex items-start gap-2 rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 text-fluid-xs text-gray-700 dark:border-dark-700 dark:bg-dark-900/50 dark:text-dark-200'
const outlineBtn =
  'inline-flex items-center justify-center rounded-lg border border-gray-300 px-6 py-3 text-fluid-base font-medium text-gray-700 transition-colors hover:border-primary-500 dark:border-dark-600 dark:text-dark-200'
// 两张入口卡片共用一份样式:权重相当这件事必须由样式本身保证,
// 分成两份迟早会有一边被"顺手"调大。
const entryCardClass =
  'flex flex-col rounded-xl border border-gray-200 bg-white/80 p-6 shadow-card backdrop-blur-sm transition-all hover:-translate-y-0.5 hover:border-primary-500 hover:shadow-lg dark:border-dark-700 dark:bg-dark-800/60'

// HERO stats
const heroStats = computed(() => [
  // 数值保持语言中立（百分号各语言通用），折扣说法只出现在文案里——
  // 把「1.8折」写进 value 会让英文页面显示中文。
  { value: '14%', label: t('home.landing.hero.stats.discount') },
  { value: '100%', label: t('home.landing.hero.stats.attested') },
  { value: 'Fable 5.1', label: t('home.landing.hero.stats.claude') },
  { value: 'Hermes', label: t('home.landing.hero.stats.hermes') }
])

// APEXONE comparison table
const compareRowKeys = ['attestation', 'dataAccess', 'fable', 'failover', 'price'] as const

// 比较表单元格文案以 emoji 前缀表达状态（✅/❌/⚠️），这里把前缀拆出来
// 换成 StatusIcon 组件渲染，i18n 文案保持不变，无 emoji 的单元格（如价格）原样显示。
type StatusIconType = 'check' | 'cross' | 'warn'

const iconByEmoji: Record<string, StatusIconType> = {
  '✅': 'check',
  '❌': 'cross',
  '⚠️': 'warn',
  '⚠': 'warn'
}

function parseCell(value: string): { icon: StatusIconType | null; text: string } {
  const trimmed = value.trimStart()
  for (const [emoji, icon] of Object.entries(iconByEmoji)) {
    if (trimmed.startsWith(emoji)) {
      return { icon, text: trimmed.slice(emoji.length).trimStart() }
    }
  }
  return { icon: null, text: value }
}

const compareRows = computed(() =>
  compareRowKeys.map((key) => ({
    label: t(`home.landing.personal.compare.rows.${key}.label`),
    opaque: parseCell(t(`home.landing.personal.compare.rows.${key}.opaque`)),
    official: parseCell(t(`home.landing.personal.compare.rows.${key}.official`)),
    apexone: parseCell(t(`home.landing.personal.compare.rows.${key}.apexone`))
  }))
)

const uptimeCells = Array.from({ length: 25 }, (_, i) => ({ warn: i === 11 }))

// 三条供货通道的数据流向不同,必须分开陈述:自营和共享订阅的请求确实不出 TEE,
// 但 API 中转通道会走到供给者自己的服务端点上。中转那条用 → 而不是 ✓——它是
// 如实披露"请求会去哪",把它也画成对勾等于把一条限制说成一项卖点。
const privacyPoints = computed(() => [
  { mark: '✓', text: t('home.landing.personal.privacy.points.owned') },
  { mark: '✓', text: t('home.landing.personal.privacy.points.shared') },
  { mark: '→', text: t('home.landing.personal.privacy.points.relay') }
])

const uptimePoints = computed(() => [
  t('home.landing.personal.uptime.points.failover'),
  t('home.landing.personal.uptime.points.monitoring')
])

// PRICING plans
const pricingPlans = computed(() => [
  {
    name: t('home.landing.pricing.personal.name'),
    tagline: t('home.landing.pricing.personal.tagline'),
    priceLine: t('home.landing.pricing.personal.priceLine'),
    desc: t('home.landing.pricing.personal.desc'),
    cta: t('home.landing.pricing.personal.cta'),
    featured: true,
    features: [1, 2, 3, 4, 5, 6].map((i) => t(`home.landing.pricing.personal.features.f${i}`))
  }
])

// SUPPLY
const supplySteps = computed(() =>
  (['s1', 's2', 's3'] as const).map((key) => ({
    title: t(`home.landing.supply.howItWorks.${key}.title`),
    desc: t(`home.landing.supply.howItWorks.${key}.desc`)
  }))
)

const supplyPayoutPoints = computed(() => [
  t('home.landing.supply.payout.chain'),
  t('home.landing.supply.payout.fee'),
  t('home.landing.supply.payout.freeze')
])

// ── 视频 ─────────────────────────────────────────────────────────────────────
//
// 两支片子面向两类人，各自跟着它说的那段内容走：
//   consumer    → #personal（怎么用、便宜在哪、隐私怎么保证）
//   contributor → #supply  （怎么共享订阅、钱怎么算）

const VIDEO_BASE = 'https://publicai.s3.ap-east-1.amazonaws.com/common'

const consumerVideoUrl = `${VIDEO_BASE}/apex1-launch-47s-v4.mp4`
const promoVideoPoster = promoPosterUrl

/**
 * 共享者视频按界面语言取中/英版本。
 *
 * 用 locale 而不是浏览器语言：界面上写着什么语言，片子就该是什么语言，
 * 用户手动切过语言之后尤其如此（切了界面却还在放另一种语言的片子，
 * 会让人以为切换没生效）。
 *
 * locale 的取值只有 'en' | 'zh'（见 i18n/index.ts 的 LocaleCode），
 * 但这里仍然写成「不是 zh 就走 en」而不是穷举：将来加第三种语言时，
 * 缺失的片子退到英文，比拼出一个 404 的地址好。
 */
const contributorVideoUrl = computed(() =>
  locale.value.startsWith('zh')
    ? `${VIDEO_BASE}/apex1_contributor_zh.mp4`
    : `${VIDEO_BASE}/apex1_contributor_en.mp4`
)

// 两支片子上方那条受众标签共用一份样式：它们的意义就是「这两块是同一种东西，
// 只是给不同的人看」，分成两份迟早会有一边被顺手调走。
const videoBadgeClass =
  'inline-flex items-center gap-1.5 rounded-full border border-primary-500/30 bg-primary-500/10 px-3 py-1 text-fluid-2xs font-semibold text-primary-600 dark:bg-primary-500/20 dark:text-primary-400'

// CTA pills
const ctaPills = computed(() => [
  t('home.landing.cta.pills.noTraining'),
  t('home.landing.cta.pills.audit'),
  t('home.landing.cta.pills.encrypted')
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

// Auth state
const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))
const ctaTarget = computed(() => (isAuthenticated.value ? dashboardPath.value : '/login'))
// /supply 是 requiresAuth 的,未登录直接跳过去只会被守卫弹回登录页
const supplyTarget = computed(() => (isAuthenticated.value ? '/supply' : '/login'))

// 访客身上读不到供给开关（那是按用户的接口），所以未登录一律显示;
// 登录之后才用真实开关决定,免得有人从这里点进一个"尚未开放"的页面。
const showSupplyEntry = computed(() => !isAuthenticated.value || supplyStore.enabled)

// 与 Header 的锚点导航保持同一种滚动手感
function scrollToSupply() {
  document.getElementById('supply')?.scrollIntoView({ behavior: 'smooth' })
}

// 分流意图：已登录直接落到那个人名下（setMode），未登录先记成待应用意图
// （setPendingMode），由 supplyStore 在登录后消化。跳转由 router-link 的 :to 负责。
function chooseUsage(): void {
  if (isAuthenticated.value) supplyStore.setMode('usage')
  else supplyStore.setPendingMode('usage')
}
function chooseSharing(): void {
  if (isAuthenticated.value) supplyStore.setMode('sharing')
  else supplyStore.setPendingMode('sharing')
}

onMounted(() => {
  authStore.checkAuth()

  // 只在登录态问一次。未登录时这个接口必然 401，为了一张反正要显示的卡片
  // 去打一次注定失败的请求，只会在访客的控制台里留下一条红色报错。
  if (authStore.isAuthenticated) {
    void supplyStore.ensureStatus()
  }

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
