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
  /** 当日溢出次数上限，0 = 不限量（仍然计数）。每次溢出平台都在按自营成本供货。 */
  daily_overflow_limit: number

  /** 以下为后端下发的只读用量，PUT 时会被忽略。 */
  usage_day: string
  overflow_used_today: number
  overflow_denied_today: number
}

/** 写回时只带配置，不带用量——用量是读出来的，写回去没有意义。 */
export type SupplyPoolPayload = Omit<
  SupplyPoolSettings,
  'usage_day' | 'overflow_used_today' | 'overflow_denied_today'
>

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

// ============================ 运营视图（只读） ============================
//
// 下面这一组对应 /admin/supply/*，全部是 GET。这一刀刻意不给管理端任何改动供给侧
// 数据的能力：改归属、改余额、手工放行观察期都是会动钱的写操作，需要各自的审计
// 路径，混进看板里迟早会被当成看板随手点。前端这里没有对应的 post/put 不是遗漏。

/** 一个钱包（或一批钱包合计）的四个数。 */
export interface SupplyWalletBalance {
  /** 已解冻，随时可被消费或提现 */
  available: number
  /** 冻结中；冻结窗内发生拒付还能追回 */
  frozen: number
  /** 累计入账，只增不减 */
  history: number
  /** 累计已被消费掉的 */
  spent: number
}

export interface SupplyWalletTotals extends SupplyWalletBalance {
  /** 有钱包行的供给者数（含余额为零的） */
  wallets: number
}

/** 供给账号按状态的分布。字段固定，不是 map——多一个状态该编译不过，而不是画一个没翻译的格子。 */
export interface SupplyAccountCounts {
  total: number
  pending_review: number
  active: number
  draining: number
  retired: number
  /** 上游健康状态不是 active 的。与接入状态正交：一个 active 的号也可能是坏的。 */
  unhealthy: number
  /** 此刻真的在接单的号。与 active 不相等是正常的。 */
  schedulable: number
}

/** 最近一个窗口内的流水合计，按动作分开。 */
export interface SupplyLedgerWindow {
  days: number
  accrued: number
  clawed: number
  /** 钱包内部搬运（frozen → available）。**不要**和 accrued 相加，那是同一笔钱数两遍。 */
  thawed: number
  spent: number
  withdrawn: number
}

export interface SupplyMarketOverview {
  suppliers: number
  accounts: SupplyAccountCounts
  wallet: SupplyWalletTotals
  window: SupplyLedgerWindow
}

/** 名册排序键。取值必须在这个联合里——后端对未知键报 400，不会静默回落。 */
export type SupplierRosterSort = 'owed' | 'history' | 'accounts' | 'recent'

export const SUPPLIER_ROSTER_SORTS: SupplierRosterSort[] = ['owed', 'history', 'accounts', 'recent']

export interface SupplierRosterEntry {
  user_id: number
  email: string
  username?: string
  /** 用户自身状态。被封的用户仍然可能有余额待付，所以照常显示。 */
  user_status: string
  accounts: SupplyAccountCounts
  wallet: SupplyWalletBalance
  /** 空 = 从未赚到过钱（挂了号没被调度到，或一直卡在观察期）——本身就是线索。 */
  last_accrual_at?: string | null
}

export interface SupplyAccountAdminView {
  id: number
  name: string
  platform: string
  owner_user_id: number
  owner_email?: string
  supply_state: string
  status: string
  error_message?: string
  schedulable: boolean
  email_address?: string
  last_used_at?: string | null
  created_at: string
  probation_since?: string | null
  probe_passes: number
  probe_error?: string
  drain_until?: string | null
}

export type SupplyAccountHealth = '' | 'healthy' | 'unhealthy'

export interface SupplyAdminLedgerEntry {
  id: number
  user_id: number
  user_email?: string
  action: string
  amount: number
  request_id?: string
  account_id?: number
  source_user_id?: number
  basis_amount?: number
  share_ratio?: number
  frozen_until?: string
  available_after?: number
  frozen_after?: number
  history_after?: number
  remark?: string
  created_at: string
}

export interface SupplyAdminPage<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  pages: number
}

async function getOverview(windowDays?: number): Promise<SupplyMarketOverview> {
  const { data } = await apiClient.get<SupplyMarketOverview>('/admin/supply/overview', {
    params: windowDays ? { window_days: windowDays } : undefined,
  })
  return data
}

async function listSuppliers(
  params: { page?: number; page_size?: number; keyword?: string; sort?: SupplierRosterSort } = {}
): Promise<SupplyAdminPage<SupplierRosterEntry>> {
  const { data } = await apiClient.get<SupplyAdminPage<SupplierRosterEntry>>('/admin/supply/suppliers', { params })
  return data
}

async function listAccounts(
  params: {
    page?: number
    page_size?: number
    state?: string
    health?: SupplyAccountHealth
    owner_user_id?: number
  } = {}
): Promise<SupplyAdminPage<SupplyAccountAdminView>> {
  const { data } = await apiClient.get<SupplyAdminPage<SupplyAccountAdminView>>('/admin/supply/accounts', { params })
  return data
}

async function listLedger(
  params: {
    page?: number
    page_size?: number
    user_id?: number
    action?: string
    account_id?: number
    request_id?: string
    start_at?: string
    end_at?: string
  } = {}
): Promise<SupplyAdminPage<SupplyAdminLedgerEntry>> {
  const { data } = await apiClient.get<SupplyAdminPage<SupplyAdminLedgerEntry>>('/admin/supply/ledger', { params })
  return data
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

async function updatePoolSettings(payload: SupplyPoolPayload): Promise<SupplyPoolSettings> {
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
  getOverview,
  listSuppliers,
  listAccounts,
  listLedger,
  getSettlementSettings,
  updateSettlementSettings,
  getPoolSettings,
  updatePoolSettings,
  getProbationSettings,
  updateProbationSettings,
}

export default adminSupplyMarketAPI
