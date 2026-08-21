<!--
  APEXONE-EXT: 双边市场——管理端运营视图（只读）。

  与 SupplyMarketView.vue 分开：那一页是**配置**（改分成比例、改兜底池），这一页是
  **观测**（这个月要付多少、谁的号在被封）。两者的读者、打开频率和风险都不一样，
  合成一页会让人一边翻名册一边不小心动了分成比例。

  除提现审批外，整页没有写操作。改归属、改余额、手工放行观察期都不在这一刀里——
  那些会动钱，需要各自的审计路径（见 service/supplier_admin.go 顶部）。

  提现审批是**唯一**的例外，而且是一个明确的决定而非松了口子：一张已经扣了钱的
  单子必须有人能推进它，不给这个能力那笔钱就永远挂在供给者名下。例外只到
  「改单子状态 + 退款」为止，两个动作都走 /admin/supply/withdrawals/:id/*，
  与其余四个 GET 一样过全部管理端中间件（含审计）。
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
          <select v-model.number="windowDays" class="input w-auto" data-testid="supply-ops-window" @change="onWindowChange">
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
                reverted: formatCurrency(overview.window.withdraw_reverted),
              })
            }}
          </p>
        </div>
      </template>

      <!-- ===================== 提现审批 =====================
           放在看板正下方、名册之前：这是整页**唯一**一件运营打开它要动手做的事，
           其余三块都是查。排在名册后面，会让一张挂了三天没人处理的单子被埋在
           两屏滚动之下。

           这一块也是整页唯一的写路径，与页头那句"整页只读"是明确的例外关系：
           一张已经扣了钱的单子必须有人能推进它，不给这个能力那笔钱就永远挂着。
           例外只到"改单子状态 + 退款"为止，不碰账号、归属、观察期。 -->
      <div class="card space-y-4 p-6" data-testid="supply-ops-withdrawals">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('supplyOps.withdrawals.title') }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('supplyOps.withdrawals.description') }}
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <!-- 默认筛 pending：一屏历史单据里找待处理的那几张，是这一块最常见
                 也最没必要的一次操作。想看历史随时能切。 -->
            <select
              v-model="withdrawalStatus"
              class="input w-auto"
              data-testid="supply-ops-withdrawal-status"
              @change="reloadWithdrawals()"
            >
              <option value="">{{ t('supplyOps.withdrawals.anyStatus') }}</option>
              <option v-for="state in SUPPLY_WITHDRAWAL_STATUSES" :key="state" :value="state">
                {{ t(`supply.withdrawal.state.${state}`) }}
              </option>
            </select>
            <button
              v-if="withdrawalUserId"
              class="btn btn-secondary btn-sm"
              data-testid="supply-ops-withdrawal-user-clear"
              @click="clearWithdrawalUser()"
            >
              {{ t('supplyOps.withdrawals.userFilter', { id: withdrawalUserId }) }} ✕
            </button>
            <button class="btn btn-secondary btn-sm" data-testid="supply-ops-withdrawal-search" @click="reloadWithdrawals()">
              {{ t('supplyOps.search') }}
            </button>
            <!-- 按钮上写明窗口：这张表翻的是全部历史，导出的却只有近 N 天。 -->
            <button
              class="btn btn-secondary btn-sm"
              :disabled="exportingWithdrawals"
              data-testid="supply-ops-withdrawal-export"
              @click="exportWithdrawalsCSV()"
            >
              {{ exportingWithdrawals ? t('supplyOps.export.running') : t('supplyOps.export.button', { days: windowDays }) }}
            </button>
          </div>
        </div>

        <div v-if="withdrawalsLoading" class="py-8 text-center text-sm text-gray-400">{{ t('supplyOps.loading') }}</div>
        <p v-else-if="!withdrawals.length" class="py-8 text-center text-sm text-gray-400">{{ t('supplyOps.empty') }}</p>
        <div v-else class="overflow-x-auto">
          <table class="min-w-full text-left text-sm">
            <thead class="text-xs uppercase text-gray-400 dark:text-dark-500">
              <tr>
                <th class="px-3 py-2">{{ t('supplyOps.withdrawals.requestedAt') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.withdrawals.user') }}</th>
                <th class="px-3 py-2 text-right">{{ t('supplyOps.withdrawals.amount') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.withdrawals.payout') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.withdrawals.status') }}</th>
                <th class="px-3 py-2 text-right">{{ t('supplyOps.withdrawals.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr v-for="item in withdrawals" :key="item.id" :data-testid="`supply-ops-withdrawal-${item.id}`">
                <td class="px-3 py-3 text-gray-500 dark:text-dark-400">{{ formatDateTime(item.created_at) }}</td>
                <td class="px-3 py-3">
                  <button
                    class="text-primary-600 hover:underline dark:text-primary-400"
                    @click="focusWithdrawalUser(item.user_id)"
                  >
                    #{{ item.user_id }}
                  </button>
                </td>
                <td class="px-3 py-3 text-right font-medium text-gray-900 dark:text-white">
                  {{ formatCurrency(item.amount) }}
                </td>
                <td class="px-3 py-3">
                  <p class="text-gray-700 dark:text-gray-300">{{ item.payout_channel }}</p>
                  <!-- 收款账号用等宽字体：打款时要照着它一个字符一个字符核对，
                       比例字体下 l/1/I 分不开。 -->
                  <p class="font-mono text-xs text-gray-500 dark:text-dark-400">{{ item.payout_account }}</p>
                  <p v-if="item.user_note" class="mt-1 max-w-xs text-xs text-gray-400 dark:text-dark-500">
                    {{ item.user_note }}
                  </p>
                </td>
                <td class="px-3 py-3">
                  <span
                    class="rounded-full px-2 py-0.5 text-xs font-medium"
                    :class="withdrawalBadgeClass(item.status)"
                  >
                    {{ t(`supply.withdrawal.state.${item.status}`) }}
                  </span>
                  <p v-if="item.review_note" class="mt-1 max-w-xs text-xs text-gray-400 dark:text-dark-500">
                    {{ item.review_note }}
                  </p>
                  <p v-if="item.external_ref" class="mt-1 max-w-xs font-mono text-xs text-gray-400 dark:text-dark-500">
                    {{ item.external_ref }}
                  </p>
                </td>
                <td class="px-3 py-3 text-right">
                  <div v-if="item.status === 'pending'" class="flex flex-wrap justify-end gap-2">
                    <button
                      class="btn btn-primary btn-sm"
                      :disabled="resolvingWithdrawalId === item.id"
                      :data-testid="`supply-ops-withdrawal-paid-${item.id}`"
                      @click="markPaid(item)"
                    >
                      {{ t('supplyOps.withdrawals.markPaid') }}
                    </button>
                    <button
                      class="btn btn-danger btn-sm"
                      :disabled="resolvingWithdrawalId === item.id"
                      :data-testid="`supply-ops-withdrawal-reject-${item.id}`"
                      @click="rejectWithdrawal(item)"
                    >
                      {{ t('supplyOps.withdrawals.reject') }}
                    </button>
                  </div>
                  <span v-else class="text-xs text-gray-400 dark:text-dark-500">
                    {{ item.resolved_at ? formatDateTime(item.resolved_at) : '-' }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <Pagination
          v-if="withdrawalTotal > 0"
          :total="withdrawalTotal"
          :page="withdrawalPage"
          :page-size="PAGE_SIZE"
          :show-page-size-selector="false"
          @update:page="goWithdrawals"
        />
      </div>

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

      <!-- ===================== 失效事件 =====================
           与上面那张账号明细表刻意分开：那张答的是"这个号**此刻**怎么样"，
           这一段答的是"这段时间**发生过**什么"。号从 error 恢复成 active 的
           那一刻，上面那张表就再也说不出他这个月坏过五次。 -->
      <div class="card space-y-4 p-6">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supplyOps.incidents.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.incidents.description') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <label class="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
              <input
                v-model="incidentOpenOnly"
                type="checkbox"
                data-testid="supply-ops-incident-open"
                @change="reloadIncidents()"
              />
              {{ t('supplyOps.incidents.openOnly') }}
            </label>
            <button
              v-if="incidentUserId"
              class="btn btn-secondary btn-sm"
              data-testid="supply-ops-incident-user-clear"
              @click="clearIncidentUser()"
            >
              {{ t('supplyOps.incidents.userFilter', { id: incidentUserId }) }} ✕
            </button>
          </div>
        </div>

        <!-- 报表四格。第三格与前两格**不是同一个口径**（它不带窗口），
             所以它自己的副标题要把这件事说出来——并排四个数字里混着两种口径，
             而没有任何视觉线索的话，运营会把它们当成同一段时间里的事。 -->
        <div v-if="incidentSummary" class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <div class="card bg-gray-50 p-4 dark:bg-dark-800" data-testid="supply-ops-incident-opened">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.incidents.opened') }}</p>
            <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ incidentSummary.opened }}</p>
            <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
              {{ t('supplyOps.incidents.inWindow', { days: incidentSummary.window_days }) }}
            </p>
          </div>
          <div class="card bg-gray-50 p-4 dark:bg-dark-800" data-testid="supply-ops-incident-resolved">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.incidents.resolved') }}</p>
            <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ incidentSummary.resolved }}</p>
            <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
              {{ t('supplyOps.incidents.inWindow', { days: incidentSummary.window_days }) }}
            </p>
          </div>
          <div class="card bg-gray-50 p-4 dark:bg-dark-800" data-testid="supply-ops-incident-open">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.incidents.open') }}</p>
            <p
              class="mt-1 text-2xl font-semibold"
              :class="incidentSummary.open > 0 ? 'text-red-500' : 'text-gray-900 dark:text-white'"
            >
              {{ incidentSummary.open }}
            </p>
            <p class="mt-1 text-xs text-amber-600 dark:text-amber-400">{{ t('supplyOps.incidents.openNoWindow') }}</p>
          </div>
          <div class="card bg-gray-50 p-4 dark:bg-dark-800" data-testid="supply-ops-incident-suppliers">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyOps.incidents.suppliers') }}</p>
            <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ incidentSummary.suppliers }}</p>
            <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
              {{ t('supplyOps.incidents.ofAccounts', { count: incidentSummary.accounts }) }}
            </p>
          </div>
        </div>

        <!-- 封禁率榜单。零事件的人不在榜上（那是"谁在坏"的榜，不是全体名册）。 -->
        <div v-if="incidentSummary && incidentSummary.top.length" class="overflow-x-auto">
          <table class="min-w-full text-left text-sm">
            <thead class="text-xs uppercase text-gray-400 dark:text-dark-500">
              <tr>
                <th class="px-3 py-2">{{ t('supplyOps.incidents.supplier') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.incidents.accountsCol') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.incidents.incidentsCol') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.incidents.openCol') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.incidents.rateCol') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.incidents.lastDetectedAt') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr v-for="row in incidentSummary.top" :key="row.user_id" :data-testid="`supply-ops-incident-top-${row.user_id}`">
                <td class="px-3 py-3">
                  <button
                    class="text-primary-600 hover:underline dark:text-primary-400"
                    @click="focusIncidentUser(row.user_id)"
                  >
                    {{ row.email || row.username || `#${row.user_id}` }}
                  </button>
                </td>
                <td class="px-3 py-3 text-gray-500 dark:text-dark-400">{{ row.accounts }}</td>
                <td class="px-3 py-3 text-gray-900 dark:text-white">{{ row.incidents }}</td>
                <td class="px-3 py-3" :class="row.open_incidents > 0 ? 'text-red-500' : 'text-gray-500 dark:text-dark-400'">
                  {{ row.open_incidents }}
                </td>
                <!-- 号全解绑的人 accounts = 0，比率恒为 0；这里画成 “—” 而不是
                     0.00，因为一个 0.00 的比率看起来像"他很健康"，而事实是
                     这个比率对他没有意义。 -->
                <td class="px-3 py-3 text-gray-500 dark:text-dark-400">
                  {{ row.accounts > 0 ? row.rate.toFixed(2) : '—' }}
                </td>
                <td class="px-3 py-3 text-gray-500 dark:text-dark-400">
                  {{ row.last_detected_at ? formatDateTime(row.last_detected_at) : '—' }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-if="incidentsLoading" class="py-8 text-center text-sm text-gray-400">{{ t('supplyOps.loading') }}</div>
        <p v-else-if="!incidents.length" class="py-8 text-center text-sm text-gray-400">{{ t('supplyOps.empty') }}</p>
        <div v-else class="overflow-x-auto">
          <table class="min-w-full text-left text-sm">
            <thead class="text-xs uppercase text-gray-400 dark:text-dark-500">
              <tr>
                <th class="px-3 py-2">{{ t('supplyOps.incidents.detectedAt') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.incidents.account') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.incidents.supplier') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.incidents.reason') }}</th>
                <th class="px-3 py-2">{{ t('supplyOps.incidents.state') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr v-for="item in incidents" :key="item.id" :data-testid="`supply-ops-incident-${item.id}`">
                <td class="px-3 py-3 text-gray-500 dark:text-dark-400">{{ formatDateTime(item.detected_at) }}</td>
                <td class="px-3 py-3">
                  <p class="font-medium text-gray-900 dark:text-white">{{ item.account_name || `#${item.account_id}` }}</p>
                  <p class="text-xs text-gray-400 dark:text-dark-500">#{{ item.account_id }} · {{ item.platform }}</p>
                </td>
                <td class="px-3 py-3">
                  <button
                    class="text-primary-600 hover:underline dark:text-primary-400"
                    @click="focusIncidentUser(item.user_id)"
                  >
                    #{{ item.user_id }}
                  </button>
                </td>
                <td class="px-3 py-3">
                  <span class="text-red-500">{{ item.status }}</span>
                  <!-- 上游错误原文只在这里出现：它不进给供给者的那封信（可能带
                       token 片段、内部地址、整段上游 JSON），见 §3.10。 -->
                  <p v-if="item.error_message" class="mt-1 max-w-md text-xs text-gray-400 dark:text-dark-500">
                    {{ item.error_message }}
                  </p>
                </td>
                <td class="px-3 py-3">
                  <span
                    class="rounded-full px-2 py-0.5 text-xs font-medium"
                    :class="
                      item.resolved_at
                        ? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
                        : 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
                    "
                  >
                    {{ item.resolved_at ? t('supplyOps.incidents.closed') : t('supplyOps.incidents.stillOpen') }}
                  </span>
                  <p v-if="item.resolved_at" class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                    {{ formatDateTime(item.resolved_at) }}
                  </p>
                  <!-- 「已通知」画出来是有用的：一个开着好几个小时却始终没通知的
                       事件，说明发信这条链路本身出了问题，而那件事没有别的症状。 -->
                  <p
                    v-if="!item.notified_at"
                    class="mt-1 text-xs text-amber-600 dark:text-amber-400"
                    :data-testid="`supply-ops-incident-unnotified-${item.id}`"
                  >
                    {{ t('supplyOps.incidents.notNotified') }}
                  </p>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <Pagination
          v-if="incidentTotal > 0"
          :total="incidentTotal"
          :page="incidentPage"
          :page-size="PAGE_SIZE"
          :show-page-size-selector="false"
          @update:page="goIncidents"
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
            <button
              class="btn btn-secondary btn-sm"
              :disabled="exportingLedger"
              data-testid="supply-ops-ledger-export"
              @click="exportLedgerCSV()"
            >
              {{ exportingLedger ? t('supplyOps.export.running') : t('supplyOps.export.button', { days: windowDays }) }}
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
  SUPPLY_WITHDRAWAL_STATUSES,
  type SupplierRosterEntry,
  type SupplierRosterSort,
  type SupplyAccountAdminView,
  type SupplyAccountHealth,
  type SupplyAdminLedgerEntry,
  type SupplyIncident,
  type SupplyIncidentSummary,
  type SupplyExportFile,
  type SupplyMarketOverview,
  type SupplyWithdrawalAdminView,
  type SupplyWithdrawalStatus,
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
// 导出是唯一一件"点下去要等好几秒"的事。按钮不锁住的话，运营会以为没反应
// 再点一次，于是同一份几十 MB 的文件被拉两遍。
const exportingWithdrawals = ref(false)
const exportingLedger = ref(false)
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

// 默认筛 pending：运营打开这一页要做的事就是处理待付的单子。
const withdrawals = ref<SupplyWithdrawalAdminView[]>([])
const withdrawalsLoading = ref(true)
const withdrawalStatus = ref<SupplyWithdrawalStatus | ''>('pending')
const withdrawalUserId = ref(0)
const withdrawalPage = ref(1)
const withdrawalTotal = ref(0)
const resolvingWithdrawalId = ref<number | null>(null)

// 失效事件。报表与明细是两条接口（聚合扫全表，不该跟着每次翻页重算），
// 因此这里也是两组状态：summary 只随窗口变，列表随筛选与翻页变。
const incidentSummary = ref<SupplyIncidentSummary | null>(null)
const incidents = ref<SupplyIncident[]>([])
const incidentsLoading = ref(true)
// 默认只看未结的：运营打开这一页最先要回答的是"现在还有哪些号坏着"，
// 而混排列表里那几行会散落在各页。
const incidentOpenOnly = ref(true)
const incidentUserId = ref(0)
const incidentPage = ref(1)
const incidentTotal = ref(0)

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

/**
 * 导出的时间窗。
 *
 * 用页头那个窗口选择器，而不是各自再开一个日期控件：一页上两套时间基准，
 * 运营迟早会导出一份和屏幕上看到的不是同一段时间的文件。
 *
 * 需要注意的是反过来也不成立——屏幕上那两张表（提现、流水）本身**不带窗口**，
 * 它们翻的是全部历史。所以按钮上写明"导出近 N 天"，不让人以为导出的就是
 * 眼前这一页。
 */
