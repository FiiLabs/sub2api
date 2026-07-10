<template>
  <div class="min-h-screen bg-gray-50 pt-16 text-gray-900 dark:bg-dark-950 dark:text-white md:pt-[72px]">
    <Header />

    <main class="mx-auto max-w-4xl px-4 py-8 sm:px-6 lg:py-10">
      <!-- Hero / scope -->
      <section class="mb-8">
        <div class="flex items-start gap-4">
          <span class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-md bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300">
            <Icon name="shield" size="md" />
          </span>
          <div class="min-w-0">
            <h1 class="break-words text-2xl font-bold tracking-normal text-gray-950 dark:text-white sm:text-3xl">
              {{ t('proof.hero.title') }}
            </h1>
            <p class="mt-3 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('proof.hero.subtitle') }}</p>
          </div>
        </div>
        <!-- Honest global status: NOT verified yet (no decorative green check). -->
        <div class="mt-5 flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
          <Icon name="shield" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>{{ t('proof.hero.scopeNote') }}</span>
        </div>
      </section>

      <!-- Prompt journey (4 hops) -->
      <section class="mb-10">
        <h2 class="mb-4 text-lg font-semibold text-gray-900 dark:text-white">{{ t('proof.journey.title') }}</h2>

        <!-- Beginner-friendly flow diagram: where your prompt goes + who sees plaintext -->
        <div class="mb-6 overflow-x-auto rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900 sm:p-5">
          <div class="flex min-w-[640px] items-stretch gap-2">
            <!-- 1. Your device -->
            <div class="flex flex-1 flex-col items-center gap-1.5 rounded-lg border border-gray-200 bg-gray-50 p-3 text-center dark:border-dark-700 dark:bg-dark-800/60">
              <span class="text-2xl">📱</span>
              <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('proof.flow.device') }}</span>
              <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('proof.flow.deviceSub') }}</span>
            </div>

            <!-- encrypted arrow -->
            <div class="flex flex-col items-center justify-center px-1">
              <span class="whitespace-nowrap text-[10px] font-medium text-primary-600 dark:text-primary-400">🔒 {{ t('proof.flow.arrEncrypted') }}</span>
              <span class="text-2xl leading-none text-gray-300 dark:text-dark-600">→</span>
            </div>

            <!-- TEE boundary wrapping gateway + meridian -->
            <div class="flex flex-[2.4] flex-col rounded-xl border-2 border-dashed border-primary-400/60 bg-primary-50/40 p-3 dark:border-primary-500/40 dark:bg-primary-500/5">
              <span class="mb-2 inline-flex items-center justify-center gap-1 text-center text-[11px] font-semibold text-primary-700 dark:text-primary-300">
                🔒 {{ t('proof.flow.teeLabel') }}
              </span>
              <div class="flex items-stretch gap-2">
                <div class="flex flex-1 flex-col items-center gap-1 rounded-lg bg-white p-2.5 text-center shadow-sm dark:bg-dark-800">
                  <span class="text-xl">🌐</span>
                  <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('proof.flow.gateway') }}</span>
                  <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('proof.flow.gatewaySub') }}</span>
                </div>
                <div class="flex flex-col items-center justify-center">
                  <span class="whitespace-nowrap text-[10px] font-medium text-primary-600 dark:text-primary-400">{{ t('proof.flow.arrInternal') }}</span>
                  <span class="text-lg leading-none text-primary-300 dark:text-primary-500">→</span>
                </div>
                <div class="flex flex-1 flex-col items-center gap-1 rounded-lg bg-white p-2.5 text-center shadow-sm dark:bg-dark-800">
                  <span class="text-xl">🌉</span>
                  <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('proof.flow.meridian') }}</span>
                  <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('proof.flow.meridianSub') }}</span>
                </div>
              </div>
            </div>

            <!-- plaintext arrow -->
            <div class="flex flex-col items-center justify-center px-1">
              <span class="whitespace-nowrap text-[10px] font-medium text-amber-600 dark:text-amber-400">⚠ {{ t('proof.flow.arrPlaintext') }}</span>
              <span class="text-2xl leading-none text-gray-300 dark:text-dark-600">→</span>
            </div>

            <!-- 4. Anthropic -->
            <div class="flex flex-1 flex-col items-center gap-1.5 rounded-lg border border-amber-200 bg-amber-50 p-3 text-center dark:border-amber-500/30 dark:bg-amber-500/10">
              <span class="text-2xl">🤖</span>
              <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('proof.flow.anthropic') }}</span>
              <span class="text-xs text-amber-700 dark:text-amber-300">{{ t('proof.flow.anthropicSub') }}</span>
            </div>
          </div>

          <!-- legend + one-liner -->
          <div class="mt-4 flex flex-col gap-2 border-t border-gray-100 pt-3 dark:border-dark-800">
            <div class="flex flex-wrap gap-x-4 gap-y-1 text-xs">
              <span class="inline-flex items-center gap-1.5 text-gray-600 dark:text-dark-300"><span class="h-2.5 w-2.5 flex-shrink-0 rounded-full bg-primary-500"></span>{{ t('proof.flow.legendConfidential') }}</span>
              <span class="inline-flex items-center gap-1.5 text-gray-600 dark:text-dark-300"><span class="h-2.5 w-2.5 flex-shrink-0 rounded-full bg-amber-500"></span>{{ t('proof.flow.legendPlaintext') }}</span>
            </div>
            <p class="text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('proof.flow.caption') }}</p>
          </div>
        </div>

        <!-- Accordion: the status badge stays visible on the collapsed header row so
             progress is readable at a glance; claim + basis collapse to cut text. -->
        <div class="space-y-3">
          <div
            v-for="(hop, i) in hops"
            :key="hop.key"
            class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900"
          >
            <button
              type="button"
              class="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-gray-50 dark:hover:bg-dark-800/60"
              :aria-expanded="isHopOpen(hop)"
              @click="toggleHop(hop.key)"
            >
              <span
                class="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full text-[10px] font-bold text-white"
                :class="hop.confidential ? 'bg-primary-600' : 'bg-amber-500'"
              >{{ i + 1 }}</span>
              <span class="min-w-0 flex-1 truncate font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ hop.node }}</span>
              <span
                class="inline-flex flex-shrink-0 items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-medium"
                :class="statusClass(hop.status)"
              >{{ t('proof.status.' + hop.status) }}</span>
              <svg
                class="h-4 w-4 flex-shrink-0 text-gray-400 transition-transform dark:text-dark-500"
                :class="{ 'rotate-90': isHopOpen(hop) }"
                fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"
              >
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
              </svg>
            </button>
            <div
              v-show="isHopOpen(hop)"
              class="border-t border-gray-100 px-4 py-3 pl-[52px] dark:border-dark-800"
            >
              <p class="text-sm leading-6 text-gray-600 dark:text-dark-300">{{ hop.claim }}</p>
              <p class="mt-2 text-xs text-gray-400 dark:text-dark-500">{{ t('proof.journey.basisLabel') }}: {{ hop.basis }}</p>
            </div>
          </div>
        </div>
      </section>

      <!-- Honest disclosure: Anthropic sees plaintext -->
      <section class="mb-10 rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('proof.disclosure.title') }}</h2>
        <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('proof.disclosure.body') }}</p>
        <a
          href="https://www.anthropic.com/legal/privacy"
          target="_blank"
          rel="noopener noreferrer"
          class="mt-2 inline-block text-sm text-primary-600 underline underline-offset-4 hover:text-primary-700 dark:text-primary-300"
        >{{ t('proof.disclosure.policyLink') }} →</a>
      </section>

      <!-- Live attestation report -->
      <section class="mb-10">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('proof.live.title') }}</h2>
        <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('proof.live.desc') }}</p>

        <div class="mt-4 flex flex-wrap items-center gap-3">
          <button
            type="button"
            :disabled="busy"
            class="inline-flex items-center gap-2 rounded-lg bg-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-sm transition hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-60"
            @click="loadReport"
          >
            <span v-if="busy" class="h-4 w-4 animate-spin rounded-full border-b-2 border-white"></span>
            {{ busy ? t('proof.live.fetching') : (report ? t('proof.live.refetch') : t('proof.live.fetch')) }}
          </button>
          <button
            v-if="report"
            type="button"
            class="inline-flex items-center rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 transition hover:bg-gray-50 dark:border-dark-600 dark:text-dark-200 dark:hover:bg-dark-800"
            @click="downloadReport"
          >{{ t('proof.live.download') }}</button>
          <span v-if="nonce" class="font-mono text-xs text-gray-400 dark:text-dark-500">{{ t('proof.live.nonce') }}: {{ nonce }}</span>
        </div>

        <p class="mt-3 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('proof.live.autoNote') }}</p>

        <!-- Where the data + verification collateral come from — all click-through. -->
        <dl class="mt-3 space-y-1 text-xs text-gray-500 dark:text-dark-400">
          <div class="flex flex-wrap gap-x-2">
            <dt class="font-medium">{{ t('proof.live.endpointLabel') }}:</dt>
            <dd class="min-w-0">
              <a :href="liveReportUrl" target="_blank" rel="noopener noreferrer" class="break-all font-mono text-primary-600 underline decoration-primary-300/40 underline-offset-4 hover:text-primary-700 hover:decoration-primary-500 dark:text-primary-300">{{ liveReportUrl }}</a>
            </dd>
          </div>
          <div class="flex flex-wrap gap-x-2">
            <dt class="font-medium">{{ t('proof.live.collateralLabel') }}:</dt>
            <dd class="min-w-0">
              <a :href="PHALA_PCCS_URL" target="_blank" rel="noopener noreferrer" class="break-all font-mono text-primary-600 underline decoration-primary-300/40 underline-offset-4 hover:text-primary-700 hover:decoration-primary-500 dark:text-primary-300">{{ PHALA_PCCS_URL }}</a>
            </dd>
          </div>
          <div class="flex flex-wrap gap-x-2">
            <dt class="font-medium">{{ t('proof.live.explorerLabel') }}:</dt>
            <dd class="min-w-0">
              <a href="https://proof.t16z.com/" target="_blank" rel="noopener noreferrer" class="break-all font-mono text-primary-600 underline decoration-primary-300/40 underline-offset-4 hover:text-primary-700 hover:decoration-primary-500 dark:text-primary-300">proof.t16z.com</a>
            </dd>
          </div>
        </dl>

        <p v-if="fetchError" class="mt-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200">
          {{ t('proof.live.error') }}
        </p>

        <!-- Full-chain verification result: ACI binding (2a) + TDX hardware quote
             (2b). A hop turns green only when its gating checks pass. -->
        <div
          v-if="overallState !== 'idle'"
          class="mt-4 rounded-lg border px-4 py-3 text-sm"
          :class="bannerClass"
        >
          <p class="flex items-center gap-2 font-semibold">
            <span v-if="busy" class="h-3.5 w-3.5 flex-shrink-0 animate-spin rounded-full border-b-2 border-current"></span>
            {{ bannerTitle }}
          </p>
          <!-- On a real failure, name the failing check(s) in plain English up top so
               the user knows exactly what broke, not just "FAILED". -->
          <ul v-if="overallState === 'fail' && failingChecks.length" class="mt-2 space-y-1 font-medium">
            <li v-for="c in failingChecks" :key="c.id" class="flex items-baseline gap-2">
              <span aria-hidden="true">✗</span>
              <span>{{ friendlyCheckName(c.id) }}</span>
            </li>
          </ul>
          <p class="mt-1 leading-6">{{ bannerNote }}</p>
          <!-- Device-clock offset: name the amount + direction and tell the user exactly
               how to fix it, making clear the TEE/our security is not at fault. -->
          <div v-if="overallState === 'clockSkew'" class="mt-3 space-y-2 leading-6">
            <p>{{ t('proof.verify.clockSkew.detail', { amount: skewAmount, direction: skewDirection }) }}</p>
            <p class="font-medium">{{ t('proof.verify.clockSkew.fix') }}</p>
          </div>
          <!-- Full per-check list lives in the collapsed "For auditors" section below;
               the banner keeps only the headline + named failing checks. -->
          <!-- "Live" proof: show when this ran and let the user re-run against a fresh nonce. -->
          <div v-if="verifiedAt && !busy" class="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1 border-t border-current/10 pt-2 text-xs opacity-70">
            <span>{{ t('proof.verify.lastVerified', { time: verifiedAtLabel }) }}</span>
            <span aria-hidden="true">·</span>
            <button type="button" class="font-semibold underline underline-offset-2 hover:opacity-100" @click="loadReport">{{ t('proof.verify.rerun') }}</button>
          </div>
        </div>

      </section>

      <!-- Path A — hop-1 browser-level E2EE proof -->
      <section class="mb-10">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('proof.e2ee.title') }}</h2>
        <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('proof.e2ee.desc') }}</p>

        <!-- Verification-side status (always-on, free) -->
        <div
          v-if="e2eeResult"
          class="mt-4 flex items-start gap-2 rounded-lg border px-4 py-3 text-sm"
          :class="e2eeResult.ok
            ? 'border-green-200 bg-green-50 text-green-800 dark:border-green-500/30 dark:bg-green-500/10 dark:text-green-200'
            : 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200'"
        >
          <Icon name="shield" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>{{ e2eeResult.ok ? t('proof.e2ee.statusVerified') : t('proof.e2ee.statusUnavailable') }}</span>
        </div>
        <p v-else class="mt-4 text-sm text-gray-400 dark:text-dark-500">{{ t('proof.e2ee.statusIdle') }}</p>

        <!-- Live round-trip (opt-in) -->
        <div class="mt-5 rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('proof.e2ee.live.title') }}</h3>
          <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('proof.e2ee.live.desc') }}</p>

          <!-- State the proof up front so the point is clear even before running -->
          <div class="mt-3 rounded-lg border border-primary-200 bg-primary-50 px-4 py-3 dark:border-primary-500/30 dark:bg-primary-500/10">
            <p class="text-xs font-semibold text-primary-700 dark:text-primary-300">🎯 {{ t('proof.e2ee.live.claimTitle') }}</p>
            <p class="mt-1 text-xs leading-5 text-primary-800/90 dark:text-primary-200/90">{{ t('proof.e2ee.live.claimBody') }}</p>
          </div>

          <div class="mt-3 flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-xs leading-5 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
            <Icon name="shield" size="sm" class="mt-0.5 flex-shrink-0" />
            <span>{{ t('proof.e2ee.live.disclosure') }}</span>
          </div>

          <div class="mt-4 grid gap-3 sm:grid-cols-2">
            <label class="flex flex-col gap-1 text-xs font-medium text-gray-600 dark:text-dark-300">
              {{ t('proof.e2ee.live.apiKeyLabel') }}
              <input
                v-model="apiKeyInput"
                type="password"
                autocomplete="off"
                :placeholder="t('proof.e2ee.live.apiKeyPlaceholder')"
                class="rounded-lg border border-gray-300 px-3 py-2 font-mono text-sm text-gray-900 dark:border-dark-600 dark:bg-dark-800 dark:text-white"
              />
            </label>
            <label class="flex flex-col gap-1 text-xs font-medium text-gray-600 dark:text-dark-300">
              {{ t('proof.e2ee.live.modelLabel') }}
              <input
                v-model="modelInput"
                type="text"
                class="rounded-lg border border-gray-300 px-3 py-2 font-mono text-sm text-gray-900 dark:border-dark-600 dark:bg-dark-800 dark:text-white"
              />
            </label>
          </div>
          <label class="mt-3 flex flex-col gap-1 text-xs font-medium text-gray-600 dark:text-dark-300">
            {{ t('proof.e2ee.live.promptLabel') }}
            <textarea
              v-model="promptInput"
              rows="2"
              class="rounded-lg border border-gray-300 px-3 py-2 text-sm text-gray-900 dark:border-dark-600 dark:bg-dark-800 dark:text-white"
            ></textarea>
          </label>

          <button
            type="button"
            :disabled="liveRunning || !report || !apiKeyInput.trim()"
            class="mt-4 inline-flex items-center gap-2 rounded-lg bg-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-sm transition hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-60"
            @click="runLive"
          >
            <span v-if="liveRunning" class="h-4 w-4 animate-spin rounded-full border-b-2 border-white"></span>
            {{ liveRunning ? t('proof.e2ee.live.running') : t('proof.e2ee.live.run') }}
          </button>

          <!-- Result: make the proof VISIBLE — who sees plaintext vs only ciphertext -->
          <div
            v-if="liveResult"
            class="mt-4 rounded-lg border px-4 py-3 text-sm"
            :class="liveBoxClass"
          >
            <template v-if="liveResult.ok">
              <p class="font-semibold">{{ t('proof.e2ee.live.successTitle') }}</p>
              <p class="mt-1 leading-6">{{ t('proof.e2ee.live.successNote') }}</p>
              <!-- Who saw what -->
              <p class="mt-4 text-xs font-semibold uppercase tracking-wide opacity-70">{{ t('proof.e2ee.live.seesTitle') }}</p>
              <div class="mt-2 grid gap-2 sm:grid-cols-3">
                <div class="rounded-lg border border-green-300/60 bg-white/60 p-3 dark:border-green-500/30 dark:bg-black/20">
                  <p class="text-xs font-semibold">👁 {{ t('proof.e2ee.live.youLabel') }}</p>
                  <p class="mt-1 text-xs leading-5 opacity-80">{{ t('proof.e2ee.live.youSees') }}</p>
                </div>
                <div class="rounded-lg border border-gray-300 bg-white/60 p-3 dark:border-dark-600 dark:bg-black/20">
                  <p class="text-xs font-semibold">🚫 {{ t('proof.e2ee.live.middleLabel') }}</p>
                  <p class="mt-1 text-xs leading-5 opacity-80">{{ t('proof.e2ee.live.middleSees') }}</p>
                </div>
                <div class="rounded-lg border border-green-300/60 bg-white/60 p-3 dark:border-green-500/30 dark:bg-black/20">
                  <p class="text-xs font-semibold">🔓 {{ t('proof.e2ee.live.enclaveLabel') }}</p>
                  <p class="mt-1 text-xs leading-5 opacity-80">{{ t('proof.e2ee.live.enclaveSees') }}</p>
                </div>
              </div>

              <!-- The actual bytes: ① plaintext prompt  ② wire ciphertext  ③ decrypted reply -->
              <div class="mt-3 space-y-2.5">
                <div>
                  <p class="text-xs font-medium opacity-80">{{ t('proof.e2ee.live.promptSentLabel') }}</p>
                  <pre class="mt-1 overflow-auto whitespace-pre-wrap rounded bg-white/60 p-2 font-mono text-xs text-gray-800 dark:bg-black/20 dark:text-gray-100">{{ liveResult.promptText }}</pre>
                </div>
                <div>
                  <p class="text-xs font-medium opacity-80">{{ t('proof.e2ee.live.wireLabel') }}</p>
                  <pre class="mt-1 max-h-28 overflow-auto whitespace-pre-wrap break-all rounded bg-white/60 p-2 font-mono text-[11px] leading-4 text-gray-500 dark:bg-black/20 dark:text-dark-400">{{ truncateHex(liveResult.requestCiphertext) }}</pre>
                  <p v-if="liveResult.sealedToKeyId" class="mt-1 break-all font-mono text-[11px] text-gray-400 dark:text-dark-500">{{ t('proof.e2ee.live.sealedToLabel') }}: {{ liveResult.sealedToKeyId }}</p>
                </div>
                <div>
                  <p class="text-xs font-medium opacity-80">{{ t('proof.e2ee.live.replyLabel') }}</p>
                  <pre class="mt-1 overflow-auto whitespace-pre-wrap rounded bg-white/60 p-2 font-mono text-xs text-gray-800 dark:bg-black/20 dark:text-gray-100">{{ liveResult.replyText }}</pre>
                </div>
              </div>

              <!-- Conclusion — the takeaway in one sentence -->
              <div class="mt-3 rounded-lg border border-green-300 bg-green-100/70 p-3 dark:border-green-500/40 dark:bg-green-500/10">
                <p class="text-xs font-semibold">✅ {{ t('proof.e2ee.live.concludeTitle') }}</p>
                <p class="mt-1 text-xs leading-5">{{ t('proof.e2ee.live.concludeBody') }}</p>
              </div>
            </template>

            <template v-else>
              <p class="font-semibold">{{ t(failTitleKey) }}</p>
              <p v-if="showEncryptedPrefix" class="mt-1 leading-6">✅ {{ t('proof.e2ee.live.failure.encryptedPrefix') }}</p>
              <p class="mt-1 leading-6">{{ t(failBodyKey) }}</p>

              <div v-if="failAction" class="mt-3">
                <RouterLink
                  v-if="failAction.kind === 'route'"
                  :to="failAction.to"
                  class="inline-flex items-center gap-1 rounded-lg bg-primary-600 px-3.5 py-2 text-sm font-semibold text-white shadow-sm transition hover:bg-primary-700"
                >
                  {{ t(failAction.labelKey) }} →
                </RouterLink>
                <button
                  v-else
                  type="button"
                  :disabled="liveRunning"
                  class="inline-flex items-center gap-2 rounded-lg bg-primary-600 px-3.5 py-2 text-sm font-semibold text-white shadow-sm transition hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-60"
                  @click="runLive"
                >
                  <span v-if="liveRunning" class="h-4 w-4 animate-spin rounded-full border-b-2 border-white"></span>
                  {{ t(failAction.labelKey) }}
                </button>
              </div>

              <p v-if="liveResult.error" class="mt-2 break-all font-mono text-xs opacity-70">{{ liveResult.error }}</p>
              <p v-if="showUnaffected" class="mt-2 text-xs opacity-70">{{ t('proof.e2ee.live.failure.unaffectedNote') }}</p>
            </template>

            <!-- Raw per-step trace, collapsed by default (for the curious, not the newcomer) -->
            <details v-if="liveResult.checks.length" class="mt-3">
              <summary class="cursor-pointer text-xs font-semibold uppercase tracking-wide opacity-60">{{ t('proof.e2ee.live.stepsLabel') }}</summary>
              <ul class="mt-1 space-y-1">
                <li
                  v-for="c in liveResult.checks"
                  :key="c.id"
                  class="flex flex-wrap items-baseline gap-x-2 font-mono text-xs"
                >
                  <span :class="c.ok ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'">{{ c.ok ? '✓' : '✗' }}</span>
                  <span class="text-gray-700 dark:text-dark-200">{{ c.id }}</span>
                  <span v-if="c.detail" class="break-all text-gray-400 dark:text-dark-500">— {{ c.detail }}</span>
                </li>
              </ul>
            </details>
          </div>
        </div>
      </section>

      <!-- For auditors — detailed evidence, collapsed by default to keep the page calm.
           The headline verification result stays visible in the banner above; these
           folds hold the exhaustive per-check list, the raw report, and the reference
           fingerprints for anyone who wants to verify line-by-line. -->
      <section class="mb-10">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('proof.auditors.title') }}</h2>
        <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('proof.auditors.desc') }}</p>

        <div class="mt-4 space-y-3">
          <!-- ① Per-check results -->
          <details v-if="allChecks.length" class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
            <summary class="cursor-pointer select-none px-4 py-3 text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('proof.auditors.checks') }}
              <span class="ml-1 text-xs font-normal text-gray-400 dark:text-dark-500">{{ checksSummary }}</span>
            </summary>
            <div class="border-t border-gray-100 px-4 py-3 dark:border-dark-800">
              <ul class="space-y-1">
                <li
                  v-for="c in allChecks"
                  :key="c.id"
                  class="flex flex-wrap items-baseline gap-x-2 font-mono text-xs"
                >
                  <span :class="checkMark(c).class">{{ checkMark(c).sym }}</span>
                  <span class="text-gray-700 dark:text-dark-200">{{ c.id }}</span>
                  <span v-if="c.detail" class="break-all text-gray-400 dark:text-dark-500">— {{ c.detail }}</span>
                </li>
              </ul>
            </div>
          </details>

          <!-- ② Raw attestation report (JSON) -->
          <details v-if="report" class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
            <summary class="cursor-pointer select-none px-4 py-3 text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('proof.auditors.rawReport') }}
              <span class="ml-1 font-mono text-xs font-normal text-gray-400 dark:text-dark-500">report.json</span>
            </summary>
            <div class="border-t border-gray-100 px-4 py-3 dark:border-dark-800">
              <button
                type="button"
                class="mb-3 inline-flex items-center rounded-lg border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 transition hover:bg-gray-50 dark:border-dark-600 dark:text-dark-200 dark:hover:bg-dark-800"
                @click="downloadReport"
              >{{ t('proof.live.download') }}</button>
              <pre class="max-h-96 overflow-auto rounded-lg bg-gray-950 p-4 text-xs leading-5 text-gray-100">{{ reportJson }}</pre>
            </div>
          </details>

          <!-- ③ Published reference values -->
          <details class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
            <summary class="cursor-pointer select-none px-4 py-3 text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('proof.reference.title') }}
              <span class="ml-1 text-xs font-normal text-gray-400 dark:text-dark-500">{{ t('proof.auditors.referenceHint') }}</span>
            </summary>
            <div class="border-t border-gray-100 px-4 py-3 dark:border-dark-800">
              <p class="mb-4 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('proof.reference.desc') }}</p>
              <div class="space-y-6">
                <div v-for="ref in references" :key="ref.title">
                  <h3 class="mb-2 text-sm font-semibold text-gray-800 dark:text-dark-100">{{ ref.title }}</h3>
                  <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
                    <table class="w-full border-collapse text-left text-xs">
                      <tbody>
                        <tr v-for="row in ref.rows" :key="row.label" class="border-b border-gray-100 last:border-0 dark:border-dark-800">
                          <th class="whitespace-nowrap bg-gray-50 px-3 py-2 font-medium text-gray-600 dark:bg-dark-800/60 dark:text-dark-300">{{ row.label }}</th>
                          <td class="break-all px-3 py-2 font-mono text-gray-800 dark:text-dark-100">
                            <a
                              v-if="row.href"
                              :href="row.href"
                              target="_blank"
                              rel="noopener noreferrer"
                              class="inline-flex items-center gap-1 text-primary-600 underline decoration-primary-300/40 underline-offset-4 transition hover:text-primary-700 hover:decoration-primary-500 dark:text-primary-300"
                            >
                              {{ row.value }}
                              <Icon name="externalLink" size="xs" class="flex-shrink-0 opacity-60" />
                            </a>
                            <span v-else>{{ row.value }}</span>
                          </td>
                        </tr>
                      </tbody>
                    </table>
                  </div>
                </div>
              </div>
              <p class="mt-4 text-xs leading-5 text-gray-500 dark:text-dark-400">
                {{ t('proof.reference.ciNote') }}
                <a
                  :href="ciWorkflowUrl"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="text-primary-600 underline decoration-primary-300/40 underline-offset-4 hover:text-primary-700 hover:decoration-primary-500 dark:text-primary-300"
                >publish-tee-images.yml</a>
              </p>
            </div>
          </details>
        </div>
      </section>

    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Header from '@/components/layout/Header.vue'
