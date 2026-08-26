<!--
  APEXONE-EXT: 双边市场——管理端配置页。

  单起一页而不是往 SettingsView.vue 里加两个 section：那个文件已经近九千行，
  是上游合并最痛的一处。独立页只需要在路由表和侧边栏各加一条。

  五组配置各自一个保存按钮，与后端五对端点一一对应：改分成比例、改兜底池、
  改观察期、改协议、改提现参数是五件不同的事，共用一次提交会让审计日志
  分不清谁改了什么。
-->
<template>
  <AppLayout>
    <div class="space-y-6">
      <div>
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('supplyAdmin.title') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.description') }}</p>
      </div>

      <div v-if="loading" class="flex items-center justify-center py-16">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
      </div>

      <template v-else>
        <!-- ===================== 定价与供给健康度（只读） =====================
             排在所有配置卡之前：下面每一个参数配得对不对，只能由这张卡上「从钱
             反推出来」的读数回答，而不是由输入框里写着什么回答。运营改分成之前
             先看见实测产出，是这一页唯一能防住「照着一个没实测过的假设调价」的地方。 -->
        <div class="card space-y-4 p-6" data-testid="supply-health-card">
          <div class="flex flex-wrap items-end justify-between gap-3">
            <div>
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supplyAdmin.health.title') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.health.description') }}</p>
            </div>
            <div class="flex items-center gap-2">
              <label class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.health.window') }}</label>
              <select
                v-model.number="healthWindowDays"
                class="input w-auto"
                data-testid="supply-health-window"
                @change="loadHealth()"
              >
                <option v-for="days in HEALTH_WINDOW_CHOICES" :key="days" :value="days">
                  {{ t('supplyAdmin.health.windowDays', { days }) }}
                </option>
              </select>
            </div>
          </div>

          <div v-if="healthLoading" class="flex items-center justify-center py-10">
            <div class="h-6 w-6 animate-spin rounded-full border-b-2 border-primary-600"></div>
          </div>

          <!-- 读数拉不到时说出来并给一个重试。静默留白会被读成「这个窗口没数据」，
               而那两件事该采取的动作完全相反。 -->
          <div
            v-else-if="!health"
            class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900"
            data-testid="supply-health-error"
          >
            <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('supplyAdmin.health.loadFailed') }}</p>
            <button class="btn btn-secondary btn-sm mt-2" data-testid="supply-health-retry" @click="loadHealth()">
              {{ t('supplyAdmin.health.retry') }}
            </button>
          </div>

          <template v-else>
            <!-- 兜底告警放在空状态**之前**：供给池与兜底池同时空掉时请求是被拒的，
                 不产生任何用量——那恰恰是这张卡显示「窗口内没有流水」的时刻，
                 把告警塞进有数据的分支等于让它在最该出现的时候消失。 -->
            <div
              v-if="health.exhausted_today > 0"
              class="rounded-lg border border-red-200 bg-red-50 p-3 dark:border-red-900/40 dark:bg-red-900/20"
              data-testid="supply-health-exhausted"
            >
              <p class="text-xs font-medium text-red-700 dark:text-red-300">
                {{ t('supplyAdmin.health.exhausted.title', { count: health.exhausted_today }) }}
              </p>
              <p class="mt-1 text-xs text-red-600 dark:text-red-400">{{ t('supplyAdmin.health.exhausted.body') }}</p>
            </div>

            <!-- 没有流水时四格与自检整块不画：那些比率的分母是流水，画出来只会是
                 一排 0 和一个必然越界的偏差，读者会以为参数配错了。 -->
            <div
              v-if="!healthHasTraffic"
              class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900"
              data-testid="supply-health-empty"
            >
              <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('supplyAdmin.health.empty.title') }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.health.empty.body') }}</p>
            </div>

            <template v-else>
              <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
                <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700" data-testid="supply-health-list-value">
                  <p class="text-sm text-gray-500 dark:text-gray-400">
                    {{ t('supplyAdmin.health.listValue', { days: health.window_days }) }}
                  </p>
                  <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">
                    {{ formatCurrency(health.list_value) }}
                  </p>
                  <!-- 牌价等值与实付并排：两者差着一个倍率，只报前者会把营收虚报数倍。 -->
                  <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                    {{ t('supplyAdmin.health.listValueHint', { revenue: formatCurrency(health.revenue) }) }}
                  </p>
                </div>

                <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700" data-testid="supply-health-gross-margin">
                  <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.health.grossMargin') }}</p>
                  <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">
                    {{ formatCurrency(health.gross_margin) }}
                  </p>
                  <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                    {{
                      t('supplyAdmin.health.grossMarginHint', {
                        revenue: formatCurrency(health.revenue),
                        payout: formatCurrency(health.supplier_payout),
                      })
                    }}
                  </p>
                </div>

                <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700" data-testid="supply-health-median-output">
                  <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.health.medianOutput') }}</p>
                  <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">
                    {{ formatCurrency(health.median_monthly_output) }}
                  </p>
                  <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                    {{
                      t('supplyAdmin.health.medianOutputHint', {
                        suppliers: health.supplier_count,
                        accounts: health.supply_accounts.length,
                      })
                    }}
                  </p>
                </div>

                <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700" data-testid="supply-health-overflow-share">
                  <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.health.overflowShare') }}</p>
                  <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">
                    {{ formatPercent(health.overflow_share) }}
                  </p>
                  <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                    {{ t('supplyAdmin.health.overflowShareHint', { value: formatCurrency(health.overflow_list_value) }) }}
                  </p>
                </div>
              </div>

              <!-- 配置自检。两格并排是因为它们是同一个问题的两半：钱进来时用的倍率、
                   钱出去时用的分成，只对上一半说明不了任何事。 -->
              <div class="space-y-2 border-t border-gray-100 pt-4 dark:border-dark-700">
                <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('supplyAdmin.health.selfCheck.title') }}</p>
                <div class="grid gap-3 sm:grid-cols-2">
                  <div
                    class="rounded-lg border p-3"
                    :class="multiplierCheck.mismatched ? HEALTH_DRIFT_BOX : HEALTH_PLAIN_BOX"
                    data-testid="supply-health-check-multiplier"
                  >
                    <p class="text-xs font-medium text-gray-700 dark:text-gray-300">
                      {{ t('supplyAdmin.health.selfCheck.multiplier') }}
                    </p>
                    <p class="mt-1 text-sm text-gray-900 dark:text-white">
                      {{ t('supplyAdmin.health.selfCheck.effective', { value: formatRatio(health.effective_multiplier) }) }}
                      ·
                      {{ t('supplyAdmin.health.selfCheck.configured', { value: formatRatio(health.configured_multiplier) }) }}
                    </p>
                    <p class="mt-1 text-xs" :class="multiplierCheck.mismatched ? HEALTH_DRIFT_TEXT : HEALTH_PLAIN_TEXT">
                      {{ checkStateLabel(multiplierCheck, 'supplyAdmin.health.selfCheck.noPool') }}
                    </p>
                  </div>

                  <div
                    class="rounded-lg border p-3"
                    :class="shareCheck.mismatched ? HEALTH_DRIFT_BOX : HEALTH_PLAIN_BOX"
                    data-testid="supply-health-check-share"
                  >
                    <p class="text-xs font-medium text-gray-700 dark:text-gray-300">
                      {{ t('supplyAdmin.health.selfCheck.share') }}
                    </p>
                    <p class="mt-1 text-sm text-gray-900 dark:text-white">
                      {{ t('supplyAdmin.health.selfCheck.effective', { value: formatRatio(health.effective_share) }) }}
                      ·
                      {{ t('supplyAdmin.health.selfCheck.configured', { value: formatRatio(health.configured_share) }) }}
                    </p>
                    <p class="mt-1 text-xs" :class="shareCheck.mismatched ? HEALTH_DRIFT_TEXT : HEALTH_PLAIN_TEXT">
                      {{ checkStateLabel(shareCheck, 'supplyAdmin.health.selfCheck.noShare') }}
                    </p>
                  </div>
                </div>

                <!-- 长解释只出现一次而不是在两格里各抄一遍：它对两者是同一段话，
                     而重复的长句会让人跳过它——跳过的正是「调价后属正常」那一句。 -->
                <div
                  v-if="multiplierCheck.mismatched || shareCheck.mismatched"
                  class="rounded-lg border border-amber-200 bg-amber-50 p-3 dark:border-amber-900/40 dark:bg-amber-900/20"
                  data-testid="supply-health-mismatch"
                >
                  <p class="text-xs text-amber-700 dark:text-amber-300">{{ t('supplyAdmin.health.selfCheck.mismatch') }}</p>
                </div>
              </div>

              <!-- 产出榜。这张表回答定价方案压着的那个未实测假设：一个号一个月
                   到底产出多少牌价等值。 -->
              <div class="space-y-2 border-t border-gray-100 pt-4 dark:border-dark-700">
                <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('supplyAdmin.health.accounts.title') }}</p>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.health.accounts.description') }}</p>

                <p
                  v-if="!health.supply_accounts.length"
                  class="py-6 text-center text-sm text-gray-400"
                  data-testid="supply-health-accounts-empty"
                >
                  {{ t('supplyAdmin.health.accounts.empty') }}
                </p>
                <div v-else class="overflow-x-auto">
                  <!-- 后端已按产出降序排好，这里不再排：再排一次意味着「哪个是榜首」
                       有两个来源，而两边的口径迟早会分叉。 -->
                  <table class="min-w-full text-left text-sm">
                    <thead class="text-xs uppercase text-gray-400 dark:text-dark-500">
                      <tr>
                        <th class="px-3 py-2">{{ t('supplyAdmin.health.accounts.name') }}</th>
                        <th class="px-3 py-2 text-right">{{ t('supplyAdmin.health.accounts.monthlyOutput') }}</th>
                        <th class="px-3 py-2 text-right">{{ t('supplyAdmin.health.accounts.earned') }}</th>
                        <th class="px-3 py-2 text-right">{{ t('supplyAdmin.health.accounts.requests') }}</th>
                      </tr>
                    </thead>
                    <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                      <tr
                        v-for="account in health.supply_accounts"
                        :key="account.account_id"
                        :class="isLowOutput(account.monthly_output) ? 'text-gray-400 dark:text-dark-500' : ''"
                        :data-testid="`supply-health-account-${account.account_id}`"
                      >
                        <td class="px-3 py-3">
                          <span>{{ account.name }}</span>
                          <span
                            v-if="isLowOutput(account.monthly_output)"
                            class="ml-2 rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-500 dark:bg-dark-800 dark:text-dark-400"
                            :data-testid="`supply-health-account-low-${account.account_id}`"
                          >
                            {{ t('supplyAdmin.health.accounts.low') }}
                          </span>
                        </td>
                        <td class="px-3 py-3 text-right">{{ formatCurrency(account.monthly_output) }}</td>
                        <td class="px-3 py-3 text-right">{{ formatCurrency(account.supplier_earned) }}</td>
                        <td class="px-3 py-3 text-right">{{ account.requests }}</td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </div>
            </template>
          </template>
        </div>

        <!-- ===================== 结算参数 ===================== -->
        <div class="card space-y-4 p-6">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supplyAdmin.settlement.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.settlement.description') }}</p>
          </div>

          <div class="flex items-center justify-between">
            <div>
              <label class="font-medium text-gray-900 dark:text-white">{{ t('supplyAdmin.settlement.enabled') }}</label>
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.settlement.enabledHint') }}</p>
            </div>
            <Toggle v-model="settlementForm.enabled" data-testid="supply-settlement-enabled" />
          </div>

          <div class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700">
            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.settlement.shareRatio') }}
              </label>
              <input
                v-model.number="settlementForm.share_ratio"
                type="number"
                step="0.01"
                min="0"
                :max="settlementBounds.share_ratio_max"
                class="input"
                data-testid="supply-share-ratio"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('supplyAdmin.settlement.shareRatioHint', { max: settlementBounds.share_ratio_max }) }}
              </p>
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.settlement.freezeHours') }}
              </label>
              <input
                v-model.number="settlementForm.freeze_hours"
                type="number"
                step="1"
                min="0"
                :max="settlementBounds.freeze_hours_max"
                class="input"
                data-testid="supply-freeze-hours"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('supplyAdmin.settlement.freezeHoursHint', { max: settlementBounds.freeze_hours_max }) }}
              </p>
            </div>

            <div class="flex items-center justify-between">
              <div>
                <label class="font-medium text-gray-900 dark:text-white">
                  {{ t('supplyAdmin.settlement.spendFromWalletFirst') }}
                </label>
                <p class="text-sm text-gray-500 dark:text-gray-400">
                  {{ t('supplyAdmin.settlement.spendFromWalletFirstHint') }}
                </p>
              </div>
              <Toggle v-model="settlementForm.spend_from_wallet_first" />
            </div>
          </div>

          <div class="flex justify-end">
            <button class="btn btn-primary" :disabled="savingSettlement" @click="saveSettlement">
              {{ t('supplyAdmin.settlement.save') }}
            </button>
          </div>
        </div>

        <!-- ===================== 池路由 ===================== -->
        <div class="card space-y-4 p-6">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supplyAdmin.pool.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.pool.description') }}</p>
          </div>

          <div class="flex items-center justify-between">
            <div>
              <label class="font-medium text-gray-900 dark:text-white">{{ t('supplyAdmin.pool.enabled') }}</label>
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.pool.enabledHint') }}</p>
            </div>
            <Toggle v-model="poolForm.enabled" data-testid="supply-pool-enabled" />
          </div>

          <div class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700">
            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.pool.supplyGroupId') }}
              </label>
              <input
                v-model.number="poolForm.supply_group_id"
                type="number"
                min="0"
                step="1"
                class="input"
                data-testid="supply-group-id"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.pool.supplyGroupIdHint') }}</p>
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.pool.overflowGroupId') }}
              </label>
              <input
                v-model.number="poolForm.overflow_group_id"
                type="number"
                min="0"
                step="1"
                class="input"
                data-testid="supply-overflow-group-id"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.pool.overflowGroupIdHint') }}</p>
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.pool.dailyOverflowLimit') }}
              </label>
              <input
                v-model.number="poolForm.daily_overflow_limit"
                type="number"
                min="0"
                step="1"
                class="input"
                data-testid="supply-daily-overflow-limit"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('supplyAdmin.pool.dailyOverflowLimitHint') }}
              </p>
              <!-- 配额与今日用量必须挨着看：单看「配额 500」说明不了任何事。 -->
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400" data-testid="supply-overflow-usage">
                {{
                  t('supplyAdmin.pool.overflowUsage', {
                    day: poolUsage.usage_day || '—',
                    used: poolUsage.overflow_used_today,
                    denied: poolUsage.overflow_denied_today,
                  })
                }}
              </p>
            </div>

            <div
              v-if="poolForm.enabled"
              class="rounded-lg border border-amber-200 bg-amber-50 p-3 dark:border-amber-900/40 dark:bg-amber-900/20"
            >
              <p class="text-xs text-amber-800 dark:text-amber-200">{{ t('supplyAdmin.pool.costWarning') }}</p>
            </div>
          </div>

          <div class="flex justify-end">
            <button class="btn btn-primary" :disabled="savingPool" @click="savePool">
              {{ t('supplyAdmin.pool.save') }}
            </button>
          </div>
        </div>

        <!-- ===================== 观察期 / 下线 ===================== -->
        <div class="card space-y-4 p-6">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supplyAdmin.probation.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.probation.description') }}</p>
          </div>

          <div class="flex items-center justify-between">
            <div>
              <label class="font-medium text-gray-900 dark:text-white">{{ t('supplyAdmin.probation.enabled') }}</label>
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.probation.enabledHint') }}</p>
            </div>
            <Toggle v-model="probationForm.enabled" data-testid="supply-probation-enabled" />
          </div>

          <div class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700">
            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.probation.minObservation') }}
              </label>
              <input
                v-model.number="probationForm.min_observation_minutes"
                type="number"
                step="1"
                min="0"
                :max="probationBounds.min_observation_minutes_max"
                class="input"
                data-testid="supply-probation-min-observation"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('supplyAdmin.probation.minObservationHint', { max: probationBounds.min_observation_minutes_max }) }}
              </p>
            </div>





            <!-- 探测间隔/达标次数/排空窗/探测模型是工程参数，已从面板收起：
                 默认值即推荐值，settings API 仍可手工调（字段与行为原样保留）。 -->
            <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900">
              <p class="text-xs text-gray-600 dark:text-gray-300">{{ t('supplyAdmin.probation.advancedTucked') }}</p>
            </div>

            <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900">
              <p class="text-xs text-gray-600 dark:text-gray-300">{{ t('supplyAdmin.probation.clampNotice') }}</p>
            </div>
          </div>

          <div class="flex justify-end">
            <button class="btn btn-primary" :disabled="savingProbation" @click="saveProbation">
              {{ t('supplyAdmin.probation.save') }}
            </button>
          </div>
        </div>

        <!-- ===================== 接入上限 ===================== -->
        <div class="card space-y-4 p-6">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supplyAdmin.onboarding.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.onboarding.description') }}</p>
          </div>

          <div class="space-y-4">
            <!-- 中转接入开关（M7）。放在这张卡而不是单独一张：它管的同样是
                 「谁能把什么挂进来」。红色提示写明信任模型的翻面——
                 开关一开，消费者的 prompt 就会流经供给者的服务器。 -->
            <div class="flex items-center justify-between">
              <div>
                <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('supplyAdmin.onboarding.relayEnabled') }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.onboarding.relayEnabledHint') }}</p>
                <p v-if="onboardingForm.relay_enabled" class="mt-1 text-xs text-amber-600 dark:text-amber-400">
                  {{ t('supplyAdmin.onboarding.relayTrustWarning') }}
                </p>
              </div>
              <Toggle v-model="onboardingForm.relay_enabled" data-testid="supply-onboarding-relay-enabled" />
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.onboarding.maxPerUser') }}
              </label>
              <input
                v-model.number="onboardingForm.max_accounts_per_user"
                type="number"
                step="1"
                min="0"
                :max="onboardingBounds.max_accounts_per_user_cap"
                class="input"
                data-testid="supply-onboarding-max-per-user"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('supplyAdmin.onboarding.maxPerUserHint', { max: onboardingBounds.max_accounts_per_user_cap }) }}
              </p>
            </div>


            <!-- 每 IP 那道闸开着时才提示。这段警示不是可有可无的礼貌话：
                 被它挡住的人看到的只是"挂不上号"，不会有人来报障。 -->
            <div
              v-if="onboardingForm.max_accounts_per_ip > 0"
              class="rounded-lg border border-amber-200 bg-amber-50 p-3 dark:border-amber-900/40 dark:bg-amber-900/20"
              data-testid="supply-onboarding-ip-warning"
            >
              <p class="text-xs text-amber-700 dark:text-amber-300">{{ t('supplyAdmin.onboarding.ipWarning') }}</p>
            </div>

            <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900">
              <p class="text-xs text-gray-600 dark:text-gray-300">{{ t('supplyAdmin.onboarding.clampNotice') }}</p>
            </div>
          </div>

          <div class="flex justify-end">
            <button class="btn btn-primary" :disabled="savingOnboarding" @click="saveOnboarding">
              {{ t('supplyAdmin.onboarding.save') }}
            </button>
          </div>
        </div>

        <!-- ===================== 供给者协议 ===================== -->
        <div class="card space-y-4 p-6">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supplyAdmin.agreement.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.agreement.description') }}</p>
          </div>

          <!-- 没发布 = 自助接入整个停着。这不是一个"建议填写"的字段，
               所以状态要摆在最上面，而不是等运营去猜为什么没人挂号。 -->
          <div
            class="rounded-lg border p-3"
            :class="
              agreementPublished
                ? 'border-emerald-200 bg-emerald-50 dark:border-emerald-900/40 dark:bg-emerald-900/20'
                : 'border-amber-200 bg-amber-50 dark:border-amber-900/40 dark:bg-amber-900/20'
            "
            data-testid="supply-agreement-status"
          >
            <p
              class="text-xs"
              :class="
                agreementPublished
                  ? 'text-emerald-700 dark:text-emerald-300'
                  : 'text-amber-700 dark:text-amber-300'
              "
            >
              {{ agreementPublished ? t('supplyAdmin.agreement.publishedNotice') : t('supplyAdmin.agreement.unpublishedNotice') }}
            </p>
          </div>

          <div class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700">
            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.agreement.version') }}
              </label>
              <input
                v-model.trim="agreementForm.version"
                type="text"
                class="input"
                :maxlength="agreementBounds.version_max_len"
                :placeholder="t('supplyAdmin.agreement.versionPlaceholder')"
                data-testid="supply-agreement-version"
              />
              <!-- 改版本号 = 让所有人重新点一次同意。这句话必须在输入框边上，
                   不能只写在文档里：错别字改一改就把全站供给者挡在门外了。 -->
              <p class="mt-1 text-xs text-amber-600 dark:text-amber-400">
                {{ t('supplyAdmin.agreement.versionHint') }}
              </p>
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.agreement.url') }}
              </label>
              <input
                v-model.trim="agreementForm.url"
                type="url"
                class="input"
                :maxlength="agreementBounds.url_max_len"
                placeholder="https://"
                data-testid="supply-agreement-url"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.agreement.urlHint') }}</p>
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.agreement.body') }}
              </label>
              <textarea
                v-model="agreementForm.body"
                class="input min-h-[12rem] font-mono text-xs"
                :maxlength="agreementBounds.body_max_len"
                :placeholder="t('supplyAdmin.agreement.bodyPlaceholder')"
                data-testid="supply-agreement-body"
              ></textarea>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('supplyAdmin.agreement.bodyHint', { max: agreementBounds.body_max_len }) }}
              </p>
            </div>

            <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900">
              <p class="text-xs text-gray-600 dark:text-gray-300">{{ t('supplyAdmin.agreement.rejectNotice') }}</p>
            </div>
          </div>

          <div class="flex justify-end">
            <button
              class="btn btn-primary"
              :disabled="savingAgreement"
              data-testid="supply-agreement-save"
              @click="saveAgreement"
            >
              {{ t('supplyAdmin.agreement.save') }}
            </button>
          </div>
        </div>

        <!-- ===================== 提现 ===================== -->
        <div class="card space-y-4 p-6">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supplyAdmin.withdrawal.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.withdrawal.description') }}</p>
          </div>

          <!-- 提现有两种静默失效，都在这块横幅里说出来（见 withdrawalStatus）：
               开着但没配渠道（供给者被硬拒），开着但没配收件人（申请进来没人知道）。
               后端把 available / notify_configured 算好下发，就是为了让这两个状态
               在这里被看见，而不是等供给者来报。 -->
          <div class="rounded-lg border p-3" :class="withdrawalStatus.box" data-testid="supply-withdrawal-status">
            <p class="text-xs" :class="withdrawalStatus.text">{{ t(withdrawalStatus.message) }}</p>
          </div>

          <div class="flex items-center justify-between border-t border-gray-100 pt-4 dark:border-dark-700">
            <div>
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('supplyAdmin.withdrawal.enabled') }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.withdrawal.enabledHint') }}</p>
            </div>
            <Toggle v-model="withdrawalForm.enabled" data-testid="supply-withdrawal-enabled" />
          </div>

          <div class="grid gap-4 sm:grid-cols-2">
            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.withdrawal.minAmount') }}
              </label>
              <input
                v-model.number="withdrawalForm.min_amount"
                type="number"
                min="0"
                step="0.01"
                :max="withdrawalBounds.min_amount_max"
                class="input"
                data-testid="supply-withdrawal-min-amount"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('supplyAdmin.withdrawal.minAmountHint', { max: withdrawalBounds.min_amount_max }) }}
              </p>
            </div>

          </div>

          <!-- M6b：渠道白名单已下线。「能选什么渠道」由链上金库的配置派生
               （见下面那张卡），这里编辑一份不再被读的名单只会造出
               「面板上看着配好了、实际全被忽略」的错觉。 -->
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900">
            <p class="text-xs text-gray-600 dark:text-gray-300" data-testid="supply-withdrawal-channels-retired">
              {{ t('supplyAdmin.withdrawal.channelsRetired') }}
            </p>
          </div>

          <div>
            <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('supplyAdmin.withdrawal.notifyEmails') }}
            </label>
            <!-- 同样一行一个。填错格式后端会**报错**而不是悄悄丢掉：悄悄丢掉的话
                 运营会看到"已保存"，然后一直等一封永远不会来的信。 -->
            <textarea
              v-model="withdrawalNotifyEmailsText"
              class="input min-h-[5rem] font-mono text-xs"
              :placeholder="t('supplyAdmin.withdrawal.notifyEmailsPlaceholder')"
              data-testid="supply-withdrawal-notify-emails"
            ></textarea>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{
                t('supplyAdmin.withdrawal.notifyEmailsHint', {
                  max: withdrawalBounds.notify_emails_max,
                  len: withdrawalBounds.notify_email_max_len,
                })
              }}
            </p>
          </div>

          <div>
            <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('supplyAdmin.withdrawal.notice') }}
            </label>
            <textarea
              v-model="withdrawalForm.notice"
              class="input min-h-[5rem]"
              :maxlength="withdrawalBounds.notice_max_len"
              :placeholder="t('supplyAdmin.withdrawal.noticePlaceholder')"
              data-testid="supply-withdrawal-notice"
            ></textarea>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('supplyAdmin.withdrawal.noticeHint', { max: withdrawalBounds.notice_max_len }) }}
            </p>
          </div>

          <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900">
            <p class="text-xs text-gray-600 dark:text-gray-300">{{ t('supplyAdmin.withdrawal.rejectNotice') }}</p>
          </div>

          <div class="flex justify-end">
            <button
              class="btn btn-primary"
              :disabled="savingWithdrawal"
              data-testid="supply-withdrawal-save"
              @click="saveWithdrawal"
            >
              {{ t('supplyAdmin.withdrawal.save') }}
            </button>
          </div>
        </div>

        <!-- ===================== 链上金库（M6） ===================== -->
        <!-- 第七张卡。私钥只进不出：输入框是 password 型、保存后回显的只有
             推导出的金库地址；留空保存 = 沿用已存的那把。 -->
        <div class="card space-y-4 p-6" data-testid="supply-payout-chain-card">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supplyAdmin.payoutChain.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.payoutChain.description') }}</p>
          </div>

          <!-- 装配状态横幅：它回答「此刻真的能打款吗」，与下面「存了什么」是两个问题。 -->
          <div class="rounded-lg border p-3" :class="payoutChainBanner.box" data-testid="supply-payout-chain-status">
            <p class="text-xs font-medium" :class="payoutChainBanner.text">{{ payoutChainBanner.headline }}</p>
            <p class="mt-1 break-all font-mono text-xs text-gray-500 dark:text-gray-400">{{ payoutChainStatus?.summary }}</p>
            <p v-if="payoutChainStatus?.error" class="mt-1 text-xs text-red-600 dark:text-red-400">
              {{ payoutChainStatus.error }}
            </p>
          </div>

          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('supplyAdmin.payoutChain.enabled') }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.payoutChain.enabledHint') }}</p>
            </div>
            <Toggle v-model="payoutChainForm.enabled" data-testid="supply-payout-chain-enabled" />
          </div>

          <div class="grid gap-4 sm:grid-cols-2">
            <div class="sm:col-span-2">
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.payoutChain.rpcUrl') }}
              </label>
              <input v-model="payoutChainForm.rpc_url" class="input font-mono text-xs" type="text"
                placeholder="https://bsc-dataseed.bnbchain.org" data-testid="supply-payout-chain-rpc" />
            </div>
            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.payoutChain.chainId') }}
              </label>
              <input v-model.number="payoutChainForm.chain_id" class="input" type="number" min="1"
                data-testid="supply-payout-chain-chain-id" />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.payoutChain.chainIdHint') }}</p>
            </div>
            <div class="sm:col-span-2">
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.payoutChain.tokenAddress') }}
              </label>
              <input v-model="payoutChainForm.token_address" class="input font-mono text-xs" type="text"
                placeholder="0x…" data-testid="supply-payout-chain-token-address" />
              <p class="mt-1 text-xs text-amber-600 dark:text-amber-400">{{ t('supplyAdmin.payoutChain.tokenAddressHint') }}</p>
            </div>
            <div class="sm:col-span-2">
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.payoutChain.disperseAddress') }}
              </label>
              <input v-model="payoutChainForm.disperse_address" class="input font-mono text-xs" type="text"
                placeholder="0x…" data-testid="supply-payout-chain-disperse" />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.payoutChain.disperseHint') }}</p>
            </div>
            <div class="sm:col-span-2">
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.payoutChain.signerKey') }}
              </label>
              <input
                v-model="payoutChainSignerInput"
                class="input font-mono text-xs"
                type="password"
                autocomplete="off"
                :placeholder="
                  payoutChainSignerConfigured
                    ? t('supplyAdmin.payoutChain.signerKeyKeep')
                    : t('supplyAdmin.payoutChain.signerKeyPlaceholder')
                "
                data-testid="supply-payout-chain-signer"
              />
              <p class="mt-1 text-xs text-amber-600 dark:text-amber-400">{{ t('supplyAdmin.payoutChain.signerKeyHint') }}</p>
              <p
                v-if="payoutChainStatus?.treasury"
                class="mt-1 break-all font-mono text-xs text-gray-500 dark:text-gray-400"
                data-testid="supply-payout-chain-treasury"
              >
                {{ t('supplyAdmin.payoutChain.treasury', { address: payoutChainStatus.treasury }) }}
              </p>
            </div>
          </div>

          <div class="flex justify-end gap-2">
            <button
              class="btn btn-secondary"
              :disabled="verifyingPayoutChain"
              data-testid="supply-payout-chain-verify"
              @click="verifyPayoutChain"
            >
              {{ verifyingPayoutChain ? t('supplyAdmin.payoutChain.verifying') : t('supplyAdmin.payoutChain.verify') }}
            </button>
            <button
              class="btn btn-primary"
              :disabled="savingPayoutChain"
              data-testid="supply-payout-chain-save"
              @click="savePayoutChain"
            >
              {{ t('supplyAdmin.payoutChain.save') }}
            </button>
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Toggle from '@/components/common/Toggle.vue'
import {
  adminSupplyMarketAPI,
  type SupplyMarketHealth,
  type SupplyPayoutChainPayload,
  type SupplyPayoutChainSettings,
  type SupplyPayoutChainStatus,
  type SupplyPoolPayload,
  type SupplyPoolSettings,
  type SupplyProbationPayload,
  type SupplyProbationSettings,
  type SupplyOnboardingPayload,
  type SupplyOnboardingSettings,
  type SupplyAgreementPayload,
  type SupplyAgreementSettings,
  type SupplyWithdrawalPayload,
  type SupplyWithdrawalSettings,
} from '@/api/admin/supplyMarket'
import { useAppStore } from '@/stores/app'
import { formatCurrency } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(true)
const savingSettlement = ref(false)
const savingPool = ref(false)
const savingProbation = ref(false)
const savingOnboarding = ref(false)
const savingAgreement = ref(false)
const savingWithdrawal = ref(false)

