/**
 * APEXONE-EXT: 双边市场——供给侧功能开关的共享状态。
 *
 * 存在的唯一理由是侧边栏：菜单项要不要显示取决于 `/user/supply/status`，
 * 那是一个**按用户**的后端调用，而上游的 featureFlags 注册表只吃 public settings。
 * 走那条路要改 11 处上游文件（见 utils/featureFlags.ts 顶部的清单）才能加一个
 * 全站开关，而我们要的是"这个部署有没有配供给池"——一次请求就能答的问题。
 *
 * 状态只拉一次并缓存：侧边栏每次路由切换都会重挂载，不缓存就是每跳一个页面
 * 打一次后端。失败按"未开放"处理（fail-closed）——把入口显示出来、点进去才发现
 * 功能没开，比不显示更糟。
 */

import { defineStore } from 'pinia'
import { ref } from 'vue'
import { supplyAPI, type SupplyStatus } from '@/api/supply'
import { useAuthStore } from './auth'

export const useSupplyStore = defineStore('supply', () => {
  const enabled = ref(false)
  const settlementEnabled = ref(false)
  const loaded = ref(false)

  // 缓存是按用户的：换个人登录（同一个 SPA 会话内，不刷新页面）必须重新问一次，
  // 否则上一个人的开关会留在侧边栏上。这里主动记住 userId 而不是让 auth store
  // 在登出时来调 reset()——那要改上游的 clearAuth，而依赖方向反过来就不用。
  const statusUserID = ref<number | null>(null)

  // 并发去重：侧边栏和页面可能同时触发首次加载，没有这个就会打两次。
  let inflight: Promise<void> | null = null

  function apply(status: SupplyStatus | null): void {
    enabled.value = status?.enabled === true
    settlementEnabled.value = status?.settlement_enabled === true
  }

  /** 拉一次状态；同一个用户已经拉过就直接返回。用 refresh() 强制重拉。 */
  async function ensureStatus(): Promise<void> {
    const currentUserID = useAuthStore().user?.id ?? null
    if (loaded.value && statusUserID.value === currentUserID) return
    if (inflight) return inflight
    statusUserID.value = currentUserID
    inflight = (async () => {
      try {
        apply(await supplyAPI.getStatus())
      } catch {
        // 未登录、后端没这个路由（旧版本）、网络故障——都按"没开放"处理。
        apply(null)
      } finally {
        loaded.value = true
        inflight = null
      }
    })()
    return inflight
  }

  async function refresh(): Promise<void> {
    loaded.value = false
    inflight = null
    await ensureStatus()
  }

  /** 退出登录时调用：开关是按用户的，换个人登录必须重新问一次。 */
  function reset(): void {
    enabled.value = false
    settlementEnabled.value = false
    loaded.value = false
    statusUserID.value = null
    inflight = null
  }

  return { enabled, settlementEnabled, loaded, ensureStatus, refresh, reset }
})
