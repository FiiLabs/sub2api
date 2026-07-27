<template>
  <div class="min-h-screen bg-gray-50 pt-16 text-gray-900 dark:bg-dark-950 dark:text-white">
    <!-- Slim public header (the marketing Header lives in the separately deployed site) -->
    <header class="fixed inset-x-0 top-0 z-40 border-b border-gray-200 bg-white/95 backdrop-blur dark:border-dark-800 dark:bg-dark-900/95">
      <div class="mx-auto flex h-16 max-w-4xl items-center justify-between gap-4 px-4 sm:px-6">
        <span class="flex min-w-0 items-center gap-2.5">
          <span class="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-md bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300">
            <Icon name="shield" size="sm" />
          </span>
          <span class="truncate text-base font-semibold text-gray-950 dark:text-white">/proof</span>
        </span>
        <RouterLink
          to="/home"
          class="inline-flex flex-shrink-0 items-center rounded-lg border border-gray-300 px-3.5 py-1.5 text-sm font-medium text-gray-700 transition hover:bg-gray-100 dark:border-dark-600 dark:text-dark-200 dark:hover:bg-dark-800"
        >{{ t('proof.header.backHome') }}</RouterLink>
      </div>
    </header>

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

      <!-- Request journey -->
      <section class="mb-10">
        <h2 class="mb-4 text-lg font-semibold text-gray-900 dark:text-white">{{ t('proof.journey.title') }}</h2>

        <!-- Beginner-friendly flow diagram: where your request goes + who sees plaintext -->
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

            <!-- TEE boundary wrapping service + attestor -->
            <div class="flex flex-[2.4] flex-col rounded-xl border-2 border-dashed border-primary-400/60 bg-primary-50/40 p-3 dark:border-primary-500/40 dark:bg-primary-500/5">
              <span class="mb-2 inline-flex items-center justify-center gap-1 text-center text-[11px] font-semibold text-primary-700 dark:text-primary-300">
                🔒 {{ t('proof.flow.teeLabel') }}
              </span>
              <div class="flex items-stretch gap-2">
                <div class="flex flex-1 flex-col items-center gap-1 rounded-lg bg-white p-2.5 text-center shadow-sm dark:bg-dark-800">
                  <span class="text-xl">🌐</span>
                  <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('proof.flow.service') }}</span>
                  <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('proof.flow.serviceSub') }}</span>
                </div>
                <div class="flex flex-col items-center justify-center">
                  <span class="whitespace-nowrap text-[10px] font-medium text-primary-600 dark:text-primary-400">{{ t('proof.flow.arrInternal') }}</span>
                  <span class="text-lg leading-none text-primary-300 dark:text-primary-500">·</span>
                </div>
                <div class="flex flex-1 flex-col items-center gap-1 rounded-lg bg-white p-2.5 text-center shadow-sm dark:bg-dark-800">
                  <span class="text-xl">🛡️</span>
                  <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('proof.flow.attestor') }}</span>
                  <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('proof.flow.attestorSub') }}</span>
                </div>
              </div>
            </div>

            <!-- plaintext arrow -->
            <div class="flex flex-col items-center justify-center px-1">
              <span class="whitespace-nowrap text-[10px] font-medium text-amber-600 dark:text-amber-400">⚠ {{ t('proof.flow.arrPlaintext') }}</span>
              <span class="text-2xl leading-none text-gray-300 dark:text-dark-600">→</span>
            </div>

            <!-- 3. Upstream providers -->
            <div class="flex flex-1 flex-col items-center gap-1.5 rounded-lg border border-amber-200 bg-amber-50 p-3 text-center dark:border-amber-500/30 dark:bg-amber-500/10">
              <span class="text-2xl">🤖</span>
              <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('proof.flow.upstream') }}</span>
              <span class="text-xs text-amber-700 dark:text-amber-300">{{ t('proof.flow.upstreamSub') }}</span>
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

        <!-- Accordion: the status badge stays visible on the collapsed header row. -->
        <div class="space-y-3">
          <div
            v-for="(hop, i) in hops"
            :key="hop.key"
            class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900"
          >
            <button
              type="button"
              class="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-gray-50 dark:hover:bg-dark-800/60"
              :aria-expanded="isHopOpen(hop.key)"
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
                :class="{ 'rotate-90': isHopOpen(hop.key) }"
                fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"
              >
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
              </svg>
            </button>
            <div
              v-show="isHopOpen(hop.key)"
              class="border-t border-gray-100 px-4 py-3 pl-[52px] dark:border-dark-800"
            >
              <p class="text-sm leading-6 text-gray-600 dark:text-dark-300">{{ hop.claim }}</p>
              <p class="mt-2 text-xs text-gray-400 dark:text-dark-500">{{ t('proof.journey.basisLabel') }}: {{ hop.basis }}</p>
            </div>
          </div>
        </div>
      </section>

      <!-- Honest disclosure -->
      <section class="mb-10 rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('proof.disclosure.title') }}</h2>
        <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('proof.disclosure.body') }}</p>
      </section>

      <!-- Live attestation -->
      <section class="mb-10">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('proof.live.title') }}</h2>
        <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('proof.live.desc') }}</p>

        <div class="mt-4 flex flex-wrap items-center gap-3">
          <button
            type="button"
            :disabled="busy"
            class="inline-flex items-center gap-2 rounded-lg bg-primary-600 px-4 py-2 text-sm font-semibold text-white shadow-sm transition hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-60"
            @click="run"
          >
            <span v-if="busy" class="h-4 w-4 animate-spin rounded-full border-b-2 border-white"></span>
            {{ busy ? t('proof.live.fetching') : (result ? t('proof.live.refetch') : t('proof.live.fetch')) }}
          </button>
          <button
            v-if="result"
            type="button"
            class="inline-flex items-center rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 transition hover:bg-gray-50 dark:border-dark-600 dark:text-dark-200 dark:hover:bg-dark-800"
            @click="downloadEvidence"
          >{{ t('proof.live.download') }}</button>
          <span v-if="nonceHex" class="font-mono text-xs text-gray-400 dark:text-dark-500">{{ t('proof.live.nonce') }}: {{ nonceHex }}</span>
        </div>

        <p class="mt-3 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('proof.live.autoNote') }}</p>

        <!-- Where the data + verification collateral come from — all click-through. -->
        <dl class="mt-3 space-y-1 text-xs text-gray-500 dark:text-dark-400">
          <div class="flex flex-wrap gap-x-2">
            <dt class="font-medium">{{ t('proof.live.endpointLabel') }}:</dt>
            <dd class="min-w-0">
              <a :href="attestorUrl" target="_blank" rel="noopener noreferrer" class="break-all font-mono text-primary-600 underline decoration-primary-300/40 underline-offset-4 hover:text-primary-700 hover:decoration-primary-500 dark:text-primary-300">{{ attestorUrl }}</a>
            </dd>
          </div>
          <div class="flex flex-wrap gap-x-2">
            <dt class="font-medium">{{ t('proof.live.collateralLabel') }}:</dt>
            <dd class="min-w-0">
              <a :href="PHALA_PCCS_URL" target="_blank" rel="noopener noreferrer" class="break-all font-mono text-primary-600 underline decoration-primary-300/40 underline-offset-4 hover:text-primary-700 hover:decoration-primary-500 dark:text-primary-300">{{ PHALA_PCCS_URL }}</a>
            </dd>
          </div>
          <div class="flex flex-wrap gap-x-2">
            <dt class="font-medium">{{ t('proof.live.referenceLabel') }}:</dt>
            <dd class="min-w-0">
              <a :href="REFERENCE_JSON_URL" target="_blank" rel="noopener noreferrer" class="break-all font-mono text-primary-600 underline decoration-primary-300/40 underline-offset-4 hover:text-primary-700 hover:decoration-primary-500 dark:text-primary-300">reference.json</a>
            </dd>
          </div>
          <div class="flex flex-wrap gap-x-2">
            <dt class="font-medium">{{ t('proof.live.explorerLabel') }}:</dt>
            <dd class="min-w-0">
              <a href="https://proof.t16z.com/" target="_blank" rel="noopener noreferrer" class="break-all font-mono text-primary-600 underline decoration-primary-300/40 underline-offset-4 hover:text-primary-700 hover:decoration-primary-500 dark:text-primary-300">proof.t16z.com</a>
            </dd>
          </div>
        </dl>

        <p v-if="loaded" class="mt-2 text-xs text-gray-400 dark:text-dark-500">
          {{ loaded.source === 'repo' ? t('proof.live.referenceSource.repo') : t('proof.live.referenceSource.bakedIn') }}
        </p>

        <!-- Verification banner -->
        <div
          v-if="overallState !== 'idle'"
          class="mt-4 rounded-lg border px-4 py-3 text-sm"
          :class="bannerClass"
        >
          <p class="flex items-center gap-2 font-semibold">
            <span v-if="busy" class="h-3.5 w-3.5 flex-shrink-0 animate-spin rounded-full border-b-2 border-current"></span>
            {{ bannerTitle }}
          </p>
          <!-- On a real failure, name the failing check(s) in plain English up top. -->
          <ul v-if="overallState === 'fail' && failingChecks.length" class="mt-2 space-y-1 font-medium">
            <li v-for="c in failingChecks" :key="c.id" class="flex items-baseline gap-2">
              <span aria-hidden="true">✗</span>
              <span>{{ friendlyCheckName(c.id) }}</span>
            </li>
          </ul>
          <p class="mt-1 leading-6">{{ bannerNote }}</p>
          <p v-if="overallState === 'error' && errorMsg" class="mt-1 break-all font-mono text-xs opacity-70">{{ errorMsg }}</p>
          <div v-if="verifiedAt && !busy" class="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1 border-t border-current/10 pt-2 text-xs opacity-70">
            <span>{{ t('proof.verify.lastVerified', { time: verifiedAtLabel }) }}</span>
            <span aria-hidden="true">·</span>
            <button type="button" class="font-semibold underline underline-offset-2 hover:opacity-100" @click="run">{{ t('proof.verify.rerun') }}</button>
          </div>
        </div>
      </section>

      <!-- For auditors -->
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

          <!-- ② Raw attestation payload (JSON) -->
          <details v-if="rawResponse" class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
            <summary class="cursor-pointer select-none px-4 py-3 text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('proof.auditors.rawReport') }}
              <span class="ml-1 font-mono text-xs font-normal text-gray-400 dark:text-dark-500">evidence.json</span>
            </summary>
            <div class="border-t border-gray-100 px-4 py-3 dark:border-dark-800">
              <button
                type="button"
                class="mb-3 inline-flex items-center rounded-lg border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 transition hover:bg-gray-50 dark:border-dark-600 dark:text-dark-200 dark:hover:bg-dark-800"
                @click="downloadEvidence"
              >{{ t('proof.live.download') }}</button>
              <pre class="max-h-96 overflow-auto rounded-lg bg-gray-950 p-4 text-xs leading-5 text-gray-100">{{ rawResponseJson }}</pre>
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
              <h3 class="mb-2 text-sm font-semibold text-gray-800 dark:text-dark-100">{{ t('proof.reference.enclave') }}</h3>
              <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
                <table class="w-full border-collapse text-left text-xs">
                  <tbody>
                    <tr v-for="row in referenceRows" :key="row.label" class="border-b border-gray-100 last:border-0 dark:border-dark-800">
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

      <!-- Open source — the closing call to action, data-driven from the reference. -->
      <section class="mb-10">
        <div class="rounded-xl border border-primary-200 bg-primary-50/50 p-5 dark:border-primary-500/30 dark:bg-primary-500/5 sm:p-6">
          <div class="flex items-start gap-3">
            <span class="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-md bg-primary-100 text-primary-700 dark:bg-primary-500/15 dark:text-primary-300">
              <Icon name="terminal" size="sm" />
            </span>
            <div class="min-w-0">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('proof.openSource.title') }}</h2>
              <p class="mt-1.5 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('proof.openSource.body') }}</p>
            </div>
          </div>

          <div class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <a
              v-for="card in sourceCards"
              :key="card.key"
              :href="card.href"
              target="_blank"
              rel="noopener noreferrer"
              class="group flex flex-col rounded-lg border border-gray-200 bg-white p-4 transition hover:border-primary-400 hover:shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:hover:border-primary-500/50"
            >
              <span class="flex items-center justify-between gap-2">
                <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ card.label }}</span>
                <Icon name="externalLink" size="xs" class="flex-shrink-0 text-gray-400 transition group-hover:text-primary-600 dark:group-hover:text-primary-300" />
              </span>
              <span class="mt-1 flex-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ card.sub }}</span>
              <span v-if="card.commit" class="mt-2 inline-block w-fit rounded bg-gray-100 px-1.5 py-0.5 font-mono text-[11px] text-gray-500 dark:bg-dark-800 dark:text-dark-400">{{ t('proof.openSource.pinnedAt', { commit: card.commit }) }}</span>
              <span class="mt-2 text-xs font-medium text-primary-600 dark:text-primary-300">{{ t('proof.openSource.viewSource') }}</span>
            </a>
          </div>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import {
  ATTESTATION_QUOTE_PATH,
  ATTESTOR_BASE_URL,
  REFERENCE_JSON_URL,
  type EnclaveReference,
} from '@/constants/attestation'
import { loadReference, type LoadedReference } from '@/utils/attestation/reference'
import {
  PHALA_PCCS_URL,
  verifyEnclaveQuote,
  type Check,
  type EnclaveQuoteInput,
  type VerifyEnclaveQuoteResult,
} from '@/utils/attestation/tdxVerify'