const settlementForm = reactive({
  enabled: false,
  share_ratio: 0.7,
  freeze_hours: 168,
  spend_from_wallet_first: false,
})

// 上下限来自后端，本地只留一份兜底初值（后端不可达时表单还得能渲染）。
// 抄死上限的下场是后端改了、前端还在按旧值拦。
const settlementBounds = reactive({ share_ratio_max: 1, freeze_hours_max: 24 * 90 })

const poolForm = reactive<SupplyPoolPayload>({
  enabled: false,
  supply_group_id: 0,
  overflow_group_id: 0,
  daily_overflow_limit: 0,
})

// 今日用量与表单分开存：它是只读读数，混进 poolForm 会让保存时不小心把它一起 PUT 上去。
const poolUsage = reactive({ usage_day: '', overflow_used_today: 0, overflow_denied_today: 0 })

function applyPoolUsage(settings: SupplyPoolSettings): void {
  poolUsage.usage_day = settings.usage_day ?? ''
  poolUsage.overflow_used_today = settings.overflow_used_today ?? 0
  poolUsage.overflow_denied_today = settings.overflow_denied_today ?? 0
}

// 默认值与后端 defaultSupplyProbationSettings 对齐，但只是"后端读不到时也能渲染"的兜底：
// 真值一律以 loadProbation() 拉回来的为准。
const probationForm = reactive<SupplyProbationPayload>({
  enabled: false,
  min_observation_minutes: 60,
  required_successes: 2,
  probe_interval_minutes: 15,
  probe_model: '',
  drain_window_minutes: 10,
})