import { fetchAttestationReport, fetchMeridianQuote, type AttestationReport } from '@/api/attestation'
import { verifyAci, type VerifyAciResult, type Check } from '@/utils/attestation'
import {
  verifyQuoteBoundToReport,
  verifyMeridianQuote,
  PHALA_PCCS_URL,
  type VerifyQuoteBoundResult,
} from '@/utils/attestation/tdxVerify'
import {
  verifyE2eeChannel,
  runLiveE2eeRoundtrip,
  type E2eeChannelResult,
  type LiveRoundtripResult,
  type FailureCategory,
} from '@/utils/attestation/e2eeProof'
import { useAuthStore } from '@/stores/auth'
import {
  GATEWAY_REFERENCE,
  MERIDIAN_REFERENCE,
  E2EE_DEMO_MODEL,
  ATTESTATION_BASE_URL,
  ATTESTATION_REPORT_PATH,
} from '@/constants/attestation'

const { t, te } = useI18n()

// --- Verification state ---
const fetching = ref(false) // fetching the report + running ACI binding (2a)
const quoteVerifying = ref(false) // running the DCAP hardware quote check (2b)
const fetchError = ref(false)
const quoteError = ref(false)
const report = ref<AttestationReport | null>(null)
const nonce = ref('')
const aciResult = ref<VerifyAciResult | null>(null)
const quoteResult = ref<VerifyQuoteBoundResult | null>(null)
const meridianVerifying = ref(false) // running the Meridian enclave-B quote check (hop-3)
const meridianError = ref(false)
const meridianNonce = ref('')
const meridianResult = ref<VerifyQuoteBoundResult | null>(null)
// Path A — hop-1 browser-level E2EE proof. Verification-side (always-on) result
// plus the opt-in live round-trip state.
const e2eeResult = ref<E2eeChannelResult | null>(null)
const liveRunning = ref(false)
const liveResult = ref<LiveRoundtripResult | null>(null)
const apiKeyInput = ref('')
const modelInput = ref(E2EE_DEMO_MODEL)
const promptInput = ref('Reply with the exact text: E2EE-OK')

