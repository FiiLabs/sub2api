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
  // 这里**没有** source_user_id：消费者身份在服务端就被抹掉了
  // （service/supplier_credit_service.go 的 stripConsumerIdentity）。
  // 管理端的 SupplyAdminLedgerEntry 保留该字段，两个类型的差别仅此一处。
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

/**
 * 供给者协议的状态。四种情况被压在这三个字段里，界面要按这个顺序判：
 *
 *   1. !published            → 平台还没发布协议，接入被拒，供给者点什么都没用
 *   2. published && accepted → 可以接入
 *   3. published && accepted_version 非空且不等于 version → 协议改版了，要重新确认
 *   4. published && accepted_version 为空 → 从没同意过
 *
 * 3 和 4 的界面文案必须不同：对一个明明点过同意的人说「请先同意」，他会以为系统坏了。
 */
export interface SupplyAgreement {
  /** 当前生效的协议版本号。空 = 平台尚未发布。 */
  version: string
  published: boolean
  /** 协议全文外链，可空 */
  url?: string
  /** 协议正文，可空。**按纯文本渲染**，不得当 HTML/markdown 塞进 v-html。 */
  body?: string
  /** 当前用户是否已同意 version 这一版。这一个布尔就是门禁的判据。 */
  accepted: boolean
  accepted_at?: string
  /** 该用户最近一次同意的版本号 */
  accepted_version?: string
}

/**
 * 一张提现单。
 *
 * 相对后端的领域对象少两个字段：user_id（就是他自己）和 reviewer_id（处理人是谁
 * 与钱到不到账无关）。review_note 和 external_ref 反过来**必须**留着：
 * 前者是被拒时他唯一能拿到的解释，后者是对账时双方共同的锚点。
 */
export interface SupplyWithdrawal {
  id: number
  amount: number
  /**
   * pending | processing | failed | paid | rejected | canceled。
   * 只有 pending 能撤回：processing 是 worker 正在上链（此刻撤回可能与一笔
   * 在途转账同时成立），failed 在等运营裁决（钱还挂在单子上）。
   */
  status: string
  payout_channel: string
  payout_account: string
  user_note?: string
  /**
   * 这笔提现的 gas，从 amount **内部**扣掉。人工渠道恒为 0。
   *
   * 后端不给它加 omitempty，所以这里也不写 `?`：0 和 undefined 在做减法时
   * 是两回事，后者会让界面显示「到账 NaN」。
   */
  fee_amount: number
  /** 实际到账 = amount - fee_amount。**由后端算**，前端不重算一遍。 */
  net_amount: number
  /**
   * 非空 = 这张单子会被 worker 自动打款；空 = 人工打款。
   *
   * 界面靠它决定说"几分钟内到账"还是"工作日内处理"——两句话的落差是几个小时，
   * 说错了人就会在第一个小时里来问。
   */
  network?: string
  token_symbol?: string
  /** 申请时那条 withdraw 流水的 id，用来把这张单子和账单页对上 */
  ledger_id?: number
  review_note?: string
  external_ref?: string
  created_at: string
  updated_at: string
  resolved_at?: string | null
}

export interface SupplyWithdrawalPage {
  items: SupplyWithdrawal[]
  total: number
  page: number
  page_size: number
  pages: number
}

/**
 * 申请表单需要的一切，一次拿全。
 *
 * 拆成三个接口拼是错的：这几个数必须来自同一时刻，否则会画出「余额 100、渠道
 * 列表空着、按钮还亮着」这种自相矛盾的界面。
 */
export interface SupplyWithdrawalOptions {
  /** 此刻真能提（开关开着**且**配了渠道）。按钮只看这一个布尔。 */
  available: boolean
  /**
   * 总开关。与 available 分开报，是为了让「开着但没配渠道」也能说清楚：
   * enabled && !available 时文案该是"渠道维护中"，而不是"暂未开放"。
   */
  enabled: boolean
  min_amount: number
  max_pending: number
  channels: string[]
  notice: string
  /** 此刻可提余额（钱包可用区，不含冻结） */
  available_credit: number
  /** 已挂着的未决单数，达到 max_pending 就提不了了 */
  pending_count: number
  /**
   * 会自动上链结算的渠道（渠道名 → 链 + 币）。
   *
   * 回答的是「**如果**选了这个渠道会怎么结算」，不是「现在能不能选」——
   * 能不能选看上面的 channels 白名单。两件事分开，是为了让「先把代码放上去、
   * 之后再打开」这条上线路径成立：一个渠道可以先出现在这里、暂不进白名单。
   *
   * 表单靠它决定收款账号那一栏画输入框还是画「你绑定的地址」。
   */
  onchain_channels?: SupplyOnchainChannel[]
  /**
   * 此刻**真能自动结算**的渠道各自的手续费报价。
   *
   * 它是 onchain_channels 的子集，且刻意允许比后者小：一个渠道可以「会上链」
   * 但此刻结算不了（没接客户端 / 金库没配这种币 / 手续费估不出来）——那样的
   * 渠道不出现在这里。表单靠「在不在这个数组里」分辨该画报价还是画
   * 「按人工打款处理」，**不要**拿不到报价就显示 0：0 元手续费是一个承诺。
   *
   * 报价不是承诺——建单那一刻会重新估一次，落库的是那一次的数。
   */
  onchain_fees?: SupplyWithdrawalFeeQuote[]
}