const probationBounds = reactive({
  min_observation_minutes_max: 60 * 24 * 30,
  required_successes_max: 20,
  probe_interval_minutes_min: 5,
  probe_interval_minutes_max: 60 * 24,
  drain_window_minutes_max: 60 * 24,
})

// 接入上限的兜底初值与后端 DefaultSupplyOnboardingSettings 对齐：每人 5 个、每 IP 不限。
// 每 IP 那道默认是 0（关着）不是笔误——见 api 层那两段注释：这道闸开小了会静默地
// 挡住整个 NAT 后面的人，该由运营看过真实 IP 分布之后再打开。
const onboardingForm = reactive<SupplyOnboardingPayload>({
  // 中转接入默认关（M7）：开它是一个独立的信任模型决定，见卡片上的警告。
  relay_enabled: false,
  max_accounts_per_user: 5,
  max_accounts_per_ip: 0,
})

const onboardingBounds = reactive({
  max_accounts_per_user_cap: 100,
  max_accounts_per_ip_cap: 10000,
})

// 协议默认是空的 = 尚未发布 = 自助接入被拒。这个默认值是刻意的：开源部署第一次
// 打开供给池时会撞上它，而那正是运营该决定"我拿什么条款收别人订阅"的时刻。
const agreementForm = reactive<SupplyAgreementPayload>({
  version: '',
  url: '',
  body: '',
})

