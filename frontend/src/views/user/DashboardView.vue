<template>
  <AppLayout>
    <div class="space-y-6">
      <!--
        APEXONE-EXT: 双边市场——模式切换器已移到侧边栏常驻（AppSidebar 顶部）。
        原来放在这里是因为它是当时唯一的出口；现在侧栏顶部那个是全局的、任何页面都在，
        再在 Dashboard 顶上放一个就重复了。切换逻辑不变，仍是 supplyStore.setMode。
      -->

      <div v-if="loading" class="flex items-center justify-center py-12"><LoadingSpinner /></div>
      <template v-else-if="stats">
        <!-- 只有顶部这一格随模式换。下面的趋势图和最近用量两种身份都看得懂，
             为了对称把它们也换掉，等于凭空多造两个供给侧视图。 -->
        <SupplyDashboardStats v-if="isSharing" :wallet="supplyWallet" :account-count="supplyAccountCount" :schedulable-count="supplySchedulableCount" />
        <UserDashboardStats v-else :stats="stats" :balance="user?.balance || 0" :is-simple="authStore.isSimpleMode" :platform-quotas="platformQuotas" />
        <!-- 一个号都没挂的人，上面那一格必然是四个 0——那不是"我今天没赚到"，
             是"我还没开始"。这两句话之间的差别只能由这张卡来说。
             接入过的人不显示：对他们这三步是纯占位，会把趋势图挤下去一屏。
             判断带上 supplyLoaded，是因为 supplyAccountCount 的初值就是 0，
             不等接口回来的话，已接入的人每次进控制台都会先闪一下这张卡。 -->
        <SupplyStartGuide v-if="isSharing && supplyLoaded && supplyAccountCount === 0" />
        <UserDashboardCharts v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="loadingCharts" :trend="trendData" :models="modelStats" @dateRangeChange="loadCharts" @granularityChange="loadCharts" @refresh="refreshAll" />
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
          <div class="lg:col-span-2"><UserDashboardRecentUsage :data="recentUsage" :loading="loadingUsage" /></div>
          <div class="lg:col-span-1">
            <SupplyQuickActions v-if="isSharing" />
            <UserDashboardQuickActions v-else />
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'; import { useAuthStore } from '@/stores/auth'; import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import AppLayout from '@/components/layout/AppLayout.vue'; import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'; import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'; import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import SupplyDashboardStats from '@/components/user/dashboard/SupplyDashboardStats.vue'; import SupplyQuickActions from '@/components/user/dashboard/SupplyQuickActions.vue'; import SupplyStartGuide from '@/components/user/dashboard/SupplyStartGuide.vue'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'
import { getMyPlatformQuotas } from '@/api/user'
import { formatDateLocalInput } from '@/utils/format'
import { useSupplyStore } from '@/stores/supply'; import { supplyAPI, type SupplyWallet } from '@/api/supply'

const authStore = useAuthStore(); const user = computed(() => authStore.user)
const stats = ref<UserStatsType | null>(null); const loading = ref(false); const loadingUsage = ref(false); const loadingCharts = ref(false)
const trendData = ref<TrendDataPoint[]>([]); const modelStats = ref<ModelStat[]>([]); const recentUsage = ref<UsageLog[]>([])
const platformQuotas = ref<PlatformQuotaItem[] | null>(null)

const startDate = ref(formatDateLocalInput(new Date(Date.now() - 6 * 86400000))); const endDate = ref(formatDateLocalInput(new Date())); const granularity = ref('day')

// APEXONE-EXT: 供给侧那三个数。只在共享模式下拉——消费者占绝大多数，
// 让每个人打开控制台都多打两个他永远看不到的接口是纯粹的浪费。
const supplyStore = useSupplyStore()
const isSharing = computed(() => supplyStore.mode === 'sharing')
const supplyWallet = ref<SupplyWallet | null>(null)
const supplyAccountCount = ref(0)
const supplySchedulableCount = ref(0)
// "还没问过" 与 "问过了，答案是零个" 在界面上是两件事：只有后者才该看到新手引导。
// 所以它只在成功那条路径上置位——接口失败时账号数停在初值 0，此刻若拿它当
// "这人还没接入"，就会对着一个挂了三个号的老供给者讲"三步开始赚钱"。
const supplyLoaded = ref(false)

const loadStats = async () => { loading.value = true; try { await authStore.refreshUser(); stats.value = await usageAPI.getDashboardStats() } catch (error) { console.error('Failed to load dashboard stats:', error) } finally { loading.value = false } }
const loadCharts = async () => { loadingCharts.value = true; try { const res = await Promise.all([usageAPI.getDashboardTrend({ start_date: startDate.value, end_date: endDate.value, granularity: granularity.value as any }), usageAPI.getDashboardModels({ start_date: startDate.value, end_date: endDate.value })]); trendData.value = res[0].trend || []; modelStats.value = res[1].models || [] } catch (error) { console.error('Failed to load charts:', error) } finally { loadingCharts.value = false } }
const loadRecent = async () => { loadingUsage.value = true; try { const res = await usageAPI.getByDateRange(startDate.value, endDate.value); recentUsage.value = res.items.slice(0, 5) } catch (error) { console.error('Failed to load recent usage:', error) } finally { loadingUsage.value = false } }
const loadPlatformQuotas = async () => { try { const data = await getMyPlatformQuotas(); platformQuotas.value = data.platform_quotas ?? [] } catch (error) { console.warn('Failed to load platform quotas:', error); platformQuotas.value = [] } }
// 两个接口一起失败也只是三个数显示 0：这一格是概览，报错弹窗留给 /supply 那一页，
// 用户在那里才有可做的动作。
//
// 去重是必要的：挂载、状态回来、用户当场切模式三条路都会走到这里，
// 而它们在同一屏里可能挨着发生。
let supplyInflight: Promise<void> | null = null
const loadSupply = (): Promise<void> => {
  if (supplyInflight) return supplyInflight
  supplyInflight = (async () => {
    try {
      const [wallet, accounts] = await Promise.all([supplyAPI.getWallet(), supplyAPI.listAccounts()])
      supplyWallet.value = wallet; supplyAccountCount.value = accounts.length; supplySchedulableCount.value = accounts.filter((a) => a.schedulable).length
      supplyLoaded.value = true
    } catch (error) {
      console.warn('Failed to load supply overview:', error)
    } finally {
      supplyInflight = null
    }
  })()
  return supplyInflight
}
const refreshAll = () => { loadStats(); loadCharts(); loadRecent(); loadPlatformQuotas(); if (isSharing.value) loadSupply() }

// 模式可能在状态回来之后才变成 sharing（自动判定要等 account_count 回来），
// 也可能是用户当场切过来的。两种都要拉一次，否则切过去是一屏 0——
// 而"0 收益"这句话被误信一次，代价比多一个请求大得多。
watch(isSharing, (sharing) => { if (sharing) loadSupply() })

onMounted(() => { refreshAll(); void supplyStore.ensureStatus() })
</script>