function exportWindow(): { start_at: string; end_at: string } {
  const end = new Date()
  const start = new Date(end.getTime() - windowDays.value * 24 * 60 * 60 * 1000)
  return { start_at: start.toISOString(), end_at: end.toISOString() }
}

/**
 * 把拿到的 blob 存到磁盘上，并按完整性给出不同强度的提示。
 *
 * 三档提示是刻意分开的：`truncated` 是"文件是好的，但不全，收窄窗口再来一次"，
 * `incomplete` 是"这份文件不能用来打款"。用同一句话说这两件事，运营只会记住
 * 其中一件。文件名上也各带一个后缀——提示条会消失，文件不会。
 */
function saveSupplyExport(file: SupplyExportFile): void {
  const url = window.URL.createObjectURL(file.blob)
  const link = document.createElement('a')
  link.href = url
  link.download = file.filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  window.URL.revokeObjectURL(url)

  if (file.state === 'incomplete') {
    appStore.showError(t('supplyOps.export.incomplete', { name: file.filename }))
  } else if (file.state === 'truncated') {
    appStore.showWarning(t('supplyOps.export.truncated', { note: file.note }))
  } else {
    appStore.showSuccess(t('supplyOps.export.done', { note: file.note }))
  }
}

async function exportWithdrawalsCSV(): Promise<void> {
  if (exportingWithdrawals.value) return
  exportingWithdrawals.value = true
  try {
    saveSupplyExport(
      await adminSupplyMarketAPI.exportWithdrawals({
        ...exportWindow(),
        status: withdrawalStatus.value || undefined,
        user_id: withdrawalUserId.value || undefined,
      })
    )
  } catch (error) {
    reportError(error, 'supplyOps.error.exportFailed')
  } finally {
    exportingWithdrawals.value = false
  }
}