const agreementBounds = reactive({
  version_max_len: 64,
  url_max_len: 512,
  body_max_len: 100000,
})

// 用表单里的版本号算，而不是记住后端上次返回的 published：运营清空版本号的那一刻
// 就该看到"接入会停"的警示，而不是等保存完才看到。
const agreementPublished = computed(() => agreementForm.version.trim() !== '')

// 提现默认关着。这个默认值是刻意的：一个还没定好打款流程的部署，
// 不该先把提现按钮点亮，然后让第一个申请的人去当试验品。
const withdrawalForm = reactive<SupplyWithdrawalPayload>({
  enabled: false,
  min_amount: 100,
  max_pending: 3,
  notice: '',
  notify_emails: [],
})

const withdrawalBounds = reactive({
  min_amount_max: 1000000,
  max_pending_cap: 20,
  notice_max_len: 1000,
  notify_emails_max: 10,
  notify_email_max_len: 254,
})

/**
 * 渠道列表的文本形态：一行一个。
 *
 * 用换行而不是逗号分隔，是因为渠道名里真的会有逗号（"银行卡（境内，储蓄）"），
 * 而换行不会。getter 里不做 trim/过滤——那会让运营正在敲的空行当场消失，
 * 光标跳走；清理留到 setter，也就是真正要存的那一刻。
 */

