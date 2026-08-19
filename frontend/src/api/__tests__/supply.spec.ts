/**
 * APEXONE-EXT: 供给侧 API 的线上契约。
 *
 * 这里盯的是"发出去的请求长什么样"，因为下线通道错一个字段就是一次
 * 不可撤销的误操作：mode 丢了后端按 graceful 兜底，用户点的却是"立即下线"。
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post },
}))

import { supplyAPI } from '@/api/supply'
import { adminSupplyMarketAPI } from '@/api/admin/supplyMarket'

describe('supply api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
  })

  it('sends the pause mode explicitly', async () => {
    await supplyAPI.pauseAccount(42, 'immediate')

    expect(post).toHaveBeenCalledWith('/user/supply/accounts/42/pause', { mode: 'immediate' })
  })

  it('defaults to the cancellable channel when no mode is given', async () => {
    // 默认必须是 graceful：不可撤销的那条通道只能由调用方显式选。
    await supplyAPI.pauseAccount(7)

    expect(post).toHaveBeenCalledWith('/user/supply/accounts/7/pause', { mode: 'graceful' })
  })

  it('resumes without a body', async () => {
    await supplyAPI.resumeAccount(7)

    expect(post).toHaveBeenCalledWith('/user/supply/accounts/7/resume')
  })

  it('never sends a user id — ownership comes from the JWT', async () => {
    await supplyAPI.listAccounts()

    expect(get).toHaveBeenCalledWith('/user/supply/accounts')
  })
})

describe('admin supply market api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
  })

  it('reads and writes probation settings on their own endpoint', async () => {
    const put = vi.fn().mockResolvedValue({ data: {} })
    // put 不在默认 mock 里，单测里补一个，避免为一条断言改动全局 stub 形状。
    const client = (await import('@/api/client')).apiClient as unknown as Record<string, unknown>
    client.put = put

    await adminSupplyMarketAPI.getProbationSettings()
    expect(get).toHaveBeenCalledWith('/admin/settings/supply-probation')

    await adminSupplyMarketAPI.updateProbationSettings({
      enabled: true,
      min_observation_minutes: 60,
      required_successes: 2,
      probe_interval_minutes: 15,
      probe_model: '',
      drain_window_minutes: 10,
    })
    expect(put).toHaveBeenCalledWith('/admin/settings/supply-probation', {
      enabled: true,
      min_observation_minutes: 60,
      required_successes: 2,
      probe_interval_minutes: 15,
      probe_model: '',
      drain_window_minutes: 10,
    })
  })

  it('writes the overflow cap but never writes back the usage readout', async () => {
    // 用量是后端算出来的只读数字。把它一起 PUT 回去，管理员保存一次配置就等于
    // 声称自己知道今天溢出了多少次——那是个会被后端忽略、却先污染了请求语义的字段。
    const put = vi.fn().mockResolvedValue({ data: {} })
    const client = (await import('@/api/client')).apiClient as unknown as Record<string, unknown>
    client.put = put

    await adminSupplyMarketAPI.updatePoolSettings({
      enabled: true,
      supply_group_id: 10,
      overflow_group_id: 11,
      daily_overflow_limit: 500,
    })

    expect(put).toHaveBeenCalledWith('/admin/settings/supply-pool', {
      enabled: true,
      supply_group_id: 10,
      overflow_group_id: 11,
      daily_overflow_limit: 500,
    })
  })

  it('keeps 0 in the payload — it means unlimited, not "unset"', async () => {
    // 0 被过滤掉的话后端会当成「没填」而保留旧配额，管理员点了保存却发现限制还在。
    const put = vi.fn().mockResolvedValue({ data: {} })
    const client = (await import('@/api/client')).apiClient as unknown as Record<string, unknown>
    client.put = put

    await adminSupplyMarketAPI.updatePoolSettings({
      enabled: true,
      supply_group_id: 10,
      overflow_group_id: 11,
      daily_overflow_limit: 0,
    })

    expect(put).toHaveBeenCalledWith(
      '/admin/settings/supply-pool',
      expect.objectContaining({ daily_overflow_limit: 0 })
    )
  })
})

describe('admin supply ops api (read-only)', () => {
  beforeEach(() => {
    get.mockReset()
    get.mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20, pages: 1 } })
  })

  it('hits the four read endpoints under /admin/supply', async () => {
    await adminSupplyMarketAPI.getOverview(30)
    expect(get).toHaveBeenCalledWith('/admin/supply/overview', { params: { window_days: 30 } })

    await adminSupplyMarketAPI.listSuppliers({ page: 2, page_size: 20, sort: 'owed' })
    expect(get).toHaveBeenCalledWith('/admin/supply/suppliers', {
      params: { page: 2, page_size: 20, sort: 'owed' },
    })

    await adminSupplyMarketAPI.listAccounts({ state: 'pending_review', health: 'unhealthy' })
    expect(get).toHaveBeenCalledWith('/admin/supply/accounts', {
      params: { state: 'pending_review', health: 'unhealthy' },
    })

    await adminSupplyMarketAPI.listLedger({ request_id: 'req-1' })
    expect(get).toHaveBeenCalledWith('/admin/supply/ledger', { params: { request_id: 'req-1' } })
  })

  it('omits window_days when not given, letting the backend pick the default', async () => {
    // 传 0 会被后端夹回默认值，但那要求前端和后端各自记着「0 表示默认」。
    // 干脆不传：默认值只有后端一份。
    await adminSupplyMarketAPI.getOverview()
    expect(get).toHaveBeenCalledWith('/admin/supply/overview', { params: undefined })
  })

  it('exposes no write path for the ops view', () => {
    // 这一刀刻意不给管理端改供给侧数据的能力。多出来的任何一个 update* 都该
    // 先经过一次设计讨论，而不是顺手加在这个 API 对象上。
    const writers = Object.keys(adminSupplyMarketAPI).filter((key) => /^(update|create|delete)/.test(key))
    expect(writers.sort()).toEqual([
      'updatePoolSettings',
      'updateProbationSettings',
      'updateSettlementSettings',
    ])
  })
})
