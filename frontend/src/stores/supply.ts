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
import { computed, ref } from 'vue'
import { supplyAPI, type SupplyStatus } from '@/api/supply'
import { useAuthStore } from './auth'

/**
 * 控制台模式。两侧人群看的是两套完全不同的数：消费者关心花了多少、还剩多少，
 * 共享者关心赚了多少、什么时候能提。同一套仪表盘同时服务两者，结果是两边都看到
 * 一半跟自己无关的卡片。
 */
export type ConsoleMode = 'usage' | 'sharing'

// localStorage 的 key 按用户分。同一个 SPA 会话里换人登录是会发生的（statusUserID
// 这个字段本身就是为此存在的），共用一个 key 的话上一个人手动选的「共享模式」
// 会直接落到下一个人的控制台上——而后者可能一个供给账号都没有，看到的是一整屏 0。
const MODE_KEY_PREFIX = 'apexone.consoleMode.'

// 待应用意图：访客在首页点「我要用 AI / 我有闲置额度」时还没登录，按用户分的
// key 存不下来（statusUserID 是 null）。先记在这个**不按用户分**的临时 key 里，
// 登录后 ensureStatus 拿到真实 userID 时把它落到那个人名下、随即清掉。
// 这样"从首页选了入口"这个意图能穿过登录活到登录后。
const PENDING_MODE_KEY = 'apexone.pendingConsoleMode'

function modeStorageKey(userID: number | null): string {
  return `${MODE_KEY_PREFIX}${userID ?? 'anonymous'}`
}

function readPendingMode(): ConsoleMode | null {
  try {
    const raw = localStorage.getItem(PENDING_MODE_KEY)
    return raw === 'usage' || raw === 'sharing' ? raw : null
  } catch {
    return null
  }
}

function clearPendingMode(): void {
  try {
    localStorage.removeItem(PENDING_MODE_KEY)
  } catch {
    /* 存储不可用：意图丢失只是回到自动判定，不该报错 */
  }
}

function readStoredMode(userID: number | null): ConsoleMode | null {
  try {
    const raw = localStorage.getItem(modeStorageKey(userID))
    return raw === 'usage' || raw === 'sharing' ? raw : null
  } catch {
    // 隐私模式 / 存储被禁用。手动选择丢失只是回到自动判定，不该让控制台白屏。
    return null
  }
}