/** 运营收件人的文本形态。同 withdrawalChannelsText：一行一个，清理留到 setter。 */
const withdrawalNotifyEmailsText = computed({
  get: () => withdrawalForm.notify_emails.join('\n'),
  set: (value: string) => {
    withdrawalForm.notify_emails = value
      .split('\n')
      .map(line => line.trim())
      .filter(line => line !== '')
  },
})

/**
 * 提现卡片顶部横幅的四个状态，按严重程度排序。
 *
 * 抽成 computed 而不是在模板里套四层三元：这块横幅要在 box/text/message 三处
 * 复述同一个判断，内联的话改一个状态得同步改三份，而漏掉一份的症状是「颜色是
 * 绿的但文案在报警」——比没有横幅更糟。
 *
 * noChannel 排在 noNotify 前面：前者供给者点提现当场被拒，后者功能还能用，
 * 只是没人被叫过来。两个都中时先说更硬的那个。
 */
const withdrawalStatus = computed(() => {
  if (!withdrawalForm.enabled) {
    return {
      box: 'border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-900',
      text: 'text-gray-600 dark:text-gray-300',
      message: 'supplyAdmin.withdrawal.closedNotice',
    }
  }
  // 「有没有能结算的渠道」看金库那张卡的 status，不再在这里判：
  // 判据只能有一个来源，两处各判一份迟早给出两个答案。
  if (withdrawalForm.notify_emails.length === 0) {
    return {
      box: 'border-amber-200 bg-amber-50 dark:border-amber-900/40 dark:bg-amber-900/20',
      text: 'text-amber-700 dark:text-amber-300',
      message: 'supplyAdmin.withdrawal.noNotifyNotice',
    }
  }
  return {
    box: 'border-emerald-200 bg-emerald-50 dark:border-emerald-900/40 dark:bg-emerald-900/20',
    text: 'text-emerald-700 dark:text-emerald-300',
    message: 'supplyAdmin.withdrawal.openNotice',
  }
})

