/**
 * 首页公开数据 API（无鉴权，可匿名访问）。
 * 共享号数 / 活跃用户 / 累计请求 / 已付贡献者收益——真实聚合 + 运营配的基数偏移。
 * enabled=false 时前端应隐藏这一段。
 */

import { apiClient } from './client'

/** 首页公开数据（已叠加偏移）。 */
export interface PublicHomepageStats {
  /** 总开关。false 时其余字段无意义，应隐藏。 */
  enabled: boolean
  /** 展示用共享号数。 */
  shared_accounts: number
  /** 展示用活跃用户数。 */
  active_users: number
  /** 展示用累计请求数。 */
  total_requests: number
  /** 展示用已付贡献者收益（USDT）。 */
  contributor_earnings_usdt: number
  /** 按平台的真实可调度供给号数（环形图占比用；首页数据带忽略）。enabled=false 或读不到时缺省。 */
  supply_by_platform?: Record<string, number>
}

/** 拉取首页公开数据。失败时调用方应 fail-soft（隐藏整段），不阻塞首页。 */
export async function getPublicStats(options?: { signal?: AbortSignal }): Promise<PublicHomepageStats> {
  const { data } = await apiClient.get<PublicHomepageStats>('/stats/public', {
    signal: options?.signal,
  })
  return data
}