/** 一个会自动上链结算的渠道。 */
export interface SupplyOnchainChannel {
  /** 渠道名，与 channels 白名单里的字符串一字不差 */
  channel: string
  /** 链标识，也是绑定接口路径上那个 :network */
  network: string
  token_symbol: string
}

/** 一个链上渠道此刻的手续费报价。 */
export interface SupplyWithdrawalFeeQuote {
  /** 渠道名，与 onchain_channels 里的 channel 一字不差 */
  channel: string
  /** 手续费（与提现同一币种），会从提现金额里扣掉 */
  fee: number
  /** true = 实时估算；false = 配置里写死的兜底值 */
  estimated: boolean
}

/**
 * 一条收款地址绑定。
 *
 * address 是**小写**形态。要显示成 EIP-55 混合大小写请前端自己加——后端返回
 * 什么、就该能原样再提交回来，而一个被美化过的地址提交回来会撞在校验和那道门上。
 */
export interface SupplyPayoutWallet {
  id: number
  network: string
  address: string
  created_at: string
  updated_at: string
}

/**
 * 绑定表单需要的一切，一次拿全。
 *
 * 与提现 options 同一个理由：分两个接口拼会画出「链列表里没有 bsc、下面却显示着
 * 一个 bsc 地址」这种自相矛盾的界面。
 */
export interface SupplyPayoutWalletOptions {
  channels: SupplyOnchainChannel[]
  /** 已绑定的地址。没绑过是空数组，不是 null。 */
  wallets: SupplyPayoutWallet[]
}

export interface StartOAuthResponse {
  auth_url: string
  session_id: string
}

async function getStatus(): Promise<SupplyStatus> {
  const { data } = await apiClient.get<SupplyStatus>('/user/supply/status')
  return data
}

async function getAgreement(): Promise<SupplyAgreement> {
  const { data } = await apiClient.get<SupplyAgreement>('/user/supply/agreement')
  return data
}

/**
 * 同意当前版本的协议。
 *
 * version 必须是**页面上正在显示的那一版**，不能省略让服务端自己取当前版本：
 * 前者能证明他看到的就是这一版，后者只能证明他点了一下按钮。页面开了两天没刷新
 * 的人，点的是旧版正文——那时服务端会回 SUPPLIER_AGREEMENT_VERSION_MISMATCH，
 * 界面该做的是重新拉一次协议让他再读一遍，而不是重试。
 */