export const useSupplyStore = defineStore('supply', () => {
  const enabled = ref(false)
  const settlementEnabled = ref(false)
  const accountCount = ref(0)
  // 0 = 后端没给出比例，界面据此**不显示**而不是显示 0%。
  const shareRatio = ref(0)
  const loaded = ref(false)

  // 缓存是按用户的：换个人登录（同一个 SPA 会话内，不刷新页面）必须重新问一次，
  // 否则上一个人的开关会留在侧边栏上。这里主动记住 userId 而不是让 auth store
  // 在登出时来调 reset()——那要改上游的 clearAuth，而依赖方向反过来就不用。
  const statusUserID = ref<number | null>(null)

  // 首页未登录时选的意图（响应式）。用来在首页把「当前选中」的入口高亮固定住——
  // 点一下 setPendingMode 就同步更新，选中框立刻落到那个入口上，且能从一个换到另一个。
  // 登录后被 ensureStatus 消化并清空。
  const pendingMode = ref<ConsoleMode | null>(readPendingMode())

  // 手动选择的模式，null = 没选过、走自动判定。
  //
  // 刻意**不做**首次登录的二选一弹窗：绝大多数人只属于一侧，而弹窗会拦在他和
  // 他真正要做的事之间，让他为一个自己还不理解的概念做决定。有号就是共享者、
  // 没号就是消费者，这个判断在 99% 的情况下是对的；剩下 1% 用切换器改一次即可。
  const manualMode = ref<ConsoleMode | null>(null)

  // 并发去重：侧边栏和页面可能同时触发首次加载，没有这个就会打两次。
  let inflight: Promise<void> | null = null

  function apply(status: SupplyStatus | null): void {
    enabled.value = status?.enabled === true
    settlementEnabled.value = status?.settlement_enabled === true
    accountCount.value = typeof status?.account_count === 'number' ? status.account_count : 0
    // 只认 (0, 1] 的比例。后端给 0（读不到设置）、负数或大于 1 的脏值时一律归零 →
    // 界面不显示。对一个正在决定要不要挂号的人报一个错的分成比例，比不报更糟。
    const ratio = status?.share_ratio
    shareRatio.value = typeof ratio === 'number' && ratio > 0 && ratio <= 1 ? ratio : 0
  }

  /** 拉一次状态；同一个用户已经拉过就直接返回。用 refresh() 强制重拉。 */
  async function ensureStatus(): Promise<void> {
    const currentUserID = useAuthStore().user?.id ?? null
    if (loaded.value && statusUserID.value === currentUserID) return
    if (inflight) return inflight
    statusUserID.value = currentUserID
    // 同步读，不等请求回来：模式判定的第一优先级是"他自己选过什么"，
    // 让这一条晚一个 tick 生效会让切过模式的人每次进控制台先闪一下另一种。
    manualMode.value = readStoredMode(currentUserID)
    // 登录后消化首页选的意图：它压过历史选择（这次他明确点了另一侧），
    // 落到本人名下后清掉。只在已登录（userID 非空）时消化——否则无处安放。
    if (currentUserID != null) {
      const pending = readPendingMode()
      if (pending) {
        manualMode.value = pending
        try {
          localStorage.setItem(modeStorageKey(currentUserID), pending)
        } catch {
          /* 存储不可用：本次会话内内存里生效即可 */
        }
        clearPendingMode()
        pendingMode.value = null
      }
    }
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

  /**
   * 当前该显示哪套控制台。优先级从高到低：
   *
   *   1. 供给功能关着 → 恒为使用模式。此时共享模式下的每个入口都会撞到一个
   *      "尚未开放"的页面，能进去比进不去更糟。
   *   2. 他自己选过 → 用他选的。人工判断永远压过自动判断。
   *   3. 名下有供给账号 → 共享模式。挂了号的人打开控制台想看的是收益。
   *   4. 其余 → 使用模式。
   */
  const mode = computed<ConsoleMode>(() => {
    if (!enabled.value) return 'usage'
    if (manualMode.value) return manualMode.value
    return accountCount.value > 0 ? 'sharing' : 'usage'
  })

  /**
   * 切换器该不该出现。与 mode 分开：mode 在功能关着时也有值（usage），
   * 但那时候「切到共享模式」是一个通往空页面的按钮。
   */
  const canSwitchMode = computed(() => enabled.value)

  /** 记下手动选择。写盘失败不影响本次会话内的切换生效。 */
  function setMode(next: ConsoleMode): void {
    manualMode.value = next
    try {
      localStorage.setItem(modeStorageKey(statusUserID.value), next)
    } catch {
      /* 存储不可用时只在内存里生效，下次进来回到自动判定 */
    }
  }

  /** 记下「登录后想进哪套」的意图，给首页未登录入口用。登录后由 ensureStatus 消化。 */
  function setPendingMode(next: ConsoleMode): void {
    pendingMode.value = next  // 同步更新，首页选中框立刻固定到这个入口
    try {
      localStorage.setItem(PENDING_MODE_KEY, next)
    } catch {
      /* 存储不可用：这次分流意图丢失，回到自动判定 */
    }
  }

  /**
   * 首页入口该把哪个标成「已选中」。
   *   已登录 → 用生效中的 mode（他实际在哪套体验里）；
   *   未登录 → 用 pendingMode（他刚在首页点的那个），没点过就是 null（都不高亮）。
   */
  const chosenEntry = computed<ConsoleMode | null>(() => {
    if (useAuthStore().user?.id != null) return mode.value
    return pendingMode.value
  })

  /** 退出登录时调用：开关是按用户的，换个人登录必须重新问一次。 */
  function reset(): void {
    enabled.value = false
    settlementEnabled.value = false
    accountCount.value = 0
    shareRatio.value = 0
    loaded.value = false
    statusUserID.value = null
    // 只清内存里的选择，不清 localStorage：那条记录是**上一个用户的**，
    // 他下次登录回来时应该还在。
    manualMode.value = null
    inflight = null
  }

  return {
    enabled,
    settlementEnabled,
    accountCount,
    shareRatio,
    loaded,
    mode,
    canSwitchMode,
    chosenEntry,
    setMode,
    setPendingMode,
    ensureStatus,
    refresh,
    reset,
  }
})
