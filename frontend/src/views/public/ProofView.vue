<template>
  <div class="min-h-screen bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <header class="border-b border-gray-200 bg-white/95 dark:border-dark-800 dark:bg-dark-900/95">
      <div class="mx-auto flex max-w-5xl items-center justify-between gap-4 px-4 py-4 sm:px-6">
        <RouterLink to="/home" class="text-base font-semibold text-gray-950 dark:text-white">
          {{ t('proof.title') }}
        </RouterLink>
        <div class="flex items-center gap-2">
          <button
            class="inline-flex items-center rounded-lg border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 transition hover:bg-gray-100 disabled:opacity-50 dark:border-dark-600 dark:text-dark-200 dark:hover:bg-dark-800"
            :disabled="phase === 'running'"
            @click="run"
          >
            {{ t('proof.actions.rerun') }}
          </button>
          <button
            class="inline-flex items-center rounded-lg border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 transition hover:bg-gray-100 disabled:opacity-50 dark:border-dark-600 dark:text-dark-200 dark:hover:bg-dark-800"
            :disabled="!result"
            @click="downloadEvidence"
          >
            {{ t('proof.actions.download') }}
          </button>
        </div>
      </div>
    </header>

    <main class="mx-auto max-w-5xl px-4 py-8 sm:px-6 lg:py-10">
      <p class="max-w-3xl text-sm leading-6 text-gray-600 dark:text-dark-300">
        {{ t('proof.subtitle') }}
      </p>

      <!-- Overall status banner -->
      <div class="mt-6 rounded-lg border p-4 text-sm font-medium" :class="bannerClass" role="status">
        <span v-if="phase === 'running'">{{ t('proof.overall.running') }}</span>
        <span v-else-if="phase === 'done' && result?.ok">{{ t('proof.overall.pass') }}</span>
        <span v-else-if="phase === 'done'">{{ t('proof.overall.fail') }}</span>
        <span v-else-if="phase === 'error'">
          {{ t('proof.overall.error') }}
          <span class="font-normal opacity-80">{{ errorMsg }}</span>
        </span>
      </div>

      <!-- Reference source note -->
      <p v-if="loaded" class="mt-2 text-xs text-gray-500 dark:text-dark-400">
        {{ t('proof.reference.source') }}:
        {{ loaded.source === 'repo' ? t('proof.reference.repo') : t('proof.reference.bakedIn') }}
      </p>

      <!-- Verification cards -->
      <div class="mt-6 grid gap-4 lg:grid-cols-2">
        <!-- 1. Hardware -->
        <section class="rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
          <h2 class="flex items-center gap-2 text-base font-semibold">
            <StatusMark :ok="hardwareOk" />
            {{ t('proof.hardware.title') }}
          </h2>
          <p class="mt-2 text-sm text-gray-600 dark:text-dark-300">{{ t('proof.hardware.desc') }}</p>
          <ul class="mt-3 space-y-1.5 text-sm">
            <li class="flex items-start gap-2">
              <StatusMark :ok="checkOk('quote_genuine')" />
              <span>{{ t('proof.hardware.quoteGenuine') }}</span>
            </li>
            <li class="flex items-start gap-2">
              <StatusMark :ok="checkOk('tcb_status')" />
              <span>
                {{ t('proof.hardware.tcbStatus') }}
                <code v-if="result?.measurements.tcbStatus" class="ml-1 rounded bg-gray-100 px-1 text-xs dark:bg-dark-800">{{ result.measurements.tcbStatus }}</code>
              </span>
            </li>
            <li class="flex items-start gap-2">
              <StatusMark :ok="checkOk('nonce_binding')" />
              <span>{{ t('proof.hardware.nonceBinding') }}</span>
            </li>
          </ul>
        </section>

        <!-- 2. Measurements -->
        <section class="rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
          <h2 class="flex items-center gap-2 text-base font-semibold">
            <StatusMark :ok="measureOk" />
            {{ t('proof.measure.title') }}
          </h2>
          <p class="mt-2 text-sm text-gray-600 dark:text-dark-300">{{ t('proof.measure.desc') }}</p>
          <ul class="mt-3 space-y-1.5 text-sm">
            <li class="flex items-start gap-2">
              <StatusMark :ok="checkOk('measurement_rtmr3_replay')" />
              <span>{{ t('proof.measure.rtmrReplay') }}</span>
            </li>
            <li class="flex items-start gap-2">
              <StatusMark :ok="checkOk('measurement_app_id')" />
              <span>{{ t('proof.measure.appId') }}</span>
            </li>
            <li class="flex items-start gap-2">
              <StatusMark :ok="bothOk('measurement_compose_hash_eventlog', 'measurement_compose_hash_mrconfigid')" />
              <span>{{ t('proof.measure.composeHash') }}</span>
            </li>
            <li class="flex items-start gap-2">
              <StatusMark :ok="checkOk('measurement_os_image_hash')" />
              <span>{{ t('proof.measure.osImageHash') }}</span>
            </li>
          </ul>
        </section>

        <!-- 3. Open source / provable build -->
        <section class="rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
          <h2 class="text-base font-semibold">{{ t('proof.source.title') }}</h2>
          <p class="mt-2 text-sm text-gray-600 dark:text-dark-300">{{ t('proof.source.desc') }}</p>
          <dl class="mt-3 space-y-2 text-sm">
            <div>
              <dt class="font-medium text-gray-700 dark:text-dark-200">{{ t('proof.source.repo') }}</dt>
              <dd>
                <a :href="reference?.sourceRepo" target="_blank" rel="noopener" class="break-all text-primary-600 hover:underline">
                  {{ reference?.sourceRepo }}
                </a>
              </dd>
            </div>
            <div>
              <dt class="font-medium text-gray-700 dark:text-dark-200">{{ t('proof.source.commit') }}</dt>
              <dd><code class="break-all text-xs">{{ reference?.sourceCommit }}</code></dd>
            </div>
            <div>
              <dt class="font-medium text-gray-700 dark:text-dark-200">{{ t('proof.source.images') }}</dt>
              <dd>
                <ul class="space-y-1">
                  <li v-for="img in reference?.images" :key="img.name">
                    <code class="break-all text-xs">{{ img.digest }}</code>
                  </li>
                </ul>
              </dd>
            </div>
            <div>
              <dt class="font-medium text-gray-700 dark:text-dark-200">{{ t('proof.source.verifyCmd') }}</dt>
              <dd>
                <pre class="mt-1 overflow-x-auto rounded bg-gray-100 p-2 text-xs leading-5 dark:bg-dark-800">{{ cosignCommand }}</pre>
              </dd>
            </div>
          </dl>
        </section>

        <!-- 4. Honest disclosure -->
        <section class="rounded-lg border border-amber-300 bg-amber-50 p-5 dark:border-amber-500/40 dark:bg-amber-500/10">
          <h2 class="text-base font-semibold text-amber-900 dark:text-amber-200">
            {{ t('proof.disclosure.title') }}
          </h2>
          <ul class="mt-2 list-disc space-y-1.5 pl-5 text-sm text-amber-900/90 dark:text-amber-100/90">
            <li>{{ t('proof.disclosure.body1') }}</li>
            <li>{{ t('proof.disclosure.body2') }}</li>
          </ul>
        </section>
      </div>

      <!-- Auditors -->
      <details class="mt-8 rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
        <summary class="cursor-pointer select-none px-5 py-4 text-base font-semibold">
          {{ t('proof.auditors.title') }}
        </summary>
        <div class="border-t border-gray-200 px-5 py-4 dark:border-dark-700">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('proof.auditors.hint') }}</p>
          <dl class="mt-3 grid gap-1 text-xs sm:grid-cols-2">
            <div>
              <dt class="inline font-medium">{{ t('proof.auditors.endpoint') }}:</dt>
              <dd class="inline break-all"><code>{{ attestorUrl }}</code></dd>
            </div>
            <div>
              <dt class="inline font-medium">{{ t('proof.auditors.pccs') }}:</dt>
              <dd class="inline break-all"><code>{{ PHALA_PCCS_URL }}</code></dd>
            </div>
          </dl>

          <h3 class="mt-4 text-sm font-semibold">{{ t('proof.auditors.checks') }}</h3>
          <table class="mt-2 w-full table-fixed text-left text-xs">
            <tbody>
              <tr
                v-for="c in result?.checks ?? []"
                :key="c.id"
                class="border-t border-gray-100 align-top dark:border-dark-800"
              >
                <td class="w-8 py-1.5"><StatusMark :ok="c.gating === false ? undefined : c.ok" /></td>
                <td class="w-64 break-all py-1.5 pr-2 font-mono">{{ c.id }}</td>
                <td class="break-all py-1.5 text-gray-500 dark:text-dark-400">{{ c.detail }}</td>
              </tr>
            </tbody>
          </table>

          <h3 class="mt-4 text-sm font-semibold">{{ t('proof.auditors.refValues') }}</h3>
          <pre class="mt-2 max-h-64 overflow-auto rounded bg-gray-100 p-2 text-xs leading-5 dark:bg-dark-800">{{ referenceJson }}</pre>

          <h3 class="mt-4 text-sm font-semibold">{{ t('proof.auditors.raw') }}</h3>
          <pre class="mt-2 max-h-64 overflow-auto rounded bg-gray-100 p-2 text-xs leading-5 dark:bg-dark-800">{{ rawResponseJson }}</pre>
        </div>
      </details>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ATTESTATION_QUOTE_PATH,
  ATTESTOR_BASE_URL,
  type EnclaveReference,
} from '@/constants/attestation'
import { loadReference, type LoadedReference } from '@/utils/attestation/reference'
import {
  PHALA_PCCS_URL,
  verifyEnclaveQuote,
  type EnclaveQuoteInput,
  type VerifyEnclaveQuoteResult,
} from '@/utils/attestation/tdxVerify'