async function loadSettlement(): Promise<void> {
  const settings = await adminSupplyMarketAPI.getSettlementSettings()
  settlementForm.enabled = settings.enabled
  settlementForm.share_ratio = settings.share_ratio
  settlementForm.freeze_hours = settings.freeze_hours
  settlementForm.spend_from_wallet_first = settings.spend_from_wallet_first
  if (settings.share_ratio_max > 0) settlementBounds.share_ratio_max = settings.share_ratio_max
  if (settings.freeze_hours_max > 0) settlementBounds.freeze_hours_max = settings.freeze_hours_max
}

async function loadPool(): Promise<void> {
  const settings = await adminSupplyMarketAPI.getPoolSettings()
  poolForm.enabled = settings.enabled
  poolForm.supply_group_id = settings.supply_group_id
  poolForm.overflow_group_id = settings.overflow_group_id
  poolForm.daily_overflow_limit = settings.daily_overflow_limit ?? 0
  applyPoolUsage(settings)
}

async function loadProbation(): Promise<void> {
  const settings = await adminSupplyMarketAPI.getProbationSettings()
  probationForm.enabled = settings.enabled
  probationForm.min_observation_minutes = settings.min_observation_minutes
  probationForm.required_successes = settings.required_successes
  probationForm.probe_interval_minutes = settings.probe_interval_minutes
  probationForm.probe_model = settings.probe_model
  probationForm.drain_window_minutes = settings.drain_window_minutes
  applyProbationBounds(settings)
}

/** 边界值只在后端确实给了正数时才覆盖本地兜底：字段缺失会被当成 0，把输入框锁死。 */
function applyProbationBounds(settings: SupplyProbationSettings): void {
  if (settings.min_observation_minutes_max > 0) {
    probationBounds.min_observation_minutes_max = settings.min_observation_minutes_max
  }
  if (settings.required_successes_max > 0) {
    probationBounds.required_successes_max = settings.required_successes_max
  }
  if (settings.probe_interval_minutes_min > 0) {
    probationBounds.probe_interval_minutes_min = settings.probe_interval_minutes_min
  }
  if (settings.probe_interval_minutes_max > 0) {
    probationBounds.probe_interval_minutes_max = settings.probe_interval_minutes_max
  }
  if (settings.drain_window_minutes_max > 0) {
    probationBounds.drain_window_minutes_max = settings.drain_window_minutes_max
  }
}

async function loadOnboarding(): Promise<void> {
  const settings = await adminSupplyMarketAPI.getOnboardingSettings()
  onboardingForm.relay_enabled = settings.relay_enabled
  onboardingForm.max_accounts_per_user = settings.max_accounts_per_user
  onboardingForm.max_accounts_per_ip = settings.max_accounts_per_ip
  applyOnboardingBounds(settings)
}

function applyOnboardingBounds(settings: SupplyOnboardingSettings): void {
  if (settings.max_accounts_per_user_cap > 0) {
    onboardingBounds.max_accounts_per_user_cap = settings.max_accounts_per_user_cap
  }
  if (settings.max_accounts_per_ip_cap > 0) {
    onboardingBounds.max_accounts_per_ip_cap = settings.max_accounts_per_ip_cap
  }
}

/**
 * 保存接入上限。后端**夹回区间而不是报错**（同观察期那一组），所以回填不是可选的：
 * 不写回来，运营会以为自己填的 500 生效了，而库里存的是 100。
 */
async function saveOnboarding(): Promise<void> {
  savingOnboarding.value = true
  try {
    const saved = await adminSupplyMarketAPI.updateOnboardingSettings({
      relay_enabled: onboardingForm.relay_enabled,
      max_accounts_per_user: onboardingForm.max_accounts_per_user,
      max_accounts_per_ip: onboardingForm.max_accounts_per_ip,
    })
    onboardingForm.max_accounts_per_user = saved.max_accounts_per_user
    onboardingForm.max_accounts_per_ip = saved.max_accounts_per_ip
    applyOnboardingBounds(saved)
    appStore.showSuccess(t('supplyAdmin.onboarding.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.saveFailed')))
  } finally {
    savingOnboarding.value = false
  }
}

async function loadAgreement(): Promise<void> {
  const settings = await adminSupplyMarketAPI.getAgreementSettings()
  agreementForm.version = settings.version ?? ''
  agreementForm.url = settings.url ?? ''
  agreementForm.body = settings.body ?? ''
  applyAgreementBounds(settings)
}

function applyAgreementBounds(settings: SupplyAgreementSettings): void {
  if (settings.version_max_len > 0) agreementBounds.version_max_len = settings.version_max_len
  if (settings.url_max_len > 0) agreementBounds.url_max_len = settings.url_max_len
  if (settings.body_max_len > 0) agreementBounds.body_max_len = settings.body_max_len
}

/**
 * 保存协议。
 *
 * 与观察期那一组的差别要留意：后端对越界值**报错**而不是夹回，所以这里没有
 * "被夹回的值要写回表单"的问题——保存成功就意味着存下的与填的一字不差。
 */