const { t, te } = useI18n()

type DisplayCheck = Check & { gating?: boolean }

// --- verification state ---
type Phase = 'idle' | 'running' | 'done' | 'error'
const phase = ref<Phase>('idle')
const errorMsg = ref('')
const loaded = ref<LoadedReference | null>(null)
const result = ref<VerifyEnclaveQuoteResult | null>(null)
const nonceHex = ref('')
const rawResponse = ref<EnclaveQuoteInput | null>(null)
const verifiedAt = ref<Date | null>(null)

const busy = computed(() => phase.value === 'running')
const reference = computed<EnclaveReference | undefined>(() => loaded.value?.reference)
// Runtime-published attestor URL (reference.json) wins; baked-in is fallback.
const attestorUrl = computed(
  () => `${loaded.value?.attestorBaseUrl ?? ATTESTOR_BASE_URL}${ATTESTATION_QUOTE_PATH}`,
)

function randomNonceHex(): string {
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

async function run(): Promise<void> {
  phase.value = 'running'
  errorMsg.value = ''
  result.value = null
  rawResponse.value = null
  try {
    loaded.value = await loadReference()
    nonceHex.value = randomNonceHex()
    const res = await fetch(`${attestorUrl.value}?nonce=${nonceHex.value}`, { cache: 'no-store' })
    if (!res.ok) throw new Error(`${t('proof.live.error')} (HTTP ${res.status})`)
    rawResponse.value = (await res.json()) as EnclaveQuoteInput
    result.value = await verifyEnclaveQuote(rawResponse.value, nonceHex.value, {
      reference: loaded.value.reference,
    })
    verifiedAt.value = new Date()
    phase.value = 'done'
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e)
    phase.value = 'error'
  }
}

