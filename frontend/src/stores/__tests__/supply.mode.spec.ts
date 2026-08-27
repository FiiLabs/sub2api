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
})
