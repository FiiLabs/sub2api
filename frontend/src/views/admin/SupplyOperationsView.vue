<!--
  APEXONE-EXT: 双边市场——管理端运营视图（只读）。

  与 SupplyMarketView.vue 分开：那一页是**配置**（改分成比例、改兜底池），这一页是
  **观测**（这个月要付多少、谁的号在被封）。两者的读者、打开频率和风险都不一样，
  合成一页会让人一边翻名册一边不小心动了分成比例。

  整页没有一个写操作，对应后端四个 GET。改归属、改余额、手工放行观察期都不在
  这一刀里——那些会动钱，需要各自的审计路径（见 service/supplier_admin.go 顶部）。
-->
<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('supplyOps.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.description') }}</p>
        </div>
        <div class="flex items-center gap-2">
          <label class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.window.label') }}</label>
          <select v-model.number="windowDays" class="input w-auto" data-testid="supply-ops-window" @change="loadOverview">
            <option v-for="days in WINDOW_CHOICES" :key="days" :value="days">
              {{ t('supplyOps.window.days', { days }) }}
            </option>
          </select>
        </div>
      </div>

      <!-- ===================== 看板 ===================== -->
      <div v-if="overviewLoading" class="flex items-center justify-center py-12">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
      </div>
      <template v-else-if="overview">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <!-- 待付负债放在第一格：可用 + 冻结都已经记在供给者名下，差别只是能不能马上取走。 -->
          <div class="card p-5" data-testid="supply-ops-owed">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.overview.owed') }}</p>
            <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ formatCurrency(owed) }}</p>
            <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
              {{
                t('supplyOps.overview.owedBreakdown', {
                  available: formatCurrency(overview.wallet.available),
                  frozen: formatCurrency(overview.wallet.frozen),
                })
              }}
            </p>
          </div>

          <div class="card p-5" data-testid="supply-ops-suppliers">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.overview.suppliers') }}</p>
            <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ overview.suppliers }}</p>
            <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
              {{ t('supplyOps.overview.wallets', { count: overview.wallet.wallets }) }}
            </p>
          </div>

          <div class="card p-5" data-testid="supply-ops-accounts">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.overview.accounts') }}</p>
            <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ overview.accounts.total }}</p>
            <!-- schedulable 与 active 不相等是正常的（限流、临时不可调度、排空中），
                 所以两个数并排显示，不做成一个「异常」提示。 -->
            <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
              {{
                t('supplyOps.overview.accountsBreakdown', {
                  active: overview.accounts.active,
                  schedulable: overview.accounts.schedulable,
                })
              }}
            </p>
          </div>

          <div class="card p-5" data-testid="supply-ops-window-accrued">
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t('supplyOps.overview.accrued', { days: overview.window.days }) }}
            </p>
            <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">
              {{ formatCurrency(overview.window.accrued) }}
            </p>
            <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
              {{
                t('supplyOps.overview.windowBreakdown', {
                  clawed: formatCurrency(overview.window.clawed),
                  spent: formatCurrency(overview.window.spent),
                })
              }}
            </p>
          </div>
        </div>

        <!-- 状态分布 + 两条要盯的读数。解冻量单独说明「不要与入账相加」，
             那是同一笔钱在钱包内部搬家。 -->
        <div class="card space-y-3 p-5">
          <div class="flex flex-wrap gap-2">
            <button
              v-for="bucket in STATE_BUCKETS"
              :key="bucket"
              class="rounded-full px-3 py-1 text-xs font-medium"
              :class="stateBadgeClass(bucket)"
              :data-testid="`supply-ops-bucket-${bucket}`"
              @click="focusState(bucket)"
            >
              {{ stateLabel(bucket) }} · {{ overview.accounts[bucket] }}
            </button>
            <button
              class="rounded-full bg-red-100 px-3 py-1 text-xs font-medium text-red-700 dark:bg-red-900/30 dark:text-red-300"
              data-testid="supply-ops-bucket-unhealthy"
              @click="focusUnhealthy()"
            >
              {{ t('supplyOps.overview.unhealthy') }} · {{ overview.accounts.unhealthy }}
            </button>
          </div>
          <p class="text-xs text-gray-400 dark:text-dark-500">
            {{
              t('supplyOps.overview.thawHint', {
                thawed: formatCurrency(overview.window.thawed),
                withdrawn: formatCurrency(overview.window.withdrawn),
              })
            }}
          </p>
        </div>
      </template>

      <!-- ===================== 名册 ===================== -->
      <div class="card space-y-4 p-6">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supplyOps.roster.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.roster.description') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <input
              v-model="rosterKeyword"
              class="input w-56"
              :placeholder="t('supplyOps.roster.keywordPlaceholder')"
              data-testid="supply-ops-roster-keyword"
              @keyup.enter="reloadRoster()"
            />
            <select v-model="rosterSort" class="input w-auto" data-testid="supply-ops-roster-sort" @change="reloadRoster()">
              <option v-for="sort in SUPPLIER_ROSTER_SORTS" :key="sort" :value="sort">
                {{ t(`supplyOps.roster.sort.${sort}`) }}
              </option>
            </select>
            <button class="btn btn-secondary btn-sm" data-testid="supply-ops-roster-search" @click="reloadRoster()">
              {{ t('supplyOps.search') }}
            </button>
          </div>
        </div>

        <div v-if="rosterLoading" class="py-8 text-center text-sm text-gray-400">{{ t('supplyOps.loading') }}</div>
        <p v-else-if="!roster.length" class="py-8 text-center text-sm text-gray-400">{{ t('supplyOps.empty') }}</p>
        <div v-else class="overflow-x-auto">
          <table class="min-w-full text-left text-sm">
            <thead class="text-xs uppercase text-gray-400 dark:text-dark-500">
              <tr>
                <th class="px-3 py-2">{{ t('supplyOps.roster.supplier') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.roster.accounts') }}</th>
                <th class="px-3 py-2 text-right">{{ t('supplyOps.roster.owed') }}</th>
                <th class="px-3 py-2 text-right">{{ t('supplyOps.roster.history') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.roster.lastAccrual') }}</th>
                <th class="px-3 py-2 text-right">{{ t('supplyOps.roster.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr v-for="entry in roster" :key="entry.user_id" :data-testid="`supply-ops-supplier-${entry.user_id}`">
                <td class="px-3 py-3">
                  <p class="font-medium text-gray-900 dark:text-white">{{ entry.email }}</p>
                  <p class="text-xs text-gray-400 dark:text-dark-500">
                    #{{ entry.user_id }}
                    <span v-if="entry.username"> · {{ entry.username }}</span>
                    <!-- 被封的用户仍然可能有余额待付，所以状态照常显示而不是把这一行藏起来。 -->
                    <span v-if="entry.user_status && entry.user_status !== 'active'" class="text-red-500">
                      · {{ entry.user_status }}
                    </span>
                  </p>
                </td>
                <td class="px-3 py-3 text-gray-700 dark:text-gray-300">
                  {{ entry.accounts.total }}
                  <span class="text-xs text-gray-400 dark:text-dark-500">
                    ({{ t('supplyOps.roster.accountsHint', {
                      active: entry.accounts.active,
                      pending: entry.accounts.pending_review,
                      unhealthy: entry.accounts.unhealthy,
                    }) }})
                  </span>
                </td>
                <td class="px-3 py-3 text-right font-medium text-gray-900 dark:text-white">
                  {{ formatCurrency(entry.wallet.available + entry.wallet.frozen) }}
                </td>
                <td class="px-3 py-3 text-right text-gray-500 dark:text-dark-400">
                  {{ formatCurrency(entry.wallet.history) }}
                </td>
                <td class="px-3 py-3 text-gray-500 dark:text-dark-400">
                  {{ entry.last_accrual_at ? formatDateTime(entry.last_accrual_at) : t('supplyOps.roster.neverAccrued') }}
                </td>
                <td class="px-3 py-3 text-right">
                  <div class="flex flex-wrap justify-end gap-2">
                    <button class="btn btn-secondary btn-sm" @click="focusOwner(entry.user_id)">
                      {{ t('supplyOps.roster.viewAccounts') }}
                    </button>
                    <button class="btn btn-secondary btn-sm" @click="focusLedgerUser(entry.user_id)">
                      {{ t('supplyOps.roster.viewLedger') }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <Pagination
          v-if="rosterTotal > 0"
          :total="rosterTotal"
          :page="rosterPage"
          :page-size="PAGE_SIZE"
          :show-page-size-selector="false"
          @update:page="goRoster"
        />
      </div>

      <!-- ===================== 账号明细 ===================== -->
      <div class="card space-y-4 p-6">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supplyOps.accounts.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.accounts.description') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <select v-model="accountState" class="input w-auto" data-testid="supply-ops-account-state" @change="reloadAccounts()">
              <option value="">{{ t('supplyOps.accounts.anyState') }}</option>
              <option v-for="bucket in STATE_BUCKETS" :key="bucket" :value="bucket">{{ stateLabel(bucket) }}</option>
            </select>
            <select v-model="accountHealth" class="input w-auto" data-testid="supply-ops-account-health" @change="reloadAccounts()">
              <option value="">{{ t('supplyOps.accounts.anyHealth') }}</option>
              <option value="healthy">{{ t('supplyOps.accounts.healthy') }}</option>
              <option value="unhealthy">{{ t('supplyOps.accounts.unhealthy') }}</option>
            </select>
            <button
              v-if="accountOwnerId"
              class="btn btn-secondary btn-sm"
              data-testid="supply-ops-account-owner-clear"
              @click="clearOwner()"
            >
              {{ t('supplyOps.accounts.ownerFilter', { id: accountOwnerId }) }} ✕
            </button>
          </div>
        </div>

        <div v-if="accountsLoading" class="py-8 text-center text-sm text-gray-400">{{ t('supplyOps.loading') }}</div>
        <p v-else-if="!accounts.length" class="py-8 text-center text-sm text-gray-400">{{ t('supplyOps.empty') }}</p>
        <div v-else class="overflow-x-auto">
          <table class="min-w-full text-left text-sm">
            <thead class="text-xs uppercase text-gray-400 dark:text-dark-500">
              <tr>
                <th class="px-3 py-2">{{ t('supplyOps.accounts.account') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.accounts.owner') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.accounts.state') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.accounts.health') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.accounts.lastUsedAt') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr v-for="account in accounts" :key="account.id" :data-testid="`supply-ops-account-${account.id}`">
                <td class="px-3 py-3">
                  <p class="font-medium text-gray-900 dark:text-white">{{ account.name }}</p>
                  <p class="text-xs text-gray-400 dark:text-dark-500">
                    #{{ account.id }} · {{ account.platform }}
                    <span v-if="account.email_address"> · {{ account.email_address }}</span>
                  </p>
                </td>
                <td class="px-3 py-3">
                  <button class="text-primary-600 hover:underline dark:text-primary-400" @click="focusLedgerUser(account.owner_user_id)">
                    {{ account.owner_email || `#${account.owner_user_id}` }}
                  </button>
                </td>
                <td class="px-3 py-3">
                  <span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="stateBadgeClass(account.supply_state)">
                    {{ stateLabel(account.supply_state) }}
                  </span>
                  <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                    {{ account.schedulable ? t('supplyOps.accounts.schedulable') : t('supplyOps.accounts.notSchedulable') }}
                  </p>
                  <!-- 「卡在观察期」要能一眼看出卡在哪一边：是还没攒够探测次数，
                       还是探测一直在失败（后者只有供给者本人能修）。 -->
                  <template v-if="account.supply_state === 'pending_review'">
                    <p v-if="account.probation_since" class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                      {{ t('supplyOps.accounts.probationSince', { time: formatDateTime(account.probation_since) }) }}
                    </p>
                    <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                      {{ t('supplyOps.accounts.probePasses', { passes: account.probe_passes }) }}
                    </p>
                    <p v-if="account.probe_error" class="mt-1 max-w-xs text-xs text-red-500">
                      {{ t('supplyOps.accounts.probeError', { reason: account.probe_error }) }}
                    </p>
                  </template>
                  <p
                    v-else-if="account.supply_state === 'draining' && account.drain_until"
                    class="mt-1 text-xs text-amber-600 dark:text-amber-400"
                  >
                    {{ t('supplyOps.accounts.drainUntil', { time: formatDateTime(account.drain_until) }) }}
                  </p>
                </td>
                <td class="px-3 py-3">
                  <span :class="account.status === 'active' ? 'text-gray-700 dark:text-gray-300' : 'text-red-500'">
                    {{ account.status }}
                  </span>
                  <p v-if="account.error_message" class="mt-1 max-w-xs text-xs text-red-500">{{ account.error_message }}</p>
                </td>
                <td class="px-3 py-3 text-gray-500 dark:text-dark-400">
                  {{ account.last_used_at ? formatDateTime(account.last_used_at) : t('supplyOps.accounts.never') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <Pagination
          v-if="accountTotal > 0"
          :total="accountTotal"
          :page="accountPage"
          :page-size="PAGE_SIZE"
          :show-page-size-selector="false"
          @update:page="goAccounts"
        />
      </div>

      <!-- ===================== 全站流水 ===================== -->
      <div class="card space-y-4 p-6">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supplyOps.ledger.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.ledger.description') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <select v-model="ledgerAction" class="input w-auto" data-testid="supply-ops-ledger-action" @change="reloadLedger()">
              <option value="">{{ t('supplyOps.ledger.anyAction') }}</option>
              <option v-for="action in LEDGER_ACTIONS" :key="action" :value="action">
                {{ t(`supply.action.${action}`) }}
              </option>
            </select>
            <input
              v-model="ledgerRequestId"
              class="input w-56"
              :placeholder="t('supplyOps.ledger.requestIdPlaceholder')"
              data-testid="supply-ops-ledger-request"
              @keyup.enter="reloadLedger()"
            />
            <button
              v-if="ledgerUserId"
              class="btn btn-secondary btn-sm"
              data-testid="supply-ops-ledger-user-clear"
              @click="clearLedgerUser()"
            >
              {{ t('supplyOps.ledger.userFilter', { id: ledgerUserId }) }} ✕
            </button>
            <button class="btn btn-secondary btn-sm" data-testid="supply-ops-ledger-search" @click="reloadLedger()">
              {{ t('supplyOps.search') }}
            </button>
          </div>
        </div>

        <div v-if="ledgerLoading" class="py-8 text-center text-sm text-gray-400">{{ t('supplyOps.loading') }}</div>
        <p v-else-if="!ledger.length" class="py-8 text-center text-sm text-gray-400">{{ t('supplyOps.empty') }}</p>
        <div v-else class="overflow-x-auto">
          <table class="min-w-full text-left text-sm">
            <thead class="text-xs uppercase text-gray-400 dark:text-dark-500">
              <tr>
                <th class="px-3 py-2">{{ t('supplyOps.ledger.time') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.ledger.user') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.ledger.action') }}</th>
                <th class="px-3 py-2 text-right">{{ t('supplyOps.ledger.amount') }}</th>
                <th class="px-3 py-2 text-right">{{ t('supplyOps.ledger.basis') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.ledger.requestId') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr v-for="entry in ledger" :key="entry.id" :data-testid="`supply-ops-ledger-${entry.id}`">
                <td class="px-3 py-3 text-gray-500 dark:text-dark-400">{{ formatDateTime(entry.created_at) }}</td>
                <td class="px-3 py-3">
                  <button class="text-primary-600 hover:underline dark:text-primary-400" @click="focusLedgerUser(entry.user_id)">
                    {{ entry.user_email || `#${entry.user_id}` }}
                  </button>
                </td>
                <td class="px-3 py-3 text-gray-700 dark:text-gray-300">{{ actionLabel(entry.action) }}</td>
                <!-- 追回是负向动作，金额用红色：一屏流水里最该被一眼认出来的就是它。 -->
                <td
                  class="px-3 py-3 text-right font-medium"
                  :class="entry.action === 'clawback' ? 'text-red-500' : 'text-gray-900 dark:text-white'"
                >
                  {{ formatCurrency(entry.amount) }}
                </td>
                <td class="px-3 py-3 text-right text-gray-500 dark:text-dark-400">
                  {{ entry.basis_amount === undefined ? '-' : formatCurrency(entry.basis_amount) }}
                </td>
                <td class="px-3 py-3 font-mono text-xs text-gray-400 dark:text-dark-500">{{ entry.request_id || '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <Pagination
          v-if="ledgerTotal > 0"
          :total="ledgerTotal"
          :page="ledgerPage"
          :page-size="PAGE_SIZE"
          :show-page-size-selector="false"
          @update:page="goLedger"
        />
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import {
  adminSupplyMarketAPI,
  SUPPLIER_ROSTER_SORTS,
  type SupplierRosterEntry,
  type SupplierRosterSort,
  type SupplyAccountAdminView,
  type SupplyAccountHealth,
  type SupplyAdminLedgerEntry,
  type SupplyMarketOverview,
} from '@/api/admin/supplyMarket'
import { useAppStore } from '@/stores/app'
import { formatCurrency, formatDateTime } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const PAGE_SIZE = 20
const WINDOW_CHOICES = [7, 30, 90] as const
// 与后端 SupplyAccountCounts 的四个状态桶一一对应。写死是刻意的：多一个状态
// 应该在这里编译不过，而不是画出一个没有翻译的格子。
const STATE_BUCKETS = ['pending_review', 'active', 'draining', 'retired'] as const
const LEDGER_ACTIONS = ['accrue', 'thaw', 'spend', 'clawback', 'withdraw'] as const

const windowDays = ref<number>(30)
const overview = ref<SupplyMarketOverview | null>(null)
const overviewLoading = ref(true)

/** 待付负债 = 可用 + 冻结。两笔钱都已记在供给者名下，差别只是能不能马上取走。 */
const owed = computed(() => (overview.value ? overview.value.wallet.available + overview.value.wallet.frozen : 0))

const roster = ref<SupplierRosterEntry[]>([])
const rosterLoading = ref(true)
const rosterKeyword = ref('')
const rosterSort = ref<SupplierRosterSort>('owed')
const rosterPage = ref(1)
const rosterTotal = ref(0)

const accounts = ref<SupplyAccountAdminView[]>([])
const accountsLoading = ref(true)
const accountState = ref('')
const accountHealth = ref<SupplyAccountHealth>('')
const accountOwnerId = ref(0)
const accountPage = ref(1)
const accountTotal = ref(0)

const ledger = ref<SupplyAdminLedgerEntry[]>([])
const ledgerLoading = ref(true)
const ledgerAction = ref('')
const ledgerRequestId = ref('')
const ledgerUserId = ref(0)
const ledgerPage = ref(1)
const ledgerTotal = ref(0)

function stateLabel(state: string): string {
  return (STATE_BUCKETS as readonly string[]).includes(state) ? t(`supply.state.${state}`) : t('supply.state.unknown')
}

function actionLabel(action: string): string {
  return (LEDGER_ACTIONS as readonly string[]).includes(action) ? t(`supply.action.${action}`) : action
}

function stateBadgeClass(state: string): string {
  switch (state) {
    case 'active':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    case 'retired':
      return 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300'
    case 'draining':
      return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-300'
    default:
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  }
}

/** 出错时只报一次，不清空已经显示的数据：一个半旧的看板也比一屏空白有用。 */
function reportError(error: unknown, fallbackKey: string): void {
  appStore.showError(extractApiErrorMessage(error, t(fallbackKey)))
}

async function loadOverview(): Promise<void> {
  overviewLoading.value = true
  try {
    overview.value = await adminSupplyMarketAPI.getOverview(windowDays.value)
  } catch (error) {
    reportError(error, 'supplyOps.error.overviewFailed')
  } finally {
    overviewLoading.value = false
  }
}

async function loadRoster(): Promise<void> {
  rosterLoading.value = true
  try {
    const page = await adminSupplyMarketAPI.listSuppliers({
      page: rosterPage.value,
      page_size: PAGE_SIZE,
      keyword: rosterKeyword.value.trim() || undefined,
      sort: rosterSort.value,
    })
    roster.value = page.items ?? []
    rosterTotal.value = page.total ?? 0
  } catch (error) {
    reportError(error, 'supplyOps.error.rosterFailed')
  } finally {
    rosterLoading.value = false
  }
}

async function loadAccounts(): Promise<void> {
  accountsLoading.value = true
  try {
    const page = await adminSupplyMarketAPI.listAccounts({
      page: accountPage.value,
      page_size: PAGE_SIZE,
      state: accountState.value || undefined,
      health: accountHealth.value || undefined,
      owner_user_id: accountOwnerId.value || undefined,
    })
    accounts.value = page.items ?? []
    accountTotal.value = page.total ?? 0
  } catch (error) {
    reportError(error, 'supplyOps.error.accountsFailed')
  } finally {
    accountsLoading.value = false
  }
}

async function loadLedger(): Promise<void> {
  ledgerLoading.value = true
  try {
    const page = await adminSupplyMarketAPI.listLedger({
      page: ledgerPage.value,
      page_size: PAGE_SIZE,
      user_id: ledgerUserId.value || undefined,
      action: ledgerAction.value || undefined,
      request_id: ledgerRequestId.value.trim() || undefined,
    })
    ledger.value = page.items ?? []
    ledgerTotal.value = page.total ?? 0
  } catch (error) {
    reportError(error, 'supplyOps.error.ledgerFailed')
  } finally {
    ledgerLoading.value = false
  }
}

// 改筛选条件一律回到第一页：留在第 7 页翻一个只有 2 页的结果集，界面上是一片空白，
// 读起来像「没有数据」。
function reloadRoster(): void {
  rosterPage.value = 1
  void loadRoster()
}

function reloadAccounts(): void {
  accountPage.value = 1
  void loadAccounts()
}

function reloadLedger(): void {
  ledgerPage.value = 1
  void loadLedger()
}

function goRoster(page: number): void {
  rosterPage.value = page
  void loadRoster()
}

function goAccounts(page: number): void {
  accountPage.value = page
  void loadAccounts()
}

function goLedger(page: number): void {
  ledgerPage.value = page
  void loadLedger()
}

/** 看板上的状态桶点一下就把账号表筛到那个状态——「哪些卡在观察期」是一次点击的事。 */
function focusState(state: string): void {
  accountState.value = state
  accountHealth.value = ''
  reloadAccounts()
}

function focusUnhealthy(): void {
  accountState.value = ''
  accountHealth.value = 'unhealthy'
  reloadAccounts()
}

function focusOwner(userID: number): void {
  accountOwnerId.value = userID
  accountState.value = ''
  accountHealth.value = ''
  reloadAccounts()
}

function clearOwner(): void {
  accountOwnerId.value = 0
  reloadAccounts()
}

function focusLedgerUser(userID: number): void {
  ledgerUserId.value = userID
  reloadLedger()
}

function clearLedgerUser(): void {
  ledgerUserId.value = 0
  reloadLedger()
}

onMounted(() => {
  void loadOverview()
  void loadRoster()
  void loadAccounts()
  void loadLedger()
})
</script>