async function saveAgreement(): Promise<void> {
  savingAgreement.value = true
  try {
    const saved = await adminSupplyMarketAPI.updateAgreementSettings({
      version: agreementForm.version,
      url: agreementForm.url,
      body: agreementForm.body,
    })
    agreementForm.version = saved.version ?? ''
    agreementForm.url = saved.url ?? ''
    agreementForm.body = saved.body ?? ''
    applyAgreementBounds(saved)
    appStore.showSuccess(t('supplyAdmin.agreement.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.saveFailed')))
  } finally {
    savingAgreement.value = false
  }
}

async function saveSettlement(): Promise<void> {
  savingSettlement.value = true
  try {
    // 不在前端重复后端的区间校验：那边写路径已经对越界值报错，
    // 抄一份就是给同一条规则立两个源头。这里只负责把用户填的原样送过去。
    const saved = await adminSupplyMarketAPI.updateSettlementSettings({
      enabled: settlementForm.enabled,
      share_ratio: settlementForm.share_ratio,
      freeze_hours: settlementForm.freeze_hours,
      spend_from_wallet_first: settlementForm.spend_from_wallet_first,
    })
    // 回填后端返回值：写路径会 normalize，表单必须显示库里真正存下的数。
    settlementForm.share_ratio = saved.share_ratio
    settlementForm.freeze_hours = saved.freeze_hours
    appStore.showSuccess(t('supplyAdmin.settlement.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.saveFailed')))
  } finally {
    savingSettlement.value = false
  }
}

async function savePool(): Promise<void> {
  savingPool.value = true
  try {
    const saved = await adminSupplyMarketAPI.updatePoolSettings({
      enabled: poolForm.enabled,
      supply_group_id: poolForm.supply_group_id,
      overflow_group_id: poolForm.overflow_group_id,
      daily_overflow_limit: poolForm.daily_overflow_limit,
    })
    poolForm.enabled = saved.enabled
    poolForm.supply_group_id = saved.supply_group_id
    poolForm.overflow_group_id = saved.overflow_group_id
    poolForm.daily_overflow_limit = saved.daily_overflow_limit ?? 0
    // 保存的响应里带着最新用量，顺手刷新——否则保存完这块读数就停在打开页面那一刻。
    applyPoolUsage(saved)
    appStore.showSuccess(t('supplyAdmin.pool.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.saveFailed')))
  } finally {
    savingPool.value = false
  }
}

async function saveProbation(): Promise<void> {
  savingProbation.value = true
  try {
    const saved = await adminSupplyMarketAPI.updateProbationSettings({
      enabled: probationForm.enabled,
      min_observation_minutes: probationForm.min_observation_minutes,
      required_successes: probationForm.required_successes,
      probe_interval_minutes: probationForm.probe_interval_minutes,
      probe_model: probationForm.probe_model,
      drain_window_minutes: probationForm.drain_window_minutes,
    })
    // 这一组后端是**夹回区间而不是报错**（与结算参数刻意不同），所以回填不是可选的：
    // 不写回来，运营会以为自己填的 1 分钟生效了，而库里存的是 5。
    probationForm.enabled = saved.enabled
    probationForm.min_observation_minutes = saved.min_observation_minutes
    probationForm.required_successes = saved.required_successes
    probationForm.probe_interval_minutes = saved.probe_interval_minutes
    probationForm.probe_model = saved.probe_model
    probationForm.drain_window_minutes = saved.drain_window_minutes
    applyProbationBounds(saved)
    appStore.showSuccess(t('supplyAdmin.probation.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.saveFailed')))
  } finally {
    savingProbation.value = false
  }
}

async function loadWithdrawal(): Promise<void> {
  const settings = await adminSupplyMarketAPI.getWithdrawalSettings()
  applyWithdrawal(settings)
}

function applyWithdrawal(settings: SupplyWithdrawalSettings): void {
  withdrawalForm.enabled = settings.enabled
  withdrawalForm.min_amount = settings.min_amount
  withdrawalForm.max_pending = settings.max_pending
  withdrawalForm.notice = settings.notice ?? ''
  withdrawalForm.notify_emails = [...(settings.notify_emails ?? [])]
  if (settings.min_amount_max > 0) withdrawalBounds.min_amount_max = settings.min_amount_max
  if (settings.max_pending_cap > 0) withdrawalBounds.max_pending_cap = settings.max_pending_cap
  if (settings.notify_emails_max > 0) withdrawalBounds.notify_emails_max = settings.notify_emails_max
  if (settings.notify_email_max_len > 0) withdrawalBounds.notify_email_max_len = settings.notify_email_max_len
}

/**
 * 保存提现参数。
 *
 * 与协议那一组同脾气：后端对越界值**报错**而不是夹回，所以错误必须原样弹给运营。
 * 起提额被悄悄夹到上限的后果是所有人都提不了钱，而面板上看不出任何异常。
 *
 * 「开着却不给渠道」也会被后端拒——那个组合唯一的效果是让供给者点一个必定失败的
 * 按钮。想关掉入口就关 enabled，不要靠清空渠道来间接关。
 */
async function saveWithdrawal(): Promise<void> {
  savingWithdrawal.value = true
  try {
    const saved = await adminSupplyMarketAPI.updateWithdrawalSettings({
      enabled: withdrawalForm.enabled,
      min_amount: withdrawalForm.min_amount,
      max_pending: withdrawalForm.max_pending,
      notice: withdrawalForm.notice,
      notify_emails: withdrawalForm.notify_emails,
    })
    applyWithdrawal(saved)
    appStore.showSuccess(t('supplyAdmin.withdrawal.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.saveFailed')))
  } finally {
    savingWithdrawal.value = false
  }
}

// ============================================================================
// 链上金库（M6）
// ============================================================================

const savingPayoutChain = ref(false)
const verifyingPayoutChain = ref(false)
const payoutChainStatus = ref<SupplyPayoutChainStatus | null>(null)
const payoutChainSignerConfigured = ref(false)
// 私钥输入框独立于表单：它是只写字段，回显永远为空，
// 留空保存 = 后端沿用已存的那把。
const payoutChainSignerInput = ref('')
const payoutChainForm = reactive<Omit<SupplyPayoutChainPayload, 'signer_key'>>({
  enabled: false,
  rpc_url: '',
  token_address: '',
  token_symbol: 'USDT',
  disperse_address: '',
  chain_id: 56,
  confirmations: 3,
})

/** 状态横幅：live 且核过链 = 绿；live 但核不上/有错 = 琥珀；其余 = 灰。 */
const payoutChainBanner = computed(() => {
  const status = payoutChainStatus.value
  if (status?.mode === 'live' && status.chain_verified && !status.error) {
    return {
      box: 'border-emerald-200 bg-emerald-50 dark:border-emerald-900/40 dark:bg-emerald-900/20',
      text: 'text-emerald-700 dark:text-emerald-300',
      headline: t('supplyAdmin.payoutChain.statusLive'),
    }
  }
  if (status?.mode === 'live') {
    return {
      box: 'border-amber-200 bg-amber-50 dark:border-amber-900/40 dark:bg-amber-900/20',
      text: 'text-amber-700 dark:text-amber-300',
      headline: t('supplyAdmin.payoutChain.statusUnverified'),
    }
  }
  return {
    box: 'border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-900',
    text: 'text-gray-600 dark:text-gray-300',
    headline: t('supplyAdmin.payoutChain.statusOff'),
  }
})