const { t } = useI18n()

/** ✓ / ✗ / ℹ status glyph. undefined = pending/informational. */
const StatusMark = defineComponent({
  props: { ok: { type: Boolean, default: undefined } },
  setup(props) {
    return () =>
      props.ok === true
        ? h('span', { class: 'text-green-600 dark:text-green-400' }, '✓')
        : props.ok === false
          ? h('span', { class: 'text-red-600 dark:text-red-400' }, '✗')
          : h('span', { class: 'text-gray-400' }, 'ℹ')
  },
})

type Phase = 'idle' | 'running' | 'done' | 'error'
const phase = ref<Phase>('idle')
const errorMsg = ref('')
const loaded = ref<LoadedReference | null>(null)
const result = ref<VerifyEnclaveQuoteResult | null>(null)
const nonceHex = ref('')
const rawResponse = ref<EnclaveQuoteInput | null>(null)

const attestorUrl = `${ATTESTOR_BASE_URL}${ATTESTATION_QUOTE_PATH}`
const reference = computed<EnclaveReference | undefined>(() => loaded.value?.reference)

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
    const res = await fetch(`${attestorUrl}?nonce=${nonceHex.value}`, { cache: 'no-store' })
    if (!res.ok) {
      throw new Error(`${t('proof.errors.fetchFailed')} (HTTP ${res.status})`)
    }
    rawResponse.value = (await res.json()) as EnclaveQuoteInput
    result.value = await verifyEnclaveQuote(rawResponse.value, nonceHex.value, {
      reference: loaded.value.reference,
    })
    phase.value = 'done'
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e)
    phase.value = 'error'
  }
}