// --- Live-test failure presentation (category → colour / copy / next-step) ---
const authStore = useAuthStore()

/** The next-step affordance for a failure: a deep-link, a retry, or nothing. */
type FailAction =
  | { kind: 'route'; labelKey: string; to: string }
  | { kind: 'retry'; labelKey: string }
  | null

/**
 * Pure: map a failure category + the viewer's auth state to a next step. A
 * button only appears when there is a real destination — an unknown error just
 * shows the raw detail, never a dead link.
 */
function resolveAction(category: FailureCategory, isAuthed: boolean): FailAction {
  switch (category) {
    case 'quota':
      return isAuthed
        ? { kind: 'route', labelKey: 'proof.e2ee.live.action.topup', to: '/purchase' }
        : { kind: 'route', labelKey: 'proof.e2ee.live.action.registerTopup', to: '/login' }
    case 'auth':
      return isAuthed
        ? { kind: 'route', labelKey: 'proof.e2ee.live.action.manageKeys', to: '/keys' }
        : { kind: 'route', labelKey: 'proof.e2ee.live.action.login', to: '/login' }
    case 'rate_limit':
    case 'service':
    case 'network':
    case 'other':
      return { kind: 'retry', labelKey: 'proof.e2ee.live.action.retry' }
    case 'invalid_request':
    case 'trust':
      return null
  }
}

