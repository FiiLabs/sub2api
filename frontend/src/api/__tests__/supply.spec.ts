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