async function exportLedgerCSV(): Promise<void> {
  if (exportingLedger.value) return
  exportingLedger.value = true
  try {
    saveSupplyExport(
      await adminSupplyMarketAPI.exportLedger({
        ...exportWindow(),
        user_id: ledgerUserId.value || undefined,
        action: ledgerAction.value || undefined,
        request_id: ledgerRequestId.value.trim() || undefined,
      })
    )
  } catch (error) {
    reportError(error, 'supplyOps.error.exportFailed')
  } finally {
    exportingLedger.value = false
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

/**
 * 窗口选择器同时管着看板与失效报表——它们是同一个问题的两半（"这段时间供给侧
 * 怎么样"），两个数字分别属于两个窗口是一屏自相矛盾的读数。
 *
 * 明细表**不跟着变**：它有自己的筛选，而且默认只看未结的（未结事件没有窗口
 * 可言，它就是现在还坏着的那些）。
 */
function onWindowChange(): void {
  void loadOverview()
  void loadIncidentSummary()
}

async function loadIncidentSummary(): Promise<void> {
  try {
    incidentSummary.value = await adminSupplyMarketAPI.getIncidentSummary({ window_days: windowDays.value })
  } catch (error) {
    // 报表挂了不该把下面的明细一起拖走：那两条接口互不依赖，而明细是
    // 出事时真正要看的东西。
    incidentSummary.value = null
    reportError(error, 'supplyOps.error.incidentSummaryFailed')
  }
}

async function loadIncidents(): Promise<void> {
  incidentsLoading.value = true
  try {
    const page = await adminSupplyMarketAPI.listIncidents({
      page: incidentPage.value,
      page_size: PAGE_SIZE,
      user_id: incidentUserId.value || undefined,
      open: incidentOpenOnly.value,
    })
    incidents.value = page.items ?? []
    incidentTotal.value = page.total ?? 0
  } catch (error) {
    reportError(error, 'supplyOps.error.incidentsFailed')
  } finally {
    incidentsLoading.value = false
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

async function loadWithdrawals(): Promise<void> {
  withdrawalsLoading.value = true
  try {
    const page = await adminSupplyMarketAPI.listWithdrawals({
      page: withdrawalPage.value,
      page_size: PAGE_SIZE,
      status: withdrawalStatus.value || undefined,
      user_id: withdrawalUserId.value || undefined,
    })
    withdrawals.value = page.items ?? []
    withdrawalTotal.value = page.total ?? 0
  } catch (error) {
    reportError(error, 'supplyOps.error.withdrawalsFailed')
  } finally {
    withdrawalsLoading.value = false
  }
}

/**
 * 标记已打款。**不退款**——钱在申请那一刻就出了可用区，这一步只是记账收尾。
 *
 * 确认框里带上金额和收款账号，因为这一步之后没有对称的撤销动作：点错行的代价
 * 是一笔本该被拒的钱被记成已付。凭证走 prompt 而不是一个模态表单，是因为这一版
 * 的审批量小到不值得为它做一个组件——但凭证本身不能省成"以后再补"，
 * 那是纠纷时双方唯一的共同锚点。
 */
async function markPaid(item: SupplyWithdrawalAdminView): Promise<void> {
  if (
    !window.confirm(
      t('supplyOps.withdrawals.markPaidConfirm', {
        amount: formatCurrency(item.amount),
        account: item.payout_account,
      })
    )
  ) {
    return
  }
  // prompt 取消（null）与留空（''）是两回事：前者是"我再想想"，要中止；
  // 后者是"这个渠道没有交易号"，照常提交。
  const externalRef = window.prompt(t('supplyOps.withdrawals.externalRefPrompt'), '')
  if (externalRef === null) return

  resolvingWithdrawalId.value = item.id
  try {
    await adminSupplyMarketAPI.markWithdrawalPaid(item.id, { external_ref: externalRef.trim() })
    appStore.showSuccess(t('supplyOps.withdrawals.markedPaid'))
    // 看板上的提现数跟着变，所以两块一起刷。
    await Promise.all([loadWithdrawals(), loadOverview()])
  } catch (error) {
    reportError(error, 'supplyOps.error.withdrawalResolveFailed')
  } finally {
    resolvingWithdrawalId.value = null
  }
}

/** 拒绝并退款。理由必填——一个没有理由的拒绝，对供给者来说和系统故障没有区别。 */
async function rejectWithdrawal(item: SupplyWithdrawalAdminView): Promise<void> {
  const note = window.prompt(t('supplyOps.withdrawals.rejectPrompt', { amount: formatCurrency(item.amount) }), '')
  if (note === null) return
  if (!note.trim()) {
    appStore.showError(t('supplyOps.error.rejectNoteRequired'))
    return
  }

  resolvingWithdrawalId.value = item.id
  try {
    await adminSupplyMarketAPI.rejectWithdrawal(item.id, note.trim())
    appStore.showSuccess(t('supplyOps.withdrawals.rejected'))
    await Promise.all([loadWithdrawals(), loadOverview()])
  } catch (error) {
    reportError(error, 'supplyOps.error.withdrawalResolveFailed')
  } finally {
    resolvingWithdrawalId.value = null
  }
}

function withdrawalBadgeClass(status: string): string {
  switch (status) {
    case 'paid':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    case 'rejected':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    case 'canceled':
      return 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300'
    default:
      // pending 及未知状态都按"还挂着"呈现——保守的那一边。
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
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

function reloadIncidents(): void {
  incidentPage.value = 1
  void loadIncidents()
}

function goIncidents(page: number): void {
  incidentPage.value = page
  void loadIncidents()
}

/**
 * 从榜单点进某个供给者的事件明细。
 *
 * 顺手把「只看未结」关掉：点一个人的目的是"他到底出过什么事"，
 * 留着那个筛子会让一个刚刚全部恢复的人显示成空列表——而他正是榜首。
 */
function focusIncidentUser(userID: number): void {
  incidentUserId.value = userID
  incidentOpenOnly.value = false
  reloadIncidents()
}

function clearIncidentUser(): void {
  incidentUserId.value = 0
  reloadIncidents()
}

function goLedger(page: number): void {
  ledgerPage.value = page
  void loadLedger()
}

function reloadWithdrawals(): void {
  withdrawalPage.value = 1
  void loadWithdrawals()
}

function goWithdrawals(page: number): void {
  withdrawalPage.value = page
  void loadWithdrawals()
}

/**
 * 从审批队列跳到某个供给者。
 *
 * 顺手把状态筛选清掉：点一个人的目的是"这个人的单子都长什么样"，
 * 留着 pending 会让他刚被拒的那张单子消失，看起来像点错了。
 */
function focusWithdrawalUser(userID: number): void {
  withdrawalUserId.value = userID
  withdrawalStatus.value = ''
  reloadWithdrawals()
}

function clearWithdrawalUser(): void {
  withdrawalUserId.value = 0
  reloadWithdrawals()
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
  void loadIncidentSummary()
  void loadIncidents()
  void loadWithdrawals()
  void loadRoster()
  void loadAccounts()
  void loadLedger()
})
</script>
