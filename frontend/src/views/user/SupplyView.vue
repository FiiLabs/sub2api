<!--
  APEXONE-EXT: 双边市场——供给者自助页（接入 + 仪表盘合一）。

  为什么是一页而不是两页：首版里供给者的全部动作是"挂一个号 / 看它有没有在接单 /
  看赚了多少"。拆成两页会让"我挂的号有没有在赚钱"这个唯一真正的问题，需要来回跳。

  界面上有三件事必须讲清楚，因为它们都是用户会误解的：
    1. 新号不接单（观察期），不是坏了；
    2. 结算开关是独立的，可能"能挂号但暂不计费"；
    3. 冻结中的余额不是丢了，是还没到解冻时间。
-->
<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="loading" class="flex justify-center py-12">
        <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
      </div>

      <!-- 未开放：整页只剩一段说明。没配供给池时下面所有接口都会拒绝，
           显示一堆空表格只会让人以为是加载失败。 -->
      <div v-else-if="!status.enabled" class="card p-8 text-center">
        <Icon name="link" size="lg" class="mx-auto text-gray-400 dark:text-dark-500" />
        <h3 class="mt-3 text-base font-semibold text-gray-900 dark:text-white">
          {{ t('supply.disabled.title') }}
        </h3>
        <p class="mx-auto mt-2 max-w-lg text-sm text-gray-500 dark:text-dark-400">
          {{ t('supply.disabled.body') }}
        </p>
      </div>

      <template v-else>
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('supply.title') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('supply.description') }}</p>
        </div>

        <!-- 接入开着但结算关着：这不是错误状态，但用户必须知道现在挂号不计费 -->
        <div
          v-if="!status.settlement_enabled"
          class="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-900/40 dark:bg-amber-900/20"
        >
          <p class="text-sm font-medium text-amber-800 dark:text-amber-200">
            {{ t('supply.settlementOff.title') }}
          </p>
          <p class="mt-1 text-sm text-amber-700 dark:text-amber-300">{{ t('supply.settlementOff.body') }}</p>
        </div>

        <!-- ===================== 收益 ===================== -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supply.wallet.title') }}</h3>
            <button class="btn btn-secondary btn-sm" :disabled="refreshing" @click="refreshAll">
              <Icon name="refresh" size="sm" />
              <span>{{ t('supply.wallet.refresh') }}</span>
            </button>
          </div>
          <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <div class="card p-5">
              <p class="flex items-center gap-1.5 text-sm text-gray-500 dark:text-dark-400">
                <Icon name="dollar" size="sm" class="text-emerald-500" />
                {{ t('supply.wallet.available') }}
              </p>
              <p class="mt-2 text-2xl font-semibold text-emerald-600 dark:text-emerald-400">
                {{ formatCurrency(wallet?.available_credit ?? 0) }}
              </p>
              <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">{{ t('supply.wallet.availableHint') }}</p>
            </div>
            <div class="card p-5">
              <p class="flex items-center gap-1.5 text-sm text-gray-500 dark:text-dark-400">
                <Icon name="clock" size="sm" class="text-amber-500" />
                {{ t('supply.wallet.frozen') }}
              </p>
              <p class="mt-2 text-2xl font-semibold text-amber-600 dark:text-amber-400">
                {{ formatCurrency(wallet?.frozen_credit ?? 0) }}
              </p>
              <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">{{ t('supply.wallet.frozenHint') }}</p>
            </div>
            <div class="card p-5">
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('supply.wallet.history') }}</p>
              <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
                {{ formatCurrency(wallet?.history_credit ?? 0) }}
              </p>
            </div>
            <div class="card p-5">
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('supply.wallet.spent') }}</p>
              <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
                {{ formatCurrency(wallet?.spent_credit ?? 0) }}
              </p>
            </div>
          </div>
        </div>

        <!-- ===================== 接入 ===================== -->
        <div class="card p-6">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supply.connect.title') }}</h3>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('supply.connect.description') }}</p>

          <button
            v-if="!pendingAuth"
            class="btn btn-primary mt-4"
            :disabled="starting"
            data-testid="supply-start-oauth"
            @click="startOAuth"
          >
            <Icon name="plus" size="sm" />
            <span>{{ starting ? t('supply.connect.starting') : t('supply.connect.start') }}</span>
          </button>

          <div v-else class="mt-4 space-y-4">
            <div class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900">
              <p class="text-sm font-medium text-gray-800 dark:text-gray-100">{{ t('supply.connect.step1') }}</p>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('supply.connect.step1Hint') }}</p>
              <div class="mt-3 flex flex-col gap-2 sm:flex-row">
                <a
                  class="btn btn-primary btn-sm"
                  :href="pendingAuth.auth_url"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <Icon name="externalLink" size="sm" />
                  <span>{{ t('supply.connect.openAuthUrl') }}</span>
                </a>
                <button class="btn btn-secondary btn-sm" @click="copyAuthUrl">
                  <Icon name="copy" size="sm" />
                  <span>{{ t('supply.connect.copyAuthUrl') }}</span>
                </button>
              </div>
              <p class="mt-3 text-xs text-gray-400 dark:text-dark-500">{{ t('supply.connect.expiryHint') }}</p>
            </div>

            <div class="space-y-3">
              <p class="text-sm font-medium text-gray-800 dark:text-gray-100">{{ t('supply.connect.step2') }}</p>
              <div>
                <label class="input-label" for="supply-code">{{ t('supply.connect.codeLabel') }}</label>
                <input
                  id="supply-code"
                  v-model="authCode"
                  class="input"
                  type="text"
                  autocomplete="off"
                  :placeholder="t('supply.connect.codePlaceholder')"
                  data-testid="supply-auth-code"
                />
              </div>
              <div>
                <label class="input-label" for="supply-name">{{ t('supply.connect.nameLabel') }}</label>
                <input
                  id="supply-name"
                  v-model="accountName"
                  class="input"
                  type="text"
                  :placeholder="t('supply.connect.namePlaceholder')"
                />
              </div>
              <p class="text-xs text-gray-400 dark:text-dark-500">{{ t('supply.connect.pendingHint') }}</p>
              <div class="flex gap-2">
                <button
                  class="btn btn-primary"
                  :disabled="submitting"
                  data-testid="supply-complete-oauth"
                  @click="completeOAuth"
                >
                  <Icon name="check" size="sm" />
                  <span>{{ submitting ? t('supply.connect.submitting') : t('supply.connect.submit') }}</span>
                </button>
                <button class="btn btn-secondary" :disabled="submitting" @click="cancelAuth">
                  {{ t('supply.connect.cancel') }}
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- ===================== 我的号 ===================== -->
        <div class="card p-6">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supply.accounts.title') }}</h3>
          <!-- 两条下线通道的边界，写在表格上方而不是塞进按钮 tooltip：
               "已经在流的请求停不掉"是用户唯一会因此投诉的点。 -->
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('supply.accounts.pauseHint') }}</p>

          <p v-if="accounts.length === 0" class="mt-4 text-sm text-gray-500 dark:text-dark-400">
            {{ t('supply.accounts.empty') }}
          </p>

          <div v-else class="mt-4 overflow-x-auto">
            <table class="min-w-full text-sm">
              <thead>
                <tr class="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-dark-700 dark:text-dark-400">
                  <th class="px-3 py-2">{{ t('supply.accounts.name') }}</th>
                  <th class="px-3 py-2">{{ t('supply.accounts.platform') }}</th>
                  <th class="px-3 py-2">{{ t('supply.accounts.state') }}</th>
                  <th class="px-3 py-2">{{ t('supply.accounts.status') }}</th>
                  <th class="px-3 py-2">{{ t('supply.accounts.lastUsedAt') }}</th>
                  <th class="px-3 py-2">{{ t('supply.accounts.createdAt') }}</th>
                  <th class="px-3 py-2 text-right">{{ t('supply.accounts.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                <tr v-for="account in accounts" :key="account.id" :data-testid="`supply-account-${account.id}`">
                  <td class="px-3 py-3">
                    <p class="font-medium text-gray-900 dark:text-white">{{ account.name }}</p>
                    <p v-if="account.email_address" class="text-xs text-gray-400 dark:text-dark-500">
                      {{ account.email_address }}
                    </p>
                  </td>
                  <td class="px-3 py-3 text-gray-700 dark:text-gray-300">{{ account.platform }}</td>
                  <td class="px-3 py-3">
                    <span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="stateBadgeClass(account.supply_state)">
                      {{ stateLabel(account.supply_state) }}
                    </span>
                    <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                      {{ account.schedulable ? t('supply.accounts.schedulable') : t('supply.accounts.notSchedulable') }}
                    </p>

                    <!-- 观察期进度。两个判据分开显示：用户要能分辨"还在等时间"和
                         "我的号连不上"——后者只有他自己能修。 -->
                    <template v-if="account.supply_state === 'pending_review'">
                      <p v-if="account.probe_passes > 0" class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                        {{ t('supply.accounts.probePasses', { passes: account.probe_passes }) }}
                      </p>
                      <p v-if="account.eligible_at" class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                        {{ t('supply.accounts.eligibleAt', { time: formatDateTime(account.eligible_at) }) }}
                      </p>
                      <p v-if="account.probe_error" class="mt-1 max-w-xs text-xs text-red-500">
                        {{ t('supply.accounts.probeError', { reason: account.probe_error }) }}
                      </p>
                    </template>

                    <p
                      v-if="account.supply_state === 'draining' && account.drain_until"
                      class="mt-1 text-xs text-amber-600 dark:text-amber-400"
                    >
                      {{ t('supply.accounts.drainUntil', { time: formatDateTime(account.drain_until) }) }}
                    </p>
                  </td>
                  <td class="px-3 py-3">
                    <span class="text-gray-700 dark:text-gray-300">{{ account.status }}</span>
                    <p v-if="account.error_message" class="mt-1 max-w-xs text-xs text-red-500">
                      {{ account.error_message }}
                    </p>
                  </td>
                  <td class="px-3 py-3 text-gray-500 dark:text-dark-400">
                    {{ account.last_used_at ? formatDateTime(account.last_used_at) : t('supply.accounts.never') }}
                  </td>
                  <td class="px-3 py-3 text-gray-500 dark:text-dark-400">{{ formatDateTime(account.created_at) }}</td>
                  <td class="px-3 py-3 text-right">
                    <!-- 三种状态三套动作。draining 特殊在它同时能"反悔"和"别等了"，
                         所以那两个按钮必须同时在场，否则用户只能干等排空窗。 -->
                    <div class="flex flex-wrap justify-end gap-2">
                      <template v-if="account.supply_state === 'retired'">
                        <button
                          class="btn btn-secondary btn-sm"
                          :disabled="mutatingId === account.id"
                          :data-testid="`supply-resume-${account.id}`"
                          @click="resumeAccount(account)"
                        >
                          {{ mutatingId === account.id ? t('supply.accounts.resuming') : t('supply.accounts.resume') }}
                        </button>
                      </template>
                      <template v-else-if="account.supply_state === 'draining'">
                        <button
                          class="btn btn-secondary btn-sm"
                          :disabled="mutatingId === account.id"
                          :data-testid="`supply-cancel-pause-${account.id}`"
                          @click="resumeAccount(account)"
                        >
                          {{ mutatingId === account.id ? t('supply.accounts.resuming') : t('supply.accounts.cancelPause') }}
                        </button>
                        <button
                          class="btn btn-secondary btn-sm"
                          :disabled="mutatingId === account.id"
                          :data-testid="`supply-pause-immediate-${account.id}`"
                          @click="pauseAccount(account, 'immediate')"
                        >
                          {{ mutatingId === account.id ? t('supply.accounts.pausing') : t('supply.accounts.pauseNow') }}
                        </button>
                      </template>
                      <template v-else>
                        <button
                          class="btn btn-secondary btn-sm"
                          :disabled="mutatingId === account.id"
                          :data-testid="`supply-pause-${account.id}`"
                          @click="pauseAccount(account, 'graceful')"
                        >
                          {{ mutatingId === account.id ? t('supply.accounts.pausing') : t('supply.accounts.pause') }}
                        </button>
                        <button
                          class="btn btn-secondary btn-sm"
                          :disabled="mutatingId === account.id"
                          :data-testid="`supply-pause-immediate-${account.id}`"
                          @click="pauseAccount(account, 'immediate')"
                        >
                          {{ mutatingId === account.id ? t('supply.accounts.pausing') : t('supply.accounts.pauseNow') }}
                        </button>
                      </template>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- ===================== 流水 ===================== -->
        <div class="card p-6">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supply.ledger.title') }}</h3>

          <p v-if="ledger.length === 0" class="mt-4 text-sm text-gray-500 dark:text-dark-400">
            {{ t('supply.ledger.empty') }}
          </p>

          <template v-else>
            <div class="mt-4 overflow-x-auto">
              <table class="min-w-full text-sm">
                <thead>
                  <tr class="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-dark-700 dark:text-dark-400">
                    <th class="px-3 py-2">{{ t('supply.ledger.time') }}</th>
                    <th class="px-3 py-2">{{ t('supply.ledger.action') }}</th>
                    <th class="px-3 py-2 text-right">{{ t('supply.ledger.amount') }}</th>
                    <th class="px-3 py-2 text-right">{{ t('supply.ledger.basis') }}</th>
                    <th class="px-3 py-2 text-right">{{ t('supply.ledger.ratio') }}</th>
                    <th class="px-3 py-2">{{ t('supply.ledger.frozenUntil') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                  <tr v-for="entry in ledger" :key="entry.id">
                    <td class="px-3 py-3 text-gray-500 dark:text-dark-400">{{ formatDateTime(entry.created_at) }}</td>
                    <td class="px-3 py-3 text-gray-700 dark:text-gray-300">{{ actionLabel(entry.action) }}</td>
                    <td class="px-3 py-3 text-right font-medium" :class="entry.amount >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-500'">
                      {{ formatCurrency(entry.amount) }}
                    </td>
                    <td class="px-3 py-3 text-right text-gray-500 dark:text-dark-400">
                      {{ entry.basis_amount === undefined ? '-' : formatCurrency(entry.basis_amount) }}
                    </td>
                    <td class="px-3 py-3 text-right text-gray-500 dark:text-dark-400">
                      {{ entry.share_ratio === undefined ? '-' : `${Math.round(entry.share_ratio * 1000) / 10}%` }}
                    </td>
                    <td class="px-3 py-3 text-gray-500 dark:text-dark-400">
                      {{ entry.frozen_until ? formatDateTime(entry.frozen_until) : '-' }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div class="mt-4 flex items-center justify-between">
              <p class="text-xs text-gray-400 dark:text-dark-500">
                {{ t('supply.ledger.pageInfo', { page: ledgerPage, pages: ledgerPages, total: ledgerTotal }) }}
              </p>
              <div class="flex gap-2">
                <button class="btn btn-secondary btn-sm" :disabled="ledgerPage <= 1" @click="goLedgerPage(ledgerPage - 1)">
                  {{ t('supply.ledger.prev') }}
                </button>
                <button class="btn btn-secondary btn-sm" :disabled="ledgerPage >= ledgerPages" @click="goLedgerPage(ledgerPage + 1)">
                  {{ t('supply.ledger.next') }}
                </button>
              </div>
            </div>
          </template>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { supplyAPI, type SupplyAccount, type SupplyLedgerEntry, type SupplyPauseMode, type SupplyStatus, type SupplyWallet, type StartOAuthResponse } from '@/api/supply'
import { useAppStore } from '@/stores/app'
import { useSupplyStore } from '@/stores/supply'
import { useClipboard } from '@/composables/useClipboard'
import { formatCurrency, formatDateTime } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const supplyStore = useSupplyStore()
const { copyToClipboard } = useClipboard()

const LEDGER_PAGE_SIZE = 20

const loading = ref(true)
const refreshing = ref(false)
const starting = ref(false)
const submitting = ref(false)
const mutatingId = ref<number | null>(null)

const status = ref<SupplyStatus>({ enabled: false, settlement_enabled: false })
const wallet = ref<SupplyWallet | null>(null)
const accounts = ref<SupplyAccount[]>([])
const ledger = ref<SupplyLedgerEntry[]>([])
const ledgerPage = ref(1)
const ledgerPages = ref(1)
const ledgerTotal = ref(0)

const pendingAuth = ref<StartOAuthResponse | null>(null)
const authCode = ref('')
const accountName = ref('')

function stateLabel(state: string): string {
  const known = ['pending_review', 'active', 'draining', 'retired']
  return known.includes(state) ? t(`supply.state.${state}`) : t('supply.state.unknown')
}

function stateBadgeClass(state: string): string {
  switch (state) {
    case 'active':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    case 'retired':
      return 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300'
    case 'draining':
      // 排空中：已经不接新单了，但还没到终态，也还能反悔——用橙色而不是灰色，
      // 灰色会读成"已经结束了"。
      return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-300'
    default:
      // pending_review 及任何未知状态都按"还没在接单"呈现——这是保守的那一边。
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  }
}

function actionLabel(action: string): string {
  const known = ['accrue', 'spend', 'thaw', 'clawback', 'withdraw']
  return known.includes(action) ? t(`supply.action.${action}`) : t('supply.action.unknown')
}

async function loadStatus(): Promise<void> {
  try {
    status.value = await supplyAPI.getStatus()
  } catch (error) {
    // 状态读不到就按"未开放"渲染，而不是把整页错误抛给用户：
    // 后面所有请求都会失败，弹一堆 toast 帮不上任何忙。
    status.value = { enabled: false, settlement_enabled: false }
    appStore.showError(extractApiErrorMessage(error, t('supply.error.loadFailed')))
  }
}

async function loadWallet(): Promise<void> {
  try {
    wallet.value = await supplyAPI.getWallet()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.loadFailed')))
  }
}

async function loadAccounts(): Promise<void> {
  try {
    accounts.value = await supplyAPI.listAccounts()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.loadFailed')))
  }
}

async function loadLedger(page = ledgerPage.value): Promise<void> {
  try {
    const result = await supplyAPI.listLedger({ page, page_size: LEDGER_PAGE_SIZE })
    ledger.value = result?.items ?? []
    ledgerPage.value = result?.page ?? page
    ledgerPages.value = Math.max(result?.pages ?? 1, 1)
    ledgerTotal.value = result?.total ?? 0
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.loadFailed')))
  }
}

async function goLedgerPage(page: number): Promise<void> {
  if (page < 1 || page > ledgerPages.value) return
  await loadLedger(page)
}

/** 并发拉三份数据。任一份失败只影响它自己那一块，不该让整页空着。 */
async function loadAll(): Promise<void> {
  await Promise.all([loadWallet(), loadAccounts(), loadLedger(1)])
}

async function refreshAll(): Promise<void> {
  refreshing.value = true
  try {
    await loadAll()
  } finally {
    refreshing.value = false
  }
}

async function startOAuth(): Promise<void> {
  starting.value = true
  try {
    pendingAuth.value = await supplyAPI.startOAuth()
    authCode.value = ''
    accountName.value = ''
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.startFailed')))
  } finally {
    starting.value = false
  }
}

async function copyAuthUrl(): Promise<void> {
  if (!pendingAuth.value) return
  await copyToClipboard(pendingAuth.value.auth_url, t('supply.connect.authUrlCopied'))
}

function cancelAuth(): void {
  // 只丢掉本地这份 session_id。服务端那行会话留着自然过期（15 分钟），
  // 不发"作废"请求：一个已经没人持有 id 的一次性会话，删不删都一样。
  pendingAuth.value = null
  authCode.value = ''
  accountName.value = ''
}

async function completeOAuth(): Promise<void> {
  if (!pendingAuth.value) return
  const code = authCode.value.trim()
  if (!code) {
    appStore.showError(t('supply.error.codeRequired'))
    return
  }

  submitting.value = true
  try {
    await supplyAPI.completeOAuth({
      session_id: pendingAuth.value.session_id,
      code,
      name: accountName.value.trim() || undefined,
    })
    appStore.showSuccess(t('supply.connect.success'))
    cancelAuth()
    await Promise.all([loadAccounts(), loadWallet()])
  } catch (error) {
    // 失败时保留 pendingAuth：会话可能还没被消费（比如授权码贴错了），
    // 清掉的话用户就得从第一步重来。
    appStore.showError(extractApiErrorMessage(error, t('supply.error.completeFailed')))
  } finally {
    submitting.value = false
  }
}

function replaceAccount(updated: SupplyAccount): void {
  accounts.value = accounts.value.map(item => (item.id === updated.id ? updated : item))
}

/**
 * 下线。两条通道用**不同的确认文案**：immediate 不可撤销，graceful 可以，
 * 共用一句"确定要下线吗"会把这个差别抹掉。
 */
async function pauseAccount(account: SupplyAccount, mode: SupplyPauseMode): Promise<void> {
  const confirmKey = mode === 'immediate' ? 'supply.accounts.pauseNowConfirm' : 'supply.accounts.pauseConfirm'
  if (!window.confirm(t(confirmKey))) return
  mutatingId.value = account.id
  try {
    const updated = await supplyAPI.pauseAccount(account.id, mode)
    replaceAccount(updated)
    // 用返回的实际状态而不是请求的 mode 来选提示：排空窗配成 0 时，
    // graceful 会直接落到 retired，这时说"排空中"就是在骗人。
    appStore.showSuccess(
      updated.supply_state === 'draining' ? t('supply.accounts.draining') : t('supply.accounts.paused')
    )
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.pauseFailed')))
  } finally {
    mutatingId.value = null
  }
}

async function resumeAccount(account: SupplyAccount): Promise<void> {
  const wasDraining = account.supply_state === 'draining'
  mutatingId.value = account.id
  try {
    replaceAccount(await supplyAPI.resumeAccount(account.id))
    // 取消排空是"什么都没发生"，重新挂回是"要重走观察期"——两件事，两句话。
    appStore.showSuccess(wasDraining ? t('supply.accounts.pauseCancelled') : t('supply.accounts.resumed'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.resumeFailed')))
  } finally {
    mutatingId.value = null
  }
}

onMounted(async () => {
  try {
    await loadStatus()
    // 让侧边栏与本页共用同一份判断：用户从这里进来时顺手把 store 也刷新一次。
    supplyStore.enabled = status.value.enabled
    supplyStore.settlementEnabled = status.value.settlement_enabled
    supplyStore.loaded = true
    if (status.value.enabled) {
      await loadAll()
    }
  } finally {
    loading.value = false
  }
})
</script>
