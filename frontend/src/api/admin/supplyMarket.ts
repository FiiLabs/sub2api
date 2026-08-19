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

export const adminSupplyMarketAPI = {
  getSettlementSettings,
  updateSettlementSettings,
  getPoolSettings,
  updatePoolSettings,
}

export default adminSupplyMarketAPI