// --- checks ---
const allChecks = computed<DisplayCheck[]>(() => result.value?.checks ?? [])
const failingChecks = computed<DisplayCheck[]>(() =>
  allChecks.value.filter((c) => !c.ok && c.gating !== false),
)

function checkMark(c: DisplayCheck): { sym: string; class: string } {
  if (c.ok) return { sym: '✓', class: 'text-green-600 dark:text-green-400' }
  // Non-gating checks are informational (e.g. raw MRTD has no published reference).
  if (c.gating === false) return { sym: 'ℹ', class: 'text-gray-400 dark:text-dark-500' }
  return { sym: '✗', class: 'text-red-600 dark:text-red-400' }
}

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

function friendlyCheckName(id: string): string {
  const key = `proof.verify.checkLabel.${id.replace(/\./g, '_')}`
  if (te(key)) return t(key)
  return id.replace(/\./g, ' · ').replace(/_/g, ' ')
}

function checkOk(id: string): boolean | undefined {
  return allChecks.value.find((c) => c.id === id)?.ok
}

// --- journey hops (status driven by the real per-check results) ---
type HopStatus = 'pending' | 'checking' | 'pass' | 'fail' | 'disclosure'

// hop1: genuine hardware running exactly the published code (fingerprints).
// hop2: the proof is fresh (nonce) and the measurement log is authentic.
const HOP1_CHECKS = [
  'quote_genuine',
  'tcb_status',
  'measurement_app_id',
  'measurement_compose_hash_eventlog',
  'measurement_compose_hash_mrconfigid',
  'measurement_os_image_hash',
]
const HOP2_CHECKS = ['nonce_binding', 'measurement_rtmr3_replay']