function applyPayoutChain(settings: SupplyPayoutChainSettings): void {
  payoutChainForm.enabled = settings.enabled
  payoutChainForm.rpc_url = settings.rpc_url
  payoutChainForm.token_address = settings.token_address
  payoutChainForm.token_symbol = settings.token_symbol
  payoutChainForm.disperse_address = settings.disperse_address
  payoutChainForm.chain_id = settings.chain_id
  payoutChainForm.confirmations = settings.confirmations
  payoutChainSignerConfigured.value = settings.signer_configured
  payoutChainStatus.value = settings.status
}

async function loadPayoutChain(): Promise<void> {
  applyPayoutChain(await adminSupplyMarketAPI.getPayoutChainSettings())
}

/** 保存即热换。私钥输入框在成功后**立即清空**——它不该在页面上多留一秒。 */
async function savePayoutChain(): Promise<void> {
  savingPayoutChain.value = true
  try {
    const saved = await adminSupplyMarketAPI.updatePayoutChainSettings({
      ...payoutChainForm,
      signer_key: payoutChainSignerInput.value.trim(),
    })
    payoutChainSignerInput.value = ''
    applyPayoutChain(saved)
    appStore.showSuccess(t('supplyAdmin.payoutChain.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.saveFailed')))
  } finally {
    savingPayoutChain.value = false
  }
}

async function verifyPayoutChain(): Promise<void> {
  verifyingPayoutChain.value = true
  try {
    applyPayoutChain(await adminSupplyMarketAPI.verifyPayoutChain())
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.loadFailed')))
  } finally {
    verifyingPayoutChain.value = false
  }
}

// ============================================================================
// 定价与供给健康度（只读）
// ============================================================================

/** 与后端 supplyHealthMaxWindowDays（90）一致。更长的窗对定价没有意义：跨季度的
    混合读数已经说不清"现在的参数跑成什么样"。 */
const HEALTH_WINDOW_CHOICES = [7, 30, 90] as const

/**
 * 「低于预期」的线：折月产出 $1500。
 *
 * 这个数不是拍的，它是定价方案里那张调参表的最低一档（< $1500 → 倍率提到 0.22，
 * 否则供给者留不住）。标灰的意义就是把落在那一档的号一眼挑出来。
 */
const HEALTH_LOW_MONTHLY_OUTPUT = 1500

/**
 * 自检偏差的阈值：相对差 5%。
 *
 * 定得比"任何差异都报"松，是因为这块高亮在调价后会持续亮满一个窗口（窗口内新旧
 * 计费混合），而一个几乎常亮的提示等于没有提示。
 */
const HEALTH_DEVIATION_THRESHOLD = 0.05

// 偏差与常态两套配色抽成常量：模板里 box/text 各引用一次，写死会让改配色时漏掉一处，
// 而漏掉的症状是"框是琥珀色但字还是灰的"。
const HEALTH_DRIFT_BOX = 'border-amber-200 bg-amber-50 dark:border-amber-900/40 dark:bg-amber-900/20'
const HEALTH_DRIFT_TEXT = 'text-amber-700 dark:text-amber-300'
const HEALTH_PLAIN_BOX = 'border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-900'
const HEALTH_PLAIN_TEXT = 'text-gray-500 dark:text-gray-400'

const health = ref<SupplyMarketHealth | null>(null)
const healthLoading = ref(true)
const healthWindowDays = ref<number>(30)

/** 没有流水就没有可反推的比率——分母是它。 */
const healthHasTraffic = computed(() => (health.value?.list_value ?? 0) > 0)

function isLowOutput(monthlyOutput: number): boolean {
  return monthlyOutput < HEALTH_LOW_MONTHLY_OUTPUT
}

/** 后端理论上不会给出 NaN/Infinity，但这几个数全是除出来的，兜一层比在面板上写 NaN 好。 */
function formatRatio(value: number): string {
  return Number.isFinite(value) ? value.toFixed(3) : '—'
}

function formatPercent(value: number): string {
  return Number.isFinite(value) ? `${(value * 100).toFixed(1)}%` : '—'
}

interface HealthCheck {
  /** 有没有可对照的配置值。false 时不做任何偏差判断。 */
  comparable: boolean
  mismatched: boolean
  drift: number
}

/**
 * 一行自检的判定。
 *
 * configured ≤ 0 时**不比较**：倍率为 0 的含义是「没配供给池」而不是「配了 0 倍」，
 * 拿它当分母算出的相对差必然越界，于是那块高亮会在一个根本没开供给池的站点上
 * 长亮不灭——而那里没有任何东西需要修。分成同理（0 = 结算基本没在跑）。
 */
function healthCheck(effective: number, configured: number): HealthCheck {
  if (!Number.isFinite(effective) || !Number.isFinite(configured) || configured <= 0) {
    return { comparable: false, mismatched: false, drift: 0 }
  }
  const drift = Math.abs(effective - configured) / configured
  return { comparable: true, mismatched: drift > HEALTH_DEVIATION_THRESHOLD, drift }
}

const multiplierCheck = computed(() =>
  healthCheck(health.value?.effective_multiplier ?? 0, health.value?.configured_multiplier ?? 0)
)

const shareCheck = computed(() =>
  healthCheck(health.value?.effective_share ?? 0, health.value?.configured_share ?? 0)
)

/** 格内那一行短状态。长成因只说一次（见模板里那块琥珀横幅），这里只回答"差了多少"。 */
function checkStateLabel(check: HealthCheck, noBaselineKey: string): string {
  if (!check.comparable) return t(noBaselineKey)
  if (!check.mismatched) return t('supplyAdmin.health.selfCheck.aligned')
  return t('supplyAdmin.health.selfCheck.drift', { drift: formatPercent(check.drift) })
}

/**
 * 拉健康度读数。
 *
 * 失败时把 health 清成 null 而不是留着上一个窗口的数：切窗口失败后继续显示旧数，
 * 读者会以为那是新窗口的读数，而这张卡的全部用途就是照着它调价。
 */
async function loadHealth(): Promise<void> {
  healthLoading.value = true
  try {
    health.value = await adminSupplyMarketAPI.getHealth(healthWindowDays.value)
  } catch (error) {
    health.value = null
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.health.loadFailed')))
  } finally {
    healthLoading.value = false
  }
}

onMounted(async () => {
  // 健康度与七组配置分开加载：它是只读读数，服务没接线（后端 503）时不该把整页
  // 配置一起挡在加载失败上——那七组参数照样改得动。
  void loadHealth()

  try {
    await Promise.all([
      loadSettlement(),
      loadPool(),
      loadProbation(),
      loadOnboarding(),
      loadPayoutChain(),
      loadAgreement(),
      loadWithdrawal(),
    ])
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.loadFailed')))
  } finally {
    loading.value = false
  }
})
</script>