async function acceptAgreement(version: string): Promise<SupplyAgreement> {
  const { data } = await apiClient.post<SupplyAgreement>('/user/supply/agreement/accept', { version })
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

/** 解绑的结果。带一个布尔而不是只回 success，理由见下面 detachAccount 的注释。 */
export interface DetachAccountResult {
  detached: boolean
  /** 上游那边的授权还在，需要供给者自己去撤销 */
  upstream_revoke_required: boolean
}

/**
 * 彻底解绑一个号：平台停调度、抹掉凭证、把号摘掉。**不可撤销**。
 *
 * 与 pauseAccount 的区别是本质性的，界面上必须讲清楚：下线只是"不再派单"，
 * 那份 refresh token 仍然在平台手里；解绑才是把凭证删掉。
 *
 * 返回的 upstream_revoke_required 目前恒为 true——Anthropic 没有公开可调用的
 * 撤销端点，所以上游那边的授权记录只有供给者自己能在账号设置里清掉。
 * 做成字段而不是在前端写死，是为了将来后端真能远端撤销时能一次性把提示关掉。
 */
async function detachAccount(id: number): Promise<DetachAccountResult> {
  const { data } = await apiClient.delete<DetachAccountResult>(`/user/supply/accounts/${id}`)
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

/**
 * 读申请表单的全部前提。提现没开时也照常成功返回（available=false + notice）——
 * 这个接口的用途就是解释「为什么现在提不了」，让它报错等于什么都不解释。
 */
async function getWithdrawalOptions(): Promise<SupplyWithdrawalOptions> {
  const { data } = await apiClient.get<SupplyWithdrawalOptions>('/user/supply/withdrawals/options')
  return data
}

/**
 * 提交一笔提现申请。
 *
 * **提交的这一刻钱就从可用区扣走了**，不是审批时才扣。界面文案必须按这个说，
 * 否则供给者会以为申请只是"排个队"，然后发现余额少了一块。
 *
 * 低于起提额的金额后端**拒绝**而不是夹到起提额，所以这里的错误要原样弹出去。
 */
async function requestWithdrawal(payload: {
  amount: number
  payout_channel: string
  payout_account: string
  user_note?: string
}): Promise<SupplyWithdrawal> {
  const { data } = await apiClient.post<SupplyWithdrawal>('/user/supply/withdrawals', payload)
  return data
}

async function listWithdrawals(
  params: { page?: number; page_size?: number } = {}
): Promise<SupplyWithdrawalPage> {
  const { data } = await apiClient.get<SupplyWithdrawalPage>('/user/supply/withdrawals', { params })
  return data
}

/**
 * 撤回自己的未决单，钱退回可用区。
 *
 * 是 POST 不是 DELETE：单子不会消失，它变成 canceled 留在列表里。撤回是一次
 * 状态推进，不是一次删除——那行记录本身是对账凭据。
 */
async function cancelWithdrawal(id: number): Promise<SupplyWithdrawal> {
  const { data } = await apiClient.post<SupplyWithdrawal>(`/user/supply/withdrawals/${id}/cancel`)
  return data
}

/** 读绑定表单的全部前提。没绑过也照常成功返回（wallets 为空数组）。 */
async function getPayoutWallets(): Promise<SupplyPayoutWalletOptions> {
  const { data } = await apiClient.get<SupplyPayoutWalletOptions>('/user/supply/payout-wallets')
  return data
}

/**
 * 绑定或换绑某条链上的收款地址。
 *
 * 是 PUT 不是 POST：每人每链**至多一个**地址，这是一次幂等的置换，不是往一个
 * 集合里追加。重复提交同一个地址必须是同一个结果。
 *
 * 地址走请求体、链走路径，两者不能对调：地址会出现在 access log、代理日志和
 * 浏览器历史里，而一条能把这个账户的钱全部取走的地址不该进那三个地方。
 *
 * 地址已被别人绑走时后端回 409（`SUPPLIER_PAYOUT_ADDRESS_TAKEN`），那**不是**
 * 格式问题——文案要说"这个地址已绑在另一个账号上"，否则用户会一遍遍去检查一个
 * 完全正确的地址。校验和错（`..._CHECKSUM`）同理要与格式错分开说。
 */
async function bindPayoutWallet(network: string, address: string): Promise<SupplyPayoutWallet> {
  const { data } = await apiClient.put<SupplyPayoutWallet>(
    `/user/supply/payout-wallets/${encodeURIComponent(network)}`,
    { address }
  )
  return data
}

/**
 * 解绑某条链上的收款地址。
 *
 * 不影响任何**在途**的提现单：那些单子上的收款地址是建单那一刻的快照，
 * 解绑改不了任何一笔已经在路上的钱。界面上要说清楚这一点，否则会有人以为
 * 解绑能把一笔打错地址的款拦下来。
 */
async function unbindPayoutWallet(network: string): Promise<{ network: string; bound: boolean }> {
  const { data } = await apiClient.delete<{ network: string; bound: boolean }>(
    `/user/supply/payout-wallets/${encodeURIComponent(network)}`
  )
  return data
}

export const supplyAPI = {
  getStatus,
  getAgreement,
  acceptAgreement,
  startOAuth,
  completeOAuth,
  listAccounts,
  pauseAccount,
  resumeAccount,
  detachAccount,
  getWallet,
  listLedger,
  getWithdrawalOptions,
  requestWithdrawal,
  listWithdrawals,
  cancelWithdrawal,
  getPayoutWallets,
  bindPayoutWallet,
  unbindPayoutWallet,
}

export default supplyAPI