function checkOk(id: string): boolean | undefined {
  const c = result.value?.checks.find((x) => x.id === id)
  return c?.ok
}
function bothOk(a: string, b: string): boolean | undefined {
  const ra = checkOk(a)
  const rb = checkOk(b)
  if (ra === undefined && rb === undefined) return undefined
  return ra === true && rb === true
}

const hardwareOk = computed<boolean | undefined>(() => {
  if (!result.value) return undefined
  return (
    checkOk('quote_genuine') === true &&
    checkOk('tcb_status') === true &&
    checkOk('nonce_binding') === true
  )
})
const measureOk = computed<boolean | undefined>(() => {
  if (!result.value) return undefined
  return (
    checkOk('measurement_rtmr3_replay') === true &&
    checkOk('measurement_app_id') === true &&
    bothOk('measurement_compose_hash_eventlog', 'measurement_compose_hash_mrconfigid') === true &&
    checkOk('measurement_os_image_hash') === true
  )
})

const bannerClass = computed(() => {
  if (phase.value === 'done' && result.value?.ok) {
    return 'border-green-300 bg-green-50 text-green-800 dark:border-green-500/40 dark:bg-green-500/10 dark:text-green-200'
  }
  if (phase.value === 'done' || phase.value === 'error') {
    return 'border-red-300 bg-red-50 text-red-800 dark:border-red-500/40 dark:bg-red-500/10 dark:text-red-200'
  }
  return 'border-gray-200 bg-white text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200'
})

const cosignCommand = computed(() => {
  const img = reference.value?.images?.[0]?.digest ?? 'docker.io/markerdao/sub2api@sha256:<digest>'
  return [
    'cosign verify-attestation --type slsaprovenance \\',
    "  --certificate-identity-regexp 'https://github.com/FiiLabs/sub2api/.*' \\",
    '  --certificate-oidc-issuer https://token.actions.githubusercontent.com \\',
    `  ${img}`,
  ].join('\n')
})

const referenceJson = computed(() =>
  loaded.value ? JSON.stringify(loaded.value.reference, null, 2) : '',
)
const rawResponseJson = computed(() =>
  rawResponse.value ? JSON.stringify(rawResponse.value, null, 2) : '',
)

function downloadEvidence(): void {
  const blob = new Blob(
    [
      JSON.stringify(
        {
          fetchedAt: new Date().toISOString(),
          attestorUrl,
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
