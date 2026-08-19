<!--
  APEXONE-EXT: 双边市场——管理端配置页。

  单起一页而不是往 SettingsView.vue 里加两个 section：那个文件已经近九千行，
  是上游合并最痛的一处。独立页只需要在路由表和侧边栏各加一条。

  两组配置各自一个保存按钮，与后端两对端点一一对应：改分成比例和改兜底池
  是两件不同的事，共用一次提交会让审计日志分不清谁改了什么。
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
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Toggle from '@/components/common/Toggle.vue'
import { adminSupplyMarketAPI, type SupplyPoolSettings } from '@/api/admin/supplyMarket'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(true)
const savingSettlement = ref(false)
const savingPool = ref(false)

const settlementForm = reactive({
  enabled: false,
  share_ratio: 0.7,
  freeze_hours: 168,
  spend_from_wallet_first: false,
})

// 上下限来自后端，本地只留一份兜底初值（后端不可达时表单还得能渲染）。
// 抄死上限的下场是后端改了、前端还在按旧值拦。
const settlementBounds = reactive({ share_ratio_max: 1, freeze_hours_max: 24 * 90 })

const poolForm = reactive<SupplyPoolSettings>({
  enabled: false,
  supply_group_id: 0,
  overflow_group_id: 0,
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
    })
    poolForm.enabled = saved.enabled
    poolForm.supply_group_id = saved.supply_group_id
    poolForm.overflow_group_id = saved.overflow_group_id
    appStore.showSuccess(t('supplyAdmin.pool.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.saveFailed')))
  } finally {
    savingPool.value = false
  }
}

onMounted(async () => {
  try {
    await Promise.all([loadSettlement(), loadPool()])
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supplyAdmin.error.loadFailed')))
  } finally {
    loading.value = false
  }
})
</script>