function hopStatus(checkIds: string[]): HopStatus {
  if (phase.value === 'running') return 'checking'
  if (phase.value !== 'done') return 'pending'
  return checkIds.every((id) => checkOk(id) === true) ? 'pass' : 'fail'
}

const hops = computed(() => [
  {
    key: 'hop1',
    node: t('proof.journey.hop1.node'),
    claim: t('proof.journey.hop1.claim'),
    basis: t('proof.journey.hop1.basis'),
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
    confidential: false,
    status: 'disclosure' as HopStatus,
  },
])

const openHops = ref<Set<string>>(new Set())
function isHopOpen(key: string): boolean {
  return openHops.value.has(key)
}
function toggleHop(key: string): void {
  const next = new Set(openHops.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  openHops.value = next
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

// --- banner ---
const overallState = computed<'idle' | 'running' | 'pass' | 'fail' | 'error'>(() => {
  if (phase.value === 'idle') return 'idle'
  if (phase.value === 'running') return 'running'
  if (phase.value === 'error') return 'error'
  return result.value?.ok ? 'pass' : 'fail'
})

const bannerTitle = computed(() => {
  switch (overallState.value) {
    case 'running':
      return t('proof.verify.running.title')
    case 'pass':
      return t('proof.verify.pass.title')
    case 'fail':
      return t('proof.verify.fail.title')
    default:
      return t('proof.verify.error.title')
  }
})
const bannerNote = computed(() => {
  switch (overallState.value) {
    case 'running':
      return t('proof.verify.running.note')
    case 'pass':
      return t('proof.verify.pass.note')
    case 'fail':
      return t('proof.verify.fail.note')
    default:
      return t('proof.verify.error.note')
  }
})
const bannerClass = computed(() => {
  switch (overallState.value) {
    case 'pass':
      return 'border-green-200 bg-green-50 text-green-800 dark:border-green-500/30 dark:bg-green-500/10 dark:text-green-200'
    case 'fail':
      return 'border-red-200 bg-red-50 text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200'
    case 'error':
      return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200'
    default:
      return 'border-gray-200 bg-white text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200'
  }
})

const verifiedAtLabel = computed(() =>
  verifiedAt.value ? verifiedAt.value.toISOString().slice(0, 19).replace('T', ' ') : '',
)

// --- reference table + open-source cards (data-driven, links never drift) ---
const repoWebUrl = computed(() =>
  (reference.value?.sourceRepo ?? 'https://github.com/FiiLabs/sub2api.git').replace(/\.git$/, ''),
)
const commitShort = computed(() => (reference.value?.sourceCommit ?? '').slice(0, 9))
const ciWorkflowUrl = computed(
  () => `${repoWebUrl.value}/blob/${reference.value?.sourceCommit ?? 'main'}/.github/workflows/publish-tee-images.yml`,
)

const referenceRows = computed(() => {
  const r = reference.value
  if (!r) return []
  const imageDigest = (name: string) => r.images?.find((i) => i.name === name)?.digest ?? ''
  return [
    { label: t('proof.reference.appId'), value: r.appId },
    { label: t('proof.reference.osImage'), value: `${r.osImage} (${r.osImageHash})` },
    { label: t('proof.reference.composeHash'), value: r.composeHash },
    {
      label: t('proof.reference.sourceCommit'),
      value: r.sourceCommit ?? '',
      href: r.sourceCommit ? `${repoWebUrl.value}/commit/${r.sourceCommit}` : undefined,
    },
    { label: t('proof.reference.serviceImage'), value: imageDigest('sub2api') },
    { label: t('proof.reference.attestorImage'), value: imageDigest('sub2api-attestor') },
  ].filter((row) => row.value)
})

const sourceCards = computed(() => {
  const commit = reference.value?.sourceCommit ?? 'main'
  return [
    {
      key: 'service',
      label: t('proof.openSource.serviceLabel'),
      sub: t('proof.openSource.serviceSub'),
      href: `${repoWebUrl.value}/tree/${commit}`,
      commit: commitShort.value,
    },
    {
      key: 'deploy',
      label: t('proof.openSource.deployLabel'),
      sub: t('proof.openSource.deploySub'),
      href: `${repoWebUrl.value}/tree/${commit}/deploy/phala`,
      commit: commitShort.value,
    },
    {
      key: 'ci',
      label: t('proof.openSource.ciLabel'),
      sub: t('proof.openSource.ciSub'),
      href: ciWorkflowUrl.value,
      commit: '',
    },
  ]
})

// --- evidence download ---
const rawResponseJson = computed(() =>
  rawResponse.value ? JSON.stringify(rawResponse.value, null, 2) : '',
)

function downloadEvidence(): void {
  const blob = new Blob(
    [
      JSON.stringify(
        {
          fetchedAt: new Date().toISOString(),
          attestorUrl: attestorUrl.value,
          nonce: nonceHex.value,
          referenceSource: loaded.value?.source,
          reference: loaded.value?.reference,
          response: rawResponse.value,
          checks: result.value?.checks,
          measurements: result.value?.measurements,
          ok: result.value?.ok,
        },
        null,
        2,
      ),
    ],
    { type: 'application/json' },
  )
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `proof-evidence-${Date.now()}.json`
  a.click()
  URL.revokeObjectURL(url)
}

onMounted(run)
</script>
