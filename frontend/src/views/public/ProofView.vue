<template>
  <div class="min-h-screen bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <header class="border-b border-gray-200 bg-white/95 dark:border-dark-800 dark:bg-dark-900/95">
      <div class="mx-auto flex max-w-5xl items-center justify-between gap-4 px-4 py-4 sm:px-6">
        <RouterLink to="/home" class="flex min-w-0 items-center gap-3">
          <span class="flex h-10 w-10 flex-shrink-0 items-center justify-center overflow-hidden rounded-xl bg-white shadow-sm ring-1 ring-gray-200 dark:bg-dark-800 dark:ring-dark-700">
            <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-full w-full object-contain" />
          </span>
          <span class="truncate text-base font-semibold text-gray-950 dark:text-white">{{ siteName }}</span>
        </RouterLink>
        <div class="flex flex-shrink-0 items-center gap-3">
          <LocaleSwitcher />
          <RouterLink to="/home" class="text-sm font-medium text-gray-500 hover:text-gray-800 dark:text-dark-300 dark:hover:text-white">
            {{ t('proof.header.backHome') }}
          </RouterLink>
        </div>
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

      <!-- Prompt journey (4 hops) -->
      <section class="mb-10">
        <h2 class="mb-4 text-lg font-semibold text-gray-900 dark:text-white">{{ t('proof.journey.title') }}</h2>
        <ol class="relative space-y-4 border-l-2 border-dashed border-gray-200 pl-6 dark:border-dark-700">
          <li v-for="(hop, i) in hops" :key="hop.key" class="relative">
            <span
              class="absolute -left-[31px] flex h-5 w-5 items-center justify-center rounded-full text-[10px] font-bold ring-4 ring-gray-50 dark:ring-dark-950"
              :class="hop.confidential
                ? 'bg-primary-600 text-white'
                : 'bg-amber-500 text-white'"
            >{{ i + 1 }}</span>
            <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <p class="font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ hop.node }}</p>
                <span
                  class="inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-medium"
                  :class="statusClass(hop.status)"
                >{{ t('proof.status.' + hop.status) }}</span>
              </div>
              <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ hop.claim }}</p>
              <p class="mt-2 text-xs text-gray-400 dark:text-dark-500">{{ t('proof.journey.basisLabel') }}: {{ hop.basis }}</p>
            </div>
          </li>
        </ol>
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
            {{ busy ? t('proof.live.fetching') : t('proof.live.fetch') }}
          </button>
          <button
            v-if="report"
            type="button"
            class="inline-flex items-center rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 transition hover:bg-gray-50 dark:border-dark-600 dark:text-dark-200 dark:hover:bg-dark-800"
            @click="downloadReport"
          >{{ t('proof.live.download') }}</button>
          <span v-if="nonce" class="font-mono text-xs text-gray-400 dark:text-dark-500">{{ t('proof.live.nonce') }}: {{ nonce }}</span>
        </div>

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
          <p class="mt-1 leading-6">{{ bannerNote }}</p>
          <template v-if="allChecks.length">
            <p class="mt-3 text-xs font-semibold uppercase tracking-wide opacity-70">{{ t('proof.verify.checksTitle') }}</p>
            <ul class="mt-1 space-y-1">
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
          </template>
        </div>

        <pre v-if="report" class="mt-4 max-h-96 overflow-auto rounded-lg bg-gray-950 p-4 text-xs leading-5 text-gray-100">{{ reportJson }}</pre>
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

          <!-- Result: honest green on authenticated decrypt, honest red otherwise -->
          <div
            v-if="liveResult"
            class="mt-4 rounded-lg border px-4 py-3 text-sm"
            :class="liveResult.ok
              ? 'border-green-200 bg-green-50 text-green-800 dark:border-green-500/30 dark:bg-green-500/10 dark:text-green-200'
              : 'border-red-200 bg-red-50 text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200'"
          >
            <p class="font-semibold">{{ liveResult.ok ? t('proof.e2ee.live.successTitle') : t('proof.e2ee.live.failTitle') }}</p>
            <p class="mt-1 leading-6">{{ liveResult.ok ? t('proof.e2ee.live.successNote') : t('proof.e2ee.live.failNote') }}</p>
            <template v-if="liveResult.ok && liveResult.replyText">
              <p class="mt-3 text-xs font-semibold uppercase tracking-wide opacity-70">{{ t('proof.e2ee.live.replyLabel') }}</p>
              <pre class="mt-1 overflow-auto whitespace-pre-wrap rounded bg-white/60 p-2 font-mono text-xs text-gray-800 dark:bg-black/20 dark:text-gray-100">{{ liveResult.replyText }}</pre>
            </template>
            <p v-if="!liveResult.ok && liveResult.error" class="mt-2 break-all font-mono text-xs opacity-80">{{ liveResult.error }}</p>
            <ul v-if="liveResult.checks.length" class="mt-3 space-y-1">
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
          </div>
        </div>
      </section>

      <!-- Published reference values -->
      <section class="mb-10">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('proof.reference.title') }}</h2>
        <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('proof.reference.desc') }}</p>
        <div class="mt-4 space-y-6">
          <div v-for="ref in references" :key="ref.title">
            <h3 class="mb-2 text-sm font-semibold text-gray-800 dark:text-dark-100">{{ ref.title }}</h3>
            <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
              <table class="w-full border-collapse text-left text-xs">
                <tbody>
                  <tr v-for="row in ref.rows" :key="row.label" class="border-b border-gray-100 last:border-0 dark:border-dark-800">
                    <th class="whitespace-nowrap bg-gray-50 px-3 py-2 font-medium text-gray-600 dark:bg-dark-800/60 dark:text-dark-300">{{ row.label }}</th>
                    <td class="break-all px-3 py-2 font-mono text-gray-800 dark:text-dark-100">{{ row.value }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
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
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import { getPublicSettings } from '@/api/auth'
import { sanitizeUrl } from '@/utils/url'
import { fetchAttestationReport, fetchMeridianQuote, type AttestationReport } from '@/api/attestation'
import { verifyAci, type VerifyAciResult, type Check } from '@/utils/attestation'
import {
  verifyQuoteBoundToReport,
  verifyMeridianQuote,
  type VerifyQuoteBoundResult,
} from '@/utils/attestation/tdxVerify'
import {
  verifyE2eeChannel,
  runLiveE2eeRoundtrip,
  type E2eeChannelResult,
  type LiveRoundtripResult,
} from '@/utils/attestation/e2eeProof'
import { GATEWAY_REFERENCE, MERIDIAN_REFERENCE, E2EE_DEMO_MODEL } from '@/constants/attestation'

const { t } = useI18n()

// --- Site chrome (logo / name), same pattern as LegalDocumentView ---
const siteName = ref('ApexOne')
const siteLogo = ref('')

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
const reportJson = computed(() => (report.value ? JSON.stringify(report.value, null, 2) : ''))

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
  if (ids.some((id) => map.get(id) === false)) return 'fail'
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

// --- Reference values table ---
const references = computed(() => [
  {
    title: t('proof.reference.gateway'),
    rows: [
      { label: t('proof.reference.appId'), value: GATEWAY_REFERENCE.appId },
      { label: t('proof.reference.osImage'), value: `${GATEWAY_REFERENCE.osImage} — ${GATEWAY_REFERENCE.osImageHash}` },
      { label: t('proof.reference.composeHash'), value: GATEWAY_REFERENCE.composeHash },
      { label: t('proof.reference.sourceCommit'), value: GATEWAY_REFERENCE.sourceCommit ?? '—' },
      { label: t('proof.reference.signingAddress'), value: GATEWAY_REFERENCE.receiptSigningAddress ?? '—' },
    ],
  },
  {
    title: t('proof.reference.meridian'),
    rows: [
      { label: t('proof.reference.appId'), value: MERIDIAN_REFERENCE.appId },
      { label: t('proof.reference.osImage'), value: `${MERIDIAN_REFERENCE.osImage} — ${MERIDIAN_REFERENCE.osImageHash}` },
      { label: t('proof.reference.composeHash'), value: MERIDIAN_REFERENCE.composeHash },
    ],
  },
])

// --- Overall verification banner state ---
type OverallState = 'idle' | 'running' | 'pass' | 'fail' | 'aciOnly'
const overallState = computed<OverallState>(() => {
  if (busy.value) return 'running'
  if (!aciResult.value) return 'idle'
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

onMounted(async () => {
  try {
    const settings = await getPublicSettings()
    siteName.value = settings?.site_name || 'ApexOne'
    siteLogo.value = sanitizeUrl(settings?.site_logo || '', { allowRelative: true, allowDataUrl: true })
  } catch {
    // Non-fatal: fall back to defaults.
  }
})
</script>