const failCategory = computed<FailureCategory>(() => liveResult.value?.failure?.category ?? 'other')
const failSeverity = computed(() => liveResult.value?.failure?.severity ?? 'operational')
const failTitleKey = computed(() => `proof.e2ee.live.failure.${failCategory.value}.title`)
const failBodyKey = computed(() => `proof.e2ee.live.failure.${failCategory.value}.body`)
// Honest reframe: only claim "encrypted & delivered" when the gateway actually
// replied (a status exists) — i.e. we sealed the prompt AND it reached them.
const showEncryptedPrefix = computed(() => !liveResult.value?.ok && liveResult.value?.failure?.status != null)
// Don't soften a genuine trust failure with the reassuring footnote.
const showUnaffected = computed(
  () => !!liveResult.value && !liveResult.value.ok && failSeverity.value !== 'trust',
)
const failAction = computed<FailAction>(() => resolveAction(failCategory.value, authStore.isAuthenticated))

const liveBoxClass = computed(() => {
  const r = liveResult.value
  if (!r) return ''
  if (r.ok)
    return 'border-green-200 bg-green-50 text-green-800 dark:border-green-500/30 dark:bg-green-500/10 dark:text-green-200'
  switch (failSeverity.value) {
    case 'trust':
      return 'border-red-200 bg-red-50 text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200'
    case 'inconclusive':
      return 'border-gray-200 bg-gray-50 text-gray-600 dark:border-dark-600 dark:bg-black/20 dark:text-dark-300'
    default:
      return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200'
  }
})
const reportJson = computed(() => (report.value ? JSON.stringify(report.value, null, 2) : ''))
// When the last full verification finished — shown as a "live" timestamp with a re-run.
const verifiedAt = ref<Date | null>(null)
const verifiedAtLabel = computed(() => {
  if (!verifiedAt.value) return ''
  const d = verifiedAt.value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`
})

const busy = computed(() => fetching.value || quoteVerifying.value || meridianVerifying.value)

/** Merged, namespaced checks from 2a (report./receipt.) and 2b (quote.). */
type DisplayCheck = Check & { gating?: boolean }
const allChecks = computed<DisplayCheck[]>(() => [
  ...(aciResult.value?.checks ?? []),
  ...(quoteResult.value?.checks ?? []).map((c) => ({ ...c, id: `quote.${c.id}` })),
  ...(meridianResult.value?.checks ?? []).map((c) => ({ ...c, id: `meridian.${c.id}` })),
  ...(e2eeResult.value?.checks ?? []).map((c) => ({ ...c, id: `e2ee.${c.id}` })),
])

function checkMark(c: DisplayCheck): { sym: string; class: string } {
  if (c.ok) return { sym: '✓', class: 'text-green-600 dark:text-green-400' }
  // Non-gating checks are informational (e.g. raw MRTD has no published reference).
  if (c.gating === false) return { sym: 'ℹ', class: 'text-gray-400 dark:text-dark-500' }
  return { sym: '✗', class: 'text-red-600 dark:text-red-400' }
}

// The gating checks that actually failed — surfaced by plain-English name in the banner.
const failingChecks = computed<DisplayCheck[]>(() =>
  allChecks.value.filter((c) => !c.ok && c.gating !== false),
)

// Summary line for the collapsed "Per-check results" fold, e.g. "30 passed · 2 informational".
const checksSummary = computed(() => {
  const checks = allChecks.value
  const passed = checks.filter((c) => c.ok).length
  const failed = checks.filter((c) => !c.ok && c.gating !== false).length
  const info = checks.filter((c) => !c.ok && c.gating === false).length
  const parts = [t('proof.auditors.checksPassed', { n: passed })]
  if (failed) parts.push(t('proof.auditors.checksFailed', { n: failed }))
  if (info) parts.push(t('proof.auditors.checksInfo', { n: info }))
  return parts.join(' · ')
})
/** Plain-English label for a check id, falling back to a de-jargoned form of the id. */
function friendlyCheckName(id: string): string {
  // vue-i18n splits keys on '.', so the map is keyed with '.'→'_' (e.g. report_freshness).
  const key = `proof.verify.checkLabel.${id.replace(/\./g, '_')}`
  if (te(key)) return t(key)
  return id.replace(/\./g, ' · ').replace(/_/g, ' ')
}

// --- Prompt journey hops (status driven by the real per-check results) ---
type HopStatus = 'pending' | 'checking' | 'pass' | 'fail' | 'disclosure'

// Which checks back each hop — an honest per-hop mapping.
// hop1: genuine TDX hardware running the exact measured compose (which terminates
// TLS in-CVM). hop2: that hardware attests to the ACI keyset, bound to your fresh
// nonce, and issues verifiable receipts.
const HOP1_CHECKS = [
  'quote.quote_genuine',
  'quote.tcb_status',
  'quote.measurement_rtmr3_replay',
  'quote.measurement_event_digest_binding',
  'quote.measurement_compose_hash_eventlog',
  'quote.measurement_compose_hash_mrconfigid',
  'quote.measurement_os_image_hash',
  'quote.measurement_app_id',
]
const HOP2_CHECKS = [
  'report.api_version',
  'report.workload_id',
  'report.workload_keyset_digest',
  'report.report_data',
  'report.keyset_endorsement_signature',
  'report.freshness',
  'quote.report_data_binding',
]
// hop3: Meridian enclave B — genuine TDX + nonce freshness + measured bridge code.
const HOP3_CHECKS = [
  'meridian.quote_genuine',
  'meridian.tcb_status',
  'meridian.nonce_binding',
  'meridian.measurement_rtmr3_replay',
  'meridian.measurement_app_id',
  'meridian.measurement_compose_hash_eventlog',
  'meridian.measurement_compose_hash_mrconfigid',
  'meridian.measurement_os_image_hash',
]

function hopStatus(ids: string[]): HopStatus {
  if (busy.value) return 'checking'
  if (!aciResult.value) return 'pending'
  const map = new Map(allChecks.value.map((c) => [c.id, c.ok]))
  const failing = ids.filter((id) => map.get(id) === false)
  if (failing.length) {
    // A device-clock offset (only freshness failed) is the visitor's clock, not the
    // TEE — don't alarm them with a red hop; the amber clock card below explains it.
    if (isClockSkewOnly.value && failing.length === 1 && failing[0] === 'report.freshness') return 'pending'
    return 'fail'
  }
  // "pass" requires EVERY backing check to be present and true — a missing check
  // (e.g. the quote step didn't run) stays 'pending', never green.
  if (ids.length > 0 && ids.every((id) => map.get(id) === true)) return 'pass'
  return 'pending'
}

interface Hop {
  key: string
  node: string
  claim: string
  basis: string
  confidential: boolean
  status: HopStatus
}

const hops = computed<Hop[]>(() => [
  {
    key: 'hop1',
    node: t('proof.journey.hop1.node'),
    claim: t('proof.journey.hop1.claim'),
    // Strengthen the stated basis once the browser-level E2EE channel is verified.
    basis: e2eeResult.value?.ok ? t('proof.journey.hop1.basisE2ee') : t('proof.journey.hop1.basis'),
    confidential: true,
    status: hopStatus(HOP1_CHECKS),
  },
  {
    key: 'hop2',
    node: t('proof.journey.hop2.node'),
    claim: t('proof.journey.hop2.claim'),
    basis: t('proof.journey.hop2.basis'),
    confidential: true,
    status: hopStatus(HOP2_CHECKS),
  },
  {
    key: 'hop3',
    node: t('proof.journey.hop3.node'),
    claim: t('proof.journey.hop3.claim'),
    basis: t('proof.journey.hop3.basis'),
    confidential: true,
    status: hopStatus(HOP3_CHECKS),
  },
  {
    // Anthropic sees plaintext by design — a disclosure, never a "verified".
    key: 'hop4',
    node: t('proof.journey.hop4.node'),
    claim: t('proof.journey.hop4.claim'),
    basis: t('proof.journey.hop4.basis'),
    confidential: false,
    status: 'disclosure',
  },
])

// Accordion open-state: hop1 open by default; a hop that fails verification always
// renders expanded (its failure must be visible), regardless of manual toggling.
const openHops = ref<Record<string, boolean>>({ hop1: true })
function toggleHop(key: string) {
  openHops.value[key] = !openHops.value[key]
}
function isHopOpen(hop: Hop): boolean {
  if (hop.status === 'fail') return true
  return !!openHops.value[hop.key]
}

function statusClass(status: HopStatus): string {
  switch (status) {
    case 'pass':
      return 'bg-green-100 text-green-700 dark:bg-green-500/15 dark:text-green-300'
    case 'fail':
      return 'bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-300'
    case 'checking':
      return 'bg-blue-100 text-blue-700 dark:bg-blue-500/15 dark:text-blue-300'
    default:
      return 'bg-gray-100 text-gray-500 dark:bg-dark-800 dark:text-dark-400'
  }
}

// --- Inline external links: URLs derived from the same reference constants so
// they never drift, letting a reader click straight through to each source. ---
const gatewayCommitUrl = `${(GATEWAY_REFERENCE.sourceRepo ?? '').replace(/\.git$/, '')}/commit/${GATEWAY_REFERENCE.sourceCommit ?? ''}`
const dockerHubUrl = (name: string) => `https://hub.docker.com/r/markerdao/${name}`
const shortSha = (digest: string): string => {
  const at = digest.indexOf('@')
  const h = at >= 0 ? digest.slice(at + 1) : digest
  return h ? `${h.slice(0, 15)}…` : ''
}
/** Show enough of a long wire-ciphertext hex to read as "gibberish" without flooding the page. */
function truncateHex(hex?: string): string {
  if (!hex) return ''
  const bytes = Math.floor(hex.length / 2)
  if (hex.length <= 160) return `${hex}  (${bytes} bytes)`
  return `${hex.slice(0, 128)} … ${hex.slice(-16)}   (${bytes} bytes total)`
}
const liveReportUrl = `${ATTESTATION_BASE_URL}${ATTESTATION_REPORT_PATH}`
const ciWorkflowUrl = 'https://github.com/FiiLabs/sub2api/blob/main/.github/workflows/publish-tee-images.yml'

// --- Reference values table (values link to their public source where one exists) ---
interface RefRow {
  label: string
  value: string
  href?: string
}
const references = computed<{ title: string; rows: RefRow[] }[]>(() => [
  {
    title: t('proof.reference.gateway'),
    rows: [
      { label: t('proof.reference.appId'), value: GATEWAY_REFERENCE.appId },
      { label: t('proof.reference.osImage'), value: `${GATEWAY_REFERENCE.osImage} — ${GATEWAY_REFERENCE.osImageHash}` },
      { label: t('proof.reference.composeHash'), value: GATEWAY_REFERENCE.composeHash },
      { label: t('proof.reference.sourceCommit'), value: GATEWAY_REFERENCE.sourceCommit ?? '—', href: gatewayCommitUrl },
      { label: t('proof.reference.signingAddress'), value: GATEWAY_REFERENCE.receiptSigningAddress ?? '—' },
      { label: t('proof.reference.launcherImage'), value: shortSha(GATEWAY_REFERENCE.images?.[0]?.digest ?? ''), href: dockerHubUrl('git-launcher-rust') },
    ],
  },
  {
    title: t('proof.reference.meridian'),
    rows: [
      { label: t('proof.reference.appId'), value: MERIDIAN_REFERENCE.appId },
      { label: t('proof.reference.osImage'), value: `${MERIDIAN_REFERENCE.osImage} — ${MERIDIAN_REFERENCE.osImageHash}` },
      { label: t('proof.reference.composeHash'), value: MERIDIAN_REFERENCE.composeHash },
      { label: t('proof.reference.enclaveImage'), value: shortSha(MERIDIAN_REFERENCE.images?.[0]?.digest ?? ''), href: dockerHubUrl('meridian-enclave') },
      { label: t('proof.reference.attestorImage'), value: 'sha256:7e6d51fe…', href: dockerHubUrl('meridian-attestor') },
    ],
  },
])

// --- Device-clock skew: the ONLY check that reads the local clock is freshness.
// When it's the sole failure and the report's own window is well-formed, the cause
// is the visitor's device clock, not the TEE — surface it as a friendly amber card
// that clears our security of blame, never a red "FAILED". ---
const clockSkew = computed(() => aciResult.value?.binding.clockSkew ?? null)
const isClockSkewOnly = computed(() => {
  if (busy.value || !clockSkew.value) return false
  const failing = allChecks.value.filter((c) => !c.ok && c.gating !== false)
  return failing.length === 1 && failing[0].id === 'report.freshness'
})
const skewDirection = computed(() =>
  clockSkew.value
    ? t(clockSkew.value.offsetSeconds >= 0 ? 'proof.verify.clockSkew.ahead' : 'proof.verify.clockSkew.behind')
    : '',
)
const skewAmount = computed(() => {
  if (!clockSkew.value) return ''
  const total = Math.abs(clockSkew.value.offsetSeconds)
  const m = Math.floor(total / 60)
  const s = total % 60
  const parts: string[] = []
  if (m > 0) parts.push(t('proof.verify.clockSkew.minutes', { n: m }))
  if (s > 0 || m === 0) parts.push(t('proof.verify.clockSkew.seconds', { n: s }))
  return parts.join(' ')
})

// --- Overall verification banner state ---
type OverallState = 'idle' | 'running' | 'pass' | 'fail' | 'aciOnly' | 'clockSkew'
const overallState = computed<OverallState>(() => {
  if (busy.value) return 'running'
  if (!aciResult.value) return 'idle'
  // Everything passed except a clock-skewed freshness → device-clock card, not a fail.
  if (isClockSkewOnly.value) return 'clockSkew'
  const aciOk = aciResult.value.binding.ok
  if (!quoteResult.value) return aciOk ? 'aciOnly' : 'fail'
  return quoteResult.value.ok && aciOk ? 'pass' : 'fail'
})
const bannerTitle = computed(() => t(`proof.verify.${overallState.value}.title`))
const bannerNote = computed(() => t(`proof.verify.${overallState.value}.note`))
const bannerClass = computed(() => {
  switch (overallState.value) {
    case 'pass':
      return 'border-green-200 bg-green-50 text-green-800 dark:border-green-500/30 dark:bg-green-500/10 dark:text-green-200'
    case 'fail':
      return 'border-red-200 bg-red-50 text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200'
    default:
      return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200'
  }
})

/**
 * Fetch a fresh nonce-bound report, then verify it end-to-end in the browser:
 * 2a (ACI identity binding), then 2b (Intel TDX hardware quote + measurements +
 * report_data binding). A hop turns green only when its gating checks pass.
 */
async function loadReport() {
  fetching.value = true
  fetchError.value = false
  quoteError.value = false
  meridianError.value = false
  aciResult.value = null
  quoteResult.value = null
  meridianResult.value = null
  e2eeResult.value = null
  liveResult.value = null
  let res
  try {
    res = await fetchAttestationReport()
  } catch {
    fetchError.value = true
    report.value = null
    fetching.value = false
    return
  }
  nonce.value = res.nonce
  report.value = res.report
  const aci = verifyAci(res.report, { nonce: res.nonce })
  aciResult.value = aci
  // Path A verification-side proof: the attested E2EE key is covered by the
  // (verified) workload_keyset_digest. Free, no data sent.
  e2eeResult.value = verifyE2eeChannel(res.report, aci.binding)
  fetching.value = false

  // 2b — hardware root of trust (genuine TDX + TCB + measurements + report_data
  // binding). Runs against Phala PCCS; a failure (e.g. PCCS unreachable) surfaces
  // honestly as quoteError rather than a fake pass.
  quoteVerifying.value = true
  try {
    quoteResult.value = await verifyQuoteBoundToReport(res.report, aci.reportDataDigestHex)
  } catch {
    quoteError.value = true
  } finally {
    quoteVerifying.value = false
  }

  // hop-3 — Meridian enclave B. Best-effort: its sidecar is a separate CVM
  // endpoint that may not be deployed yet, in which case this is unreachable and
  // hop-3 stays 'pending' (not failed).
  meridianVerifying.value = true
  try {
    const m = await fetchMeridianQuote()
    meridianNonce.value = m.nonce
    meridianResult.value = await verifyMeridianQuote(m.response, m.nonce)
  } catch {
    meridianError.value = true
  } finally {
    meridianVerifying.value = false
  }

  // Whole chain (2a + 2b + Meridian) done — stamp the "last verified" time.
  verifiedAt.value = new Date()
}

function downloadReport() {
  if (!report.value) return
  const blob = new Blob([JSON.stringify(report.value, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'attestation-report.json'
  a.click()
  URL.revokeObjectURL(url)
}

/**
 * Opt-in live E2EE round-trip: encrypt the prompt in-browser to the attested
 * key, POST it E2EE, decrypt the E2EE response. A failure surfaces honestly and
 * never disproves the verification-side channel above.
 */
async function runLive() {
  if (!report.value) return
  liveRunning.value = true
  liveResult.value = null
  try {
    liveResult.value = await runLiveE2eeRoundtrip(report.value, {
      apiKey: apiKeyInput.value,
      model: modelInput.value,
      prompt: promptInput.value,
    })
  } catch (e) {
    liveResult.value = { ok: false, checks: [], error: String(e) }
  } finally {
    liveRunning.value = false
  }
}

onMounted(() => {
  // Auto-run the (free, no-token) verification on load so a first-time visitor
  // immediately sees it verifying live (checking → verified) instead of a scary
  // idle "not verified" state they'd have to know to click a button to leave.
  loadReport()
})
</script>
