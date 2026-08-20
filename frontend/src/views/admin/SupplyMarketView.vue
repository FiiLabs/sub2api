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

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.probation.requiredSuccesses') }}
              </label>
              <input
                v-model.number="probationForm.required_successes"
                type="number"
                step="1"
                min="1"
                :max="probationBounds.required_successes_max"
                class="input"
                data-testid="supply-probation-required-successes"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('supplyAdmin.probation.requiredSuccessesHint', { max: probationBounds.required_successes_max }) }}
              </p>
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.probation.probeInterval') }}
              </label>
              <input
                v-model.number="probationForm.probe_interval_minutes"
                type="number"
                step="1"
                :min="probationBounds.probe_interval_minutes_min"
                :max="probationBounds.probe_interval_minutes_max"
                class="input"
                data-testid="supply-probation-probe-interval"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{
                  t('supplyAdmin.probation.probeIntervalHint', {
                    min: probationBounds.probe_interval_minutes_min,
                    max: probationBounds.probe_interval_minutes_max,
                  })
                }}
              </p>
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.probation.probeModel') }}
              </label>
              <input
                v-model.trim="probationForm.probe_model"
                type="text"
                class="input"
                :placeholder="t('supplyAdmin.probation.probeModelPlaceholder')"
                data-testid="supply-probation-probe-model"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('supplyAdmin.probation.probeModelHint') }}</p>
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.probation.drainWindow') }}
              </label>
              <input
                v-model.number="probationForm.drain_window_minutes"
                type="number"
                step="1"
                min="0"
                :max="probationBounds.drain_window_minutes_max"
                class="input"
                data-testid="supply-probation-drain-window"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('supplyAdmin.probation.drainWindowHint', { max: probationBounds.drain_window_minutes_max }) }}
              </p>
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

          <!-- 「开着但一个渠道都没配」是一种静默失效：面板显示已开启，供给者点提现
               被硬拒。后端把 available 算好下发，就是为了让这个状态在这里被看见，
               而不是等供给者来报。 -->
          <div
            class="rounded-lg border p-3"
            :class="
              withdrawalForm.enabled && withdrawalForm.channels.length === 0
                ? 'border-red-200 bg-red-50 dark:border-red-900/40 dark:bg-red-900/20'
                : withdrawalForm.enabled
                  ? 'border-emerald-200 bg-emerald-50 dark:border-emerald-900/40 dark:bg-emerald-900/20'
                  : 'border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-900'
            "
            data-testid="supply-withdrawal-status"
          >
            <p
              class="text-xs"
              :class="
                withdrawalForm.enabled && withdrawalForm.channels.length === 0
                  ? 'text-red-700 dark:text-red-300'
                  : withdrawalForm.enabled
                    ? 'text-emerald-700 dark:text-emerald-300'
                    : 'text-gray-600 dark:text-gray-300'
              "
            >
              {{
                withdrawalForm.enabled && withdrawalForm.channels.length === 0
                  ? t('supplyAdmin.withdrawal.noChannelNotice')
                  : withdrawalForm.enabled
                    ? t('supplyAdmin.withdrawal.openNotice')
                    : t('supplyAdmin.withdrawal.closedNotice')
              }}
            </p>
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

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('supplyAdmin.withdrawal.maxPending') }}
              </label>
              <input
                v-model.number="withdrawalForm.max_pending"
                type="number"
                min="1"
                :max="withdrawalBounds.max_pending_cap"
                class="input"
                data-testid="supply-withdrawal-max-pending"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('supplyAdmin.withdrawal.maxPendingHint', { max: withdrawalBounds.max_pending_cap }) }}
              </p>
            </div>
          </div>

          <div>
            <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t('supplyAdmin.withdrawal.channels') }}
            </label>
            <!-- 一行一个渠道，而不是逗号分隔：渠道名里可能有逗号（"银行卡（境内，储蓄）"），
                 而换行不会。存的是**原样字符串**，供给者提交时按完全相等匹配。 -->
            <textarea
              v-model="withdrawalChannelsText"
              class="input min-h-[6rem] font-mono text-xs"
              :placeholder="t('supplyAdmin.withdrawal.channelsPlaceholder')"
              data-testid="supply-withdrawal-channels"
            ></textarea>
            <p class="mt-1 text-xs text-amber-600 dark:text-amber-400">
              {{
                t('supplyAdmin.withdrawal.channelsHint', {
                  max: withdrawalBounds.channels_max,
                  len: withdrawalBounds.channel_max_len,
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
  type SupplyPoolPayload,
  type SupplyPoolSettings,
  type SupplyProbationPayload,
  type SupplyProbationSettings,
  type SupplyAgreementPayload,
  type SupplyAgreementSettings,
  type SupplyWithdrawalPayload,
  type SupplyWithdrawalSettings,
} from '@/api/admin/supplyMarket'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(true)
const savingSettlement = ref(false)
const savingPool = ref(false)
const savingProbation = ref(false)
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
  channels: [],
  notice: '',
})

const withdrawalBounds = reactive({
  min_amount_max: 1000000,
  max_pending_cap: 20,
  channels_max: 20,
  channel_max_len: 64,
  notice_max_len: 1000,
})

/**
 * 渠道列表的文本形态：一行一个。
 *
 * 用换行而不是逗号分隔，是因为渠道名里真的会有逗号（"银行卡（境内，储蓄）"），
 * 而换行不会。getter 里不做 trim/过滤——那会让运营正在敲的空行当场消失，
 * 光标跳走；清理留到 setter，也就是真正要存的那一刻。
 */
const withdrawalChannelsText = computed({
  get: () => withdrawalForm.channels.join('\n'),
  set: (value: string) => {
    withdrawalForm.channels = value
      .split('\n')
      .map(line => line.trim())
      .filter(line => line !== '')
  },
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
  withdrawalForm.channels = [...(settings.channels ?? [])]
  withdrawalForm.notice = settings.notice ?? ''
  if (settings.min_amount_max > 0) withdrawalBounds.min_amount_max = settings.min_amount_max
  if (settings.max_pending_cap > 0) withdrawalBounds.max_pending_cap = settings.max_pending_cap
  if (settings.channels_max > 0) withdrawalBounds.channels_max = settings.channels_max
  if (settings.channel_max_len > 0) withdrawalBounds.channel_max_len = settings.channel_max_len
  if (settings.notice_max_len > 0) withdrawalBounds.notice_max_len = settings.notice_max_len
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
      channels: withdrawalForm.channels,
      notice: withdrawalForm.notice,
    })
    applyWithdrawal(saved)
    appStore.showSuccess(t('supplyAdmin.withdrawal.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.saveFailed')))
  } finally {
    savingWithdrawal.value = false
  }
}

onMounted(async () => {
  try {
    await Promise.all([loadSettlement(), loadPool(), loadProbation(), loadAgreement(), loadWithdrawal()])
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.loadFailed')))
  } finally {
    loading.value = false
  }
})
</script>
