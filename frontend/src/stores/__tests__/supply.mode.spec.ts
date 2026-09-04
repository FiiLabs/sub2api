/**
 * APEXONE-EXT: 控制台模式的判定。
 *
 * 这个 spec 盯的是四条优先级和一条隔离：
 *   1. 功能关着一律 usage（共享模式下的每个入口都会撞到"尚未开放"）；
 *   2. 手动选过就用他选的（人工判断压过自动判断）；
 *   3. 有号 = sharing；4. 没号 = usage；
 *   5. localStorage 按用户分——同一个 SPA 会话里换人登录是会发生的，
 *      共用一个 key 就是把上一个人的身份留给下一个人。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

const mockGetStatus = vi.fn()

vi.mock('@/api/supply', () => ({
  supplyAPI: {
    getStatus: (...args: unknown[]) => mockGetStatus(...args),
  },
}))

const currentUser = { value: null as { id: number } | null }

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ get user() { return currentUser.value } }),
}))

import { useSupplyStore } from '@/stores/supply'

describe('supply store console mode', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockGetStatus.mockReset()
    localStorage.clear()
    currentUser.value = { id: 1 }
  })

  it('reads accountCount from the status payload, defaulting to 0 on older backends', async () => {
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 3 })
    const store = useSupplyStore()
    await store.ensureStatus()
    expect(store.accountCount).toBe(3)

    // 旧后端不返回这个字段：缺省 0，退化成使用模式，而不是 NaN 或 undefined。
    setActivePinia(createPinia())
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true })
    const legacy = useSupplyStore()
    await legacy.ensureStatus()
    expect(legacy.accountCount).toBe(0)
    expect(legacy.mode).toBe('usage')
  })

  it('forces usage mode and hides the switch when supply is disabled', async () => {
    // 连手动选过共享模式的人也要被拉回来：功能关着时那一侧一个可用页面都没有。
    localStorage.setItem('apexone.consoleMode.1', 'sharing')
    mockGetStatus.mockResolvedValue({ enabled: false, settlement_enabled: false, account_count: 5 })
    const store = useSupplyStore()

    await store.ensureStatus()

    expect(store.mode).toBe('usage')
    expect(store.canSwitchMode).toBe(false)
  })

  it('honours a manual choice over the account count', async () => {
    localStorage.setItem('apexone.consoleMode.1', 'usage')
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 4 })
    const store = useSupplyStore()

    await store.ensureStatus()

    expect(store.mode).toBe('usage')
    expect(store.canSwitchMode).toBe(true)
  })

  it('defaults to sharing mode when the user has accounts, usage when not', async () => {
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 1 })
    const withAccounts = useSupplyStore()
    await withAccounts.ensureStatus()
    expect(withAccounts.mode).toBe('sharing')

    setActivePinia(createPinia())
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 0 })
    const withoutAccounts = useSupplyStore()
    await withoutAccounts.ensureStatus()
    expect(withoutAccounts.mode).toBe('usage')
  })

  it('persists the manual choice under a per-user key', async () => {
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 0 })
    const store = useSupplyStore()
    await store.ensureStatus()

    store.setMode('sharing')

    expect(store.mode).toBe('sharing')
    expect(localStorage.getItem('apexone.consoleMode.1')).toBe('sharing')
    // 全局 key 会把这个选择泄漏给下一个登录的人。
    expect(localStorage.getItem('apexone.consoleMode')).toBeNull()
  })

  it('does not carry one user\'s mode over to the next one who logs in', async () => {
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 0 })
    const store = useSupplyStore()
    await store.ensureStatus()
    store.setMode('sharing')
    expect(store.mode).toBe('sharing')

    // 同一个 SPA 会话内换人：第二个人没有号、也没选过，必须回到使用模式。
    currentUser.value = { id: 2 }
    await store.ensureStatus()

    expect(store.mode).toBe('usage')
    // 第一个人的选择留在盘上，他下次回来还在。
    expect(localStorage.getItem('apexone.consoleMode.1')).toBe('sharing')
    expect(localStorage.getItem('apexone.consoleMode.2')).toBeNull()
  })

  it('restores the stored mode when the same user comes back', async () => {
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 0 })
    const store = useSupplyStore()
    await store.ensureStatus()
    store.setMode('sharing')

    // 新会话（刷新页面）：store 从零开始，但选择该被认出来。
    setActivePinia(createPinia())
    const reloaded = useSupplyStore()
    await reloaded.ensureStatus()

    expect(reloaded.mode).toBe('sharing')
  })

  it('ignores garbage left in storage instead of rendering an unknown mode', async () => {
    localStorage.setItem('apexone.consoleMode.1', 'whatever')
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 2 })
    const store = useSupplyStore()

    await store.ensureStatus()

    // 落回自动判定，而不是把 'whatever' 当成一种模式传给组件。
    expect(store.mode).toBe('sharing')
  })

  // 首页分流：访客未登录时点「我有闲置额度」→ setPendingMode('sharing')；登录后
  // ensureStatus 把这个意图落到本人名下，即使他一个供给账号都没有，也直接进赚钱模式。
  it('carries the homepage entry choice through login (pending → user-scoped)', async () => {
    // 访客点了「共享」入口（还没登录）。
    const guest = useSupplyStore()
    guest.setPendingMode('sharing')
    expect(localStorage.getItem('apexone.pendingConsoleMode')).toBe('sharing')

    // 登录后：新会话、供给开着、但这个人 0 个供给账号（自动判定本会是 usage）。
    setActivePinia(createPinia())
    currentUser.value = { id: 1 }
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 0 })
    const store = useSupplyStore()
    await store.ensureStatus()

    // 意图生效 → 赚钱模式，而不是被自动判定拉回 usage。
    expect(store.mode).toBe('sharing')
    // 意图已落到本人名下，且临时 key 被清掉（不会再泄漏给下一个登录的人）。
    expect(localStorage.getItem('apexone.consoleMode.1')).toBe('sharing')
    expect(localStorage.getItem('apexone.pendingConsoleMode')).toBeNull()
  })

  // 明确的意图压过历史选择：老共享者之前选过 sharing，这次从首页点「我要用 AI」，
  // 登录后应进 usage。
  it('a fresh entry choice overrides the previously stored mode', async () => {
    localStorage.setItem('apexone.consoleMode.1', 'sharing')  // 历史选择
    localStorage.setItem('apexone.pendingConsoleMode', 'usage') // 这次点了「用 AI」
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true, account_count: 5 })
    const store = useSupplyStore()

    await store.ensureStatus()

    expect(store.mode).toBe('usage')
    expect(localStorage.getItem('apexone.consoleMode.1')).toBe('usage')
    expect(localStorage.getItem('apexone.pendingConsoleMode')).toBeNull()
  })

  // 未登录时不消化意图：没有 userID 可安放，等真登录了再落地。
  it('does not consume the pending choice while logged out', async () => {
    currentUser.value = null
    localStorage.setItem('apexone.pendingConsoleMode', 'sharing')
    mockGetStatus.mockResolvedValue(null)  // 未登录，status 拿不到
    const store = useSupplyStore()

    await store.ensureStatus()

    // 意图仍在，等登录后消化。
    expect(localStorage.getItem('apexone.pendingConsoleMode')).toBe('sharing')
  })
})
