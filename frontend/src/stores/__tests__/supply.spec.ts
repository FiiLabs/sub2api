/**
 * APEXONE-EXT: 供给侧开关 store。
 *
 * 三件必须成立的事：请求失败要按"没开放"处理（fail-closed，否则侧边栏挂出一个
 * 点进去是 404 的入口）、同一用户只问一次（侧边栏每次路由切换都重挂载）、
 * 换个人登录必须重问（否则上一个人的开关留在菜单上）。
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

describe('supply store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    mockGetStatus.mockReset()
    currentUser.value = { id: 1 }
  })

  it('applies both flags from the backend', async () => {
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: false })
    const store = useSupplyStore()

    await store.ensureStatus()

    expect(store.enabled).toBe(true)
    expect(store.settlementEnabled).toBe(false)
  })

  it('fails closed when the status call fails', async () => {
    mockGetStatus.mockRejectedValue(new Error('401'))
    const store = useSupplyStore()

    await store.ensureStatus()

    expect(store.enabled).toBe(false)
    expect(store.settlementEnabled).toBe(false)
    expect(store.loaded).toBe(true)
  })

  it('caches per user and dedups concurrent calls', async () => {
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true })
    const store = useSupplyStore()

    await Promise.all([store.ensureStatus(), store.ensureStatus()])
    await store.ensureStatus()

    expect(mockGetStatus).toHaveBeenCalledTimes(1)
  })

  it('refetches when a different user is logged in', async () => {
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true })
    const store = useSupplyStore()
    await store.ensureStatus()

    currentUser.value = { id: 2 }
    mockGetStatus.mockResolvedValue({ enabled: false, settlement_enabled: false })
    await store.ensureStatus()

    expect(mockGetStatus).toHaveBeenCalledTimes(2)
    expect(store.enabled).toBe(false)
  })

  it('refresh() forces a new call for the same user', async () => {
    mockGetStatus.mockResolvedValue({ enabled: true, settlement_enabled: true })
    const store = useSupplyStore()
    await store.ensureStatus()

    await store.refresh()

    expect(mockGetStatus).toHaveBeenCalledTimes(2)
  })
})
