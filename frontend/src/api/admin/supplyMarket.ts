/**
 * APEXONE-EXT: 双边市场——管理端配置 API。
 *
 * 两组配置分成两对端点（而不是塞进 /admin/settings 那个巨型 PUT）：
 * 结算参数动的是钱，池配置动的是路由，共用一个端点会让审计日志分不清谁改了什么。
 */

import { apiClient } from '../client'

export interface SupplierSettlementSettings {
  /** 总开关。关闭时不入账、不走钱包，供给账号退化为普通自营账号。 */
  enabled: boolean
  /** 分成比例，基数是消费者实付金额（不是官方价）。 */
  share_ratio: number
  /** 入账冻结小时数。必须 ≥ 支付通道拒付窗，否则冻结期过后被拒付平台自吃。 */
  freeze_hours: number
  /** 为真时消费者的赚取钱包余额优先于 users.balance 被扣。 */
  spend_from_wallet_first: boolean
  /** 后端下发的边界值，前端不要另抄一份。 */
  share_ratio_max: number
  freeze_hours_max: number
}

export type SupplierSettlementPayload = Omit<
  SupplierSettlementSettings,
  'share_ratio_max' | 'freeze_hours_max'
>

export interface SupplyPoolSettings {
  enabled: boolean
  /** 供给池分组 id。只有解析后落在这个分组上的请求才会溢出。 */
  supply_group_id: number
  /** 兜底分组 id（自营池）。 */
  overflow_group_id: number
}

export interface SupplyProbationSettings {
  /** 自动入池总开关。关着时只探测、只记录，不放行——起步形态就是关的。 */
  enabled: boolean
  /** 最短观察时长（分钟）。与 required_successes 是「并且」的关系。 */
  min_observation_minutes: number
  /** 需要连续成功几次探测，中间失败一次清零。 */
  required_successes: number
  /** 两次探测的最小间隔。每次探测花的是供给者自己的额度，所以有下限。 */
  probe_interval_minutes: number
  /** 探测用的模型 id，空 = 平台默认测试模型。 */
  probe_model: string
  /** 优雅下线的排空窗（分钟）。0 = 优雅下线退化为直接终态。 */
  drain_window_minutes: number

  /** 后端下发的边界值，前端不要另抄一份。 */
  min_observation_minutes_max: number
  required_successes_max: number
  probe_interval_minutes_min: number
  probe_interval_minutes_max: number
  drain_window_minutes_max: number
}

export type SupplyProbationPayload = Omit<
  SupplyProbationSettings,
  | 'min_observation_minutes_max'
  | 'required_successes_max'
  | 'probe_interval_minutes_min'
  | 'probe_interval_minutes_max'
  | 'drain_window_minutes_max'
>

async function getSettlementSettings(): Promise<SupplierSettlementSettings> {
  const { data } = await apiClient.get<SupplierSettlementSettings>('/admin/settings/supplier-settlement')
  return data
}

async function updateSettlementSettings(
  payload: SupplierSettlementPayload
): Promise<SupplierSettlementSettings> {
  const { data } = await apiClient.put<SupplierSettlementSettings>('/admin/settings/supplier-settlement', payload)
  return data
}

async function getPoolSettings(): Promise<SupplyPoolSettings> {
  const { data } = await apiClient.get<SupplyPoolSettings>('/admin/settings/supply-pool')
  return data
}

async function updatePoolSettings(payload: SupplyPoolSettings): Promise<SupplyPoolSettings> {
  const { data } = await apiClient.put<SupplyPoolSettings>('/admin/settings/supply-pool', payload)
  return data
}

async function getProbationSettings(): Promise<SupplyProbationSettings> {
  const { data } = await apiClient.get<SupplyProbationSettings>('/admin/settings/supply-probation')
  return data
}

/**
 * 写观察期参数。后端**夹回区间而不是报错**（与结算参数刻意不同），所以这里
 * 一定要把返回值写回表单——那是运营看到自己填的 1 分钟变成 5 分钟的唯一途径。
 */
async function updateProbationSettings(
  payload: SupplyProbationPayload
): Promise<SupplyProbationSettings> {
  const { data } = await apiClient.put<SupplyProbationSettings>('/admin/settings/supply-probation', payload)
  return data
}

export const adminSupplyMarketAPI = {
  getSettlementSettings,
  updateSettlementSettings,
  getPoolSettings,
  updatePoolSettings,
  getProbationSettings,
  updateProbationSettings,
}

export default adminSupplyMarketAPI
