/**
 * APEXONE-EXT: 双边市场——供给侧 API（/api/v1/user/supply/*）。
 *
 * 全部是"当前登录用户"的接口，没有任何一个接受 user_id 参数：归属只能来自 JWT。
 * 如果日后有人想加一个 `?user_id=`，那是管理端的需求，应当走 /admin 路由。
 */

import { apiClient } from './client'

/** 供给侧的两个功能开关。分开报是因为它们真能处于不同状态，见后端 SupplierStatusResponse。 */
export interface SupplyStatus {
  /** 自助接入是否开放 */
  enabled: boolean
  /** 结算是否开启（挂上来的号产生的用量是否入账） */
  settlement_enabled: boolean
}

/** 下线通道。graceful 可在排空窗内取消，immediate 直接进终态、不可撤销。 */
export type SupplyPauseMode = 'graceful' | 'immediate'

/** 供给账号的对外视图。刻意不含 credentials——那是凭证，前端永远不需要。 */
export interface SupplyAccount {
  id: number
  name: string
  platform: string
  /** pending_review | active | draining | retired */
  supply_state: string
  /** 是否可被调度。新号一律 false，由观察期流程放行。 */
  schedulable: boolean
  /** 上游账号健康状态（active / error / ...），凭证失效时会变 */
  status: string
  error_message?: string
  /** 上游账号邮箱，用来分辨自己挂了哪几个号 */
  email_address?: string
  last_used_at?: string | null
  created_at: string

  /** 本轮观察期起点 */
  probation_since?: string | null
  /**
   * 观察窗最早何时满足。是一个「不早于」——入池还要求连续探测达标且自动入池开着，
   * 文案要按这个语气写，不能说成「将于此时入池」。
   */
  eligible_at?: string | null
  /** 已连续通过几次探测 */
  probe_passes: number
  /** 上次探测失败原因。有值 = 供给者自己要动手（多半是重新授权）。 */
  probe_error?: string
  /** 排空窗到期时刻，仅 draining 状态有值 */
  drain_until?: string | null
}

/** 赚取钱包快照。 */
export interface SupplyWallet {
  user_id: number
  /** 可用余额（已过冻结期） */
  available_credit: number
  /** 冻结中余额（未过冻结期，不可提取/消费） */
  frozen_credit: number
  /** 历史累计入账 */
  history_credit: number
  /** 已消费 */
  spent_credit: number
  created_at: string
  updated_at: string
}

/** 钱包流水一行。 */
export interface SupplyLedgerEntry {
  id: number
  user_id: number
  /** accrue | spend | thaw | clawback | withdraw */
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

export interface SupplyLedgerPage {
  items: SupplyLedgerEntry[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface StartOAuthResponse {
  auth_url: string
  session_id: string
}

async function getStatus(): Promise<SupplyStatus> {
  const { data } = await apiClient.get<SupplyStatus>('/user/supply/status')
  return data
}

/**
 * 发起一次授权。返回的 session_id 必须原样带回 completeOAuth——
 * state 和 code_verifier 留在服务端，前端拿不到，也就无从把它们喂给别的流程。
 */
async function startOAuth(): Promise<StartOAuthResponse> {
  const { data } = await apiClient.post<StartOAuthResponse>('/user/supply/oauth/start')
  return data
}

async function completeOAuth(payload: {
  session_id: string
  code: string
  name?: string
}): Promise<SupplyAccount> {
  const { data } = await apiClient.post<SupplyAccount>('/user/supply/oauth/complete', payload)
  return data
}

async function listAccounts(): Promise<SupplyAccount[]> {
  const { data } = await apiClient.get<{ accounts: SupplyAccount[] }>('/user/supply/accounts')
  return data?.accounts ?? []
}

/**
 * 下线一个号。两条通道都会立刻停止接新单，差别只在终态多快到来、还能不能反悔。
 * 两者都**停不掉已经在流的请求**——界面文案必须说清楚这一点。
 */
async function pauseAccount(id: number, mode: SupplyPauseMode = 'graceful'): Promise<SupplyAccount> {
  const { data } = await apiClient.post<SupplyAccount>(`/user/supply/accounts/${id}/pause`, { mode })
  return data
}

async function resumeAccount(id: number): Promise<SupplyAccount> {
  const { data } = await apiClient.post<SupplyAccount>(`/user/supply/accounts/${id}/resume`)
  return data
}

async function getWallet(): Promise<SupplyWallet> {
  const { data } = await apiClient.get<SupplyWallet>('/user/supply/wallet')
  return data
}

async function listLedger(params: { page?: number; page_size?: number; action?: string } = {}): Promise<SupplyLedgerPage> {
  const { data } = await apiClient.get<SupplyLedgerPage>('/user/supply/ledger', { params })
  return data
}

export const supplyAPI = {
  getStatus,
  startOAuth,
  completeOAuth,
  listAccounts,
  pauseAccount,
  resumeAccount,
  getWallet,
  listLedger,
}

export default supplyAPI
