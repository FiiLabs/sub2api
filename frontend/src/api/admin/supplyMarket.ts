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

export interface SupplyAgreementSettings {
  /** 协议版本号。空 = 尚未发布 = 自助接入被**拒绝**（不是"跳过同意"）。 */
  version: string
  /** 协议全文外链，可空。只允许 http/https，后端会拒绝别的协议头。 */
  url: string
  /** 协议正文，可空。纯文本，页面上不会被当 HTML 渲染。 */
  body: string
  /** 后端算好的「有没有发布」，前端不必自己判断空字符串的含义。 */
  published: boolean

  /** 后端下发的边界值，前端不要另抄一份。 */
  version_max_len: number
  url_max_len: number
  body_max_len: number
}

export type SupplyAgreementPayload = Omit<
  SupplyAgreementSettings,
  'published' | 'version_max_len' | 'url_max_len' | 'body_max_len'
>

export interface SupplyOnboardingSettings {
  /**
   * 一个人名下最多能有几个未解绑的供给账号。**0 = 不限**。
   *
   * 数的是当下的账号数而不是历史累计：解绑一个号就腾出一个位置。
   */
  max_accounts_per_user: number
  /**
   * 同一个接入来源 IP 上最多能有几个未解绑的供给账号。**0 = 不限**，且默认就是 0。
   *
   * 默认关着是有意的：运营商级 NAT、校园网、公司出口后面站着成百上千个真实的人，
   * 一个偏小的默认值会把他们静默地挡在门外——而症状（"注册了但挂不上号"）
   * 不会有人来报障，他们只会离开。
   */
  max_accounts_per_ip: number

  /**
   * 后端算好的「这道闸开着吗」。前端**不要**自己写 `> 0` 去判——0 在这两个字段上
   * 是「不限」而不是「没填」，这个含义只应该有一个地方知道。
   */
  user_cap_enabled: boolean
  ip_cap_enabled: boolean

  /** 后端下发的边界值，前端不要另抄一份。 */
  max_accounts_per_user_cap: number
  max_accounts_per_ip_cap: number
}

export type SupplyOnboardingPayload = Omit<
  SupplyOnboardingSettings,
  'user_cap_enabled' | 'ip_cap_enabled' | 'max_accounts_per_user_cap' | 'max_accounts_per_ip_cap'
>

export interface SupplyWithdrawalSettings {
  /** 总开关。默认关着——一个还没定好打款流程的平台不该先把提现按钮点亮。 */
  enabled: boolean
  /** 起提额。低于它的申请后端**拒绝**，不夹回。 */
  min_amount: number
  /** 每人同时挂着的未决单上限。 */
  max_pending: number
  /**
   * 收款渠道白名单。**大小写敏感**：供给者提交的渠道要与这里的字符串完全相等
   * （只 trim 首尾空白），所以 "USDT" 和 "usdt" 是两个渠道。
   */
  channels: string[]
  /** 给供给者看的说明（到账时效、手续费等），纯文本。 */
  notice: string

  /**
   * 新申请到达时通知谁。**空 = 没有任何人会被告知有单要处理**——后台不会自己
   * 弹出来，而供给者的钱在提交那一刻就已经离开可用余额了。
   *
   * 与配额告警的收件人是两份配置：收钱的是财务，收告警的是运维，合成一份会训练
   * 两边都去过滤这类邮件。
   *
   * 格式错误后端**报错**而不是静默丢弃（渠道则是静默清洗）：渠道少一个供给者
   * 立刻就看得见，收件人少一个没有任何可见症状。
   */
  notify_emails: string[]

  /**
   * enabled **且** channels 非空。开着却一个渠道都没配是一种静默失效：
   * 面板显示"已开启"，供给者点提现被硬拒。有了这个布尔，运营在设置页就能看见
   * 自己配漏了，而不是等人来报。
   */
  available: boolean

  /**
   * enabled **且** notify_emails 非空。与 available 同理，指向另一个静默失效：
   * 提现开着、渠道也配了，但申请进来时没有人被通知。
   */
  notify_configured: boolean

  /** 后端下发的边界值，前端不要另抄一份。 */
  min_amount_max: number
  max_pending_cap: number
  channels_max: number
  channel_max_len: number
  notice_max_len: number
  notify_emails_max: number
  notify_email_max_len: number
}

/** 链上金库客户端此刻的装配状态（M6）。 */
export interface SupplyPayoutChainStatus {
  /** disabled | mock | live */
  mode: string
  summary: string
  /** live 时非空。公开信息：每笔链上交易里都写着它 */
  treasury?: string
  /** 向节点核对链 ID 的结果；缺席 = 没核（disabled/mock） */
  chain_verified?: boolean
  error?: string
  /** console | env | mock-env | none */
  source: string
  applied_at: string
}

/**
 * 链上打款金库配置（M6）。响应里**永远没有私钥**，连密文都没有——
 * 回显的只有 signer_configured 和 status.treasury（从私钥推导出的地址）。
 */
export interface SupplyPayoutChainSettings {
  enabled: boolean
  rpc_url: string
  token_address: string
  token_symbol: string
  disperse_address: string
  chain_id: number
  native_usd: number
  confirmations: number
  fallback_fee: number
  fee_multiplier: number
  signer_configured: boolean
  status: SupplyPayoutChainStatus
}

/** 保存金库配置的请求体。signer_key 留空 = 保留已存的那把。 */
export interface SupplyPayoutChainPayload {
  enabled: boolean
  rpc_url: string
  token_address: string
  token_symbol: string
  disperse_address: string
  chain_id: number
  native_usd: number
  confirmations: number
  fallback_fee: number
  fee_multiplier: number
  signer_key: string
}

export type SupplyWithdrawalPayload = Omit<
  SupplyWithdrawalSettings,
  | 'available'
  | 'notify_configured'
  | 'min_amount_max'
  | 'max_pending_cap'
  | 'channels_max'
  | 'channel_max_len'
  | 'notice_max_len'
  | 'notify_emails_max'
  | 'notify_email_max_len'
>

// ======================= 运营视图（只读，提现审批除外） =======================
//
// 下面这一组对应 /admin/supply/*，除提现审批外全部是 GET。这一刀刻意不给管理端
// 改动供给侧数据的能力：改归属、改余额、手工放行观察期都是会动钱的写操作，需要
// 各自的审计路径，混进看板里迟早会被当成看板随手点。前端这里没有对应的 post/put
// 不是遗漏。
//
// **唯一的例外是提现审批**（markWithdrawalPaid / rejectWithdrawal）。它存在是因为
// 一张已经扣了钱的单子必须有人能推进它——不给这个能力，那笔钱就永远挂着。这个例外
// 只改单子状态与退款，不碰账号、归属、观察期。

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
  /** 本窗口**申请**走的（申请即扣款），不是已打款额。 */
  withdrawn: number
  /**
   * 本窗口被拒绝/被撤回退回可用区的。
   *
   * 与 withdrawn 分开报而不是相减：净额只回答"钱少了多少"，而退回的**笔数**
   * 才是"渠道配错了"或"审核标准有问题"的信号。
   */
  withdraw_reverted: number
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

/**
 * 管理端看到的提现单。比供给者那份多两个字段：user_id 和 reviewer_id。
 * 审批队列必须能看见是谁的单、上一次是谁处理的。
 */
export interface SupplyWithdrawalAdminView {
  id: number
  user_id: number
  amount: number
  status: string
  payout_channel: string
  payout_account: string
  user_note?: string
  ledger_id?: number
  reviewer_id?: number
  review_note?: string
  external_ref?: string
  // ---- 链上结算（M3/M4）。人工单上这些字段不出现。----
  /** 手续费，从 amount 内部扣；链上实发 = amount - fee_amount */
  fee_amount?: number
  network?: string
  token_symbol?: string
  token_address?: string
  /** 广播出去那笔交易的哈希。failed 单上它是运营核实链上真相的钥匙 */
  tx_hash?: string
  /** worker 上一次没走通的原因。failed 单必看：它写明了该核实什么 */
  last_error?: string
  created_at: string
  updated_at: string
  resolved_at?: string | null
}

/** 提现单状态。传给后端的值必须在这个联合里——未知状态后端报 400，不会静默"不筛"。 */
export type SupplyWithdrawalStatus = 'pending' | 'processing' | 'failed' | 'paid' | 'rejected' | 'canceled'

export const SUPPLY_WITHDRAWAL_STATUSES: SupplyWithdrawalStatus[] = [
  'pending',
  'processing',
  'failed',
  'paid',
  'rejected',
  'canceled',
]

export interface SupplyAdminPage<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  pages: number
}

/**
 * 一次供给号失效。这是一张追加式台账里的一行，不是账号此刻的状态——
 * `status` / `error_message` 都是**发现当时**的快照，号后来改名或恢复了它也不变。
 *
 * 三个时刻各答一个问题：detected_at 什么时候坏的、notified_at 信发出去了没有
 * （空 = 还没发，不是"发失败了"）、resolved_at 什么时候好的（空 = 现在还坏着）。
 */
export interface SupplyIncident {
  id: number
  account_id: number
  user_id: number
  account_name?: string
  platform?: string
  status: string
  error_message?: string
  detected_at: string
  notified_at?: string | null
  resolved_at?: string | null
}

/** 封禁率榜单的一行。rate = incidents / accounts，号全解绑的人这里是 0 而不是报错。 */
export interface SupplyIncidentRate {
  user_id: number
  email?: string
  username?: string
  accounts: number
  incidents: number
  open_incidents: number
  rate: number
  last_detected_at?: string | null
}

/**
 * 封禁率报表。
 *
 * `open` 与榜单里的 `open_incidents` **不带窗口**，其余三个数带——一个坏了三个月的
 * 号仍然要出现在"现在还有几个坏着"里。界面上因此不能把它们并排画成同一口径的四格
 * 而不加说明。
 */
export interface SupplyIncidentSummary {
  window_days: number
  opened: number
  resolved: number
  open: number
  accounts: number
  suppliers: number
  top: SupplyIncidentRate[]
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

/**
 * 读失效事件明细。
 *
 * `open` 只在为真时才传：后端认不出的值一律当 false（"认不出就是没筛"），
 * 传一个 `open=false` 与不传是同一个意思，但会让 URL 看起来像在筛什么。
 */
async function listIncidents(
  params: {
    page?: number
    page_size?: number
    user_id?: number
    account_id?: number
    open?: boolean
    start_at?: string
    end_at?: string
  } = {}
): Promise<SupplyAdminPage<SupplyIncident>> {
  const { open, ...rest } = params
  const { data } = await apiClient.get<SupplyAdminPage<SupplyIncident>>('/admin/supply/incidents', {
    params: open ? { ...rest, open: 'true' } : rest,
  })
  return data
}

/** 读封禁率报表。窗口与明细那张表的时间筛**互不影响**，两者各问各的。 */
async function getIncidentSummary(
  params: { window_days?: number; top?: number } = {}
): Promise<SupplyIncidentSummary> {
  const { data } = await apiClient.get<SupplyIncidentSummary>('/admin/supply/incidents/summary', {
    params,
  })
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

async function getOnboardingSettings(): Promise<SupplyOnboardingSettings> {
  const { data } = await apiClient.get<SupplyOnboardingSettings>('/admin/settings/supply-onboarding')
  return data
}

/**
 * 写接入上限。后端**夹回区间而不是报错**（与观察期参数同类），所以返回值一定要
 * 写回表单——运营看到自己填的 500 变成 100 的唯一途径就是这次回读。
 */
async function updateOnboardingSettings(
  payload: SupplyOnboardingPayload
): Promise<SupplyOnboardingSettings> {
  const { data } = await apiClient.put<SupplyOnboardingSettings>(
    '/admin/settings/supply-onboarding',
    payload
  )
  return data
}

async function getAgreementSettings(): Promise<SupplyAgreementSettings> {
  const { data } = await apiClient.get<SupplyAgreementSettings>('/admin/settings/supply-agreement')
  return data
}

/**
 * 写协议配置。
 *
 * 与观察期参数刻意不同：越界值后端**拒绝**而不是夹回（这是法律文本，静默截断
 * 比报错糟得多），所以这里的错误一定要原样弹给运营看，不能吞。
 */
async function updateAgreementSettings(
  payload: SupplyAgreementPayload
): Promise<SupplyAgreementSettings> {
  const { data } = await apiClient.put<SupplyAgreementSettings>('/admin/settings/supply-agreement', payload)
  return data
}

async function getWithdrawalSettings(): Promise<SupplyWithdrawalSettings> {
  const { data } = await apiClient.get<SupplyWithdrawalSettings>('/admin/settings/supply-withdrawal')
  return data
}

/**
 * 写提现参数。越界值后端**拒绝**而不是夹回（同协议那组），错误要原样弹出去。
 *
 * 开着却不给渠道也会被拒：那个组合唯一的效果是让供给者点一个必定失败的按钮。
 * 想关掉入口就把 enabled 设成 false，不要靠清空 channels 来间接关。
 */
async function updateWithdrawalSettings(
  payload: SupplyWithdrawalPayload
): Promise<SupplyWithdrawalSettings> {
  const { data } = await apiClient.put<SupplyWithdrawalSettings>('/admin/settings/supply-withdrawal', payload)
  return data
}

async function getPayoutChainSettings(): Promise<SupplyPayoutChainSettings> {
  const { data } = await apiClient.get<SupplyPayoutChainSettings>('/admin/settings/supply-payout-chain')
  return data
}

/** 保存即热换客户端：响应里的 status 是**换完之后**的装配结果。 */
async function updatePayoutChainSettings(
  payload: SupplyPayoutChainPayload
): Promise<SupplyPayoutChainSettings> {
  const { data } = await apiClient.put<SupplyPayoutChainSettings>(
    '/admin/settings/supply-payout-chain',
    payload
  )
  return data
}

/** 重新装配一次并向节点核链 ID——「测试连接」按钮。没有任何写入语义。 */
async function verifyPayoutChain(): Promise<SupplyPayoutChainSettings> {
  const { data } = await apiClient.post<SupplyPayoutChainSettings>(
    '/admin/settings/supply-payout-chain/verify'
  )
  return data
}

async function listWithdrawals(
  params: {
    page?: number
    page_size?: number
    status?: SupplyWithdrawalStatus | ''
    user_id?: number
  } = {}
): Promise<SupplyAdminPage<SupplyWithdrawalAdminView>> {
  const { data } = await apiClient.get<SupplyAdminPage<SupplyWithdrawalAdminView>>(
    '/admin/supply/withdrawals',
    { params }
  )
  return data
}

/**
 * 标记已打款。**不退款**——钱在申请那一刻就出可用区了，这一步只是记账收尾。
 *
 * external_ref 是打款凭证/交易号，可空（不是每种渠道都有）。留空的代价是纠纷
 * 时双方没有共同锚点，所以界面上该劝一句，但不该拦。
 */
async function markWithdrawalPaid(
  id: number,
  payload: { external_ref?: string; note?: string } = {}
): Promise<SupplyWithdrawalAdminView> {
  const { data } = await apiClient.post<SupplyWithdrawalAdminView>(
    `/admin/supply/withdrawals/${id}/paid`,
    payload
  )
  return data
}

/**
 * 拒绝一张单子，钱退回可用区。note 必填，后端强制——
 * 一个没有理由的拒绝，对供给者来说和系统故障没有区别。
 */
async function rejectWithdrawal(id: number, note: string): Promise<SupplyWithdrawalAdminView> {
  const { data } = await apiClient.post<SupplyWithdrawalAdminView>(
    `/admin/supply/withdrawals/${id}/reject`,
    { note }
  )
  return data
}

// ============================================================================
// 对账导出
// ============================================================================

/**
 * 一份导出文件的完整性判定。
 *
 * - `complete`：尾行在，没撞上限——这份文件可以拿去打款。
 * - `truncated`：尾行在，但后端说还有账没导出来。文件本身是好的，只是不全。
 * - `incomplete`：尾行不在。中途出错了，而流式响应那时已经回了 200，
 *   服务端没有别的地方能说这件事（见后端 supplier_export_csv.go 尾行一节）。
 */
export type SupplyExportState = 'complete' | 'truncated' | 'incomplete'

export interface SupplyExportFile {
  blob: Blob
  /** 落到磁盘上的文件名。已按 state 加过后缀，见 supplyExportFilename。 */
  filename: string
  state: SupplyExportState
  /** 尾行原文（没有尾行时为空串）。里面写着行数和窗口，值得原样给运营看。 */
  note: string
}

/** 尾行最多可能有多长——取文件末尾这么多字节就够判定了。 */
const SUPPLY_EXPORT_TAIL_BYTES = 4096

/**
 * 从 Content-Disposition 里取服务端定的文件名。
 *
 * 一定要用服务端那个：它把导出窗口写进了文件名（supply-ledger-20260721-20260820.csv），
 * 而这份文件在硬盘上躺三天之后，文件名是唯一还留在外面的上下文。
 */
function parseContentDispositionFilename(header: unknown): string {
  if (typeof header !== 'string') return ''
  const match = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i.exec(header)
  return match ? decodeURIComponent(match[1].trim()) : ''
}

/**
 * 把一个 blob 读成文本。读不出来给 null。
 *
 * 用 FileReader 而不是 `blob.text()`：后者在 jsdom 里根本不存在（它的 Blob 只有
 * slice/size/type），于是"尾行判定"这段代码在测试里就永远走不到。改用一个
 * 浏览器和 jsdom 都有的 API，测试跑的才是线上那条路。
 */
function readBlobText(blob: Blob): Promise<string | null> {
  return new Promise((resolve) => {
    const reader = new FileReader()
    reader.onload = () => resolve(typeof reader.result === 'string' ? reader.result : null)
    reader.onerror = () => resolve(null)
    reader.readAsText(blob)
  })
}

/**
 * 读尾行，判定这份文件完不完整。
 *
 * 只取末尾几 KB 而不是整份读成字符串：一次 20 万行的导出有几十 MB，
 * 为了看最后一行把它整个解码一遍是白烧运营那台机器的内存。
 *
 * 已知的一个理论误判：文件在"某个以 # 开头的行"处被截断时会被当成完整的。
 * 数据行的第一列全是数字 id，撞不上；能以 # 开头的只有尾行本身。
 */
async function readSupplyExportTrailer(blob: Blob): Promise<{ state: SupplyExportState; note: string }> {
  const tail = await readBlobText(blob.slice(Math.max(0, blob.size - SUPPLY_EXPORT_TAIL_BYTES)))
  if (tail === null) {
    // 读不出来就说不出这份文件完不完整。往"不完整"上判是刻意的：
    // 错判成完整的代价是有人照着一份残缺的对账文件打款，
    // 错判成残缺的代价是重导一次。
    return { state: 'incomplete', note: '' }
  }
  const lines = tail.split('\n').map((line) => line.trim().replace(/^"|"$/g, '')).filter(Boolean)
  const last = lines[lines.length - 1] ?? ''
  if (!last.startsWith('#')) {
    return { state: 'incomplete', note: '' }
  }
  const note = last.replace(/^#\s*,?\s*/, '').replace(/^"|"$/g, '')
  return { state: note.includes('TRUNCATED') ? 'truncated' : 'complete', note }
}

/**
 * 把完整性写进文件名。
 *
 * 提示条会消失，文件不会。一份残缺的对账文件躺在下载目录里，三天后谁都看不出
 * 它当时报过警——除非那件事就写在文件名上。
 */
function supplyExportFilename(base: string, kind: string, state: SupplyExportState): string {
  const name = base || `supply-${kind}.csv`
  if (state === 'complete') return name
  const suffix = state === 'truncated' ? '-TRUNCATED' : '-INCOMPLETE'
  return name.replace(/(\.csv)?$/i, (ext) => suffix + (ext || '.csv'))
}

async function fetchSupplyExport(
  path: string,
  kind: string,
  params: Record<string, string | number | undefined>
): Promise<SupplyExportFile> {
  // 服务不可用时后端在写响应头**之前**回一个 JSON 错误。responseType: 'blob'
  // 会让那个 JSON 也变成 blob，于是 axios 拦截器取不出 message——调用方因此
  // 只能报一句笼统的失败。这是流式下载换来的代价，不是遗漏。
  const response = await apiClient.get<Blob>(path, { params, responseType: 'blob' })
  const blob = response.data
  const { state, note } = await readSupplyExportTrailer(blob)
  return {
    blob,
    filename: supplyExportFilename(
      parseContentDispositionFilename(response.headers?.['content-disposition']),
      kind,
      state
    ),
    state,
    note,
  }
}

/** 导出提现单。窗口由 start_at/end_at 决定，与页面上那张表的筛子无关。 */
async function exportWithdrawals(
  params: { status?: string; user_id?: number; start_at?: string; end_at?: string } = {}
): Promise<SupplyExportFile> {
  return fetchSupplyExport('/admin/supply/export/withdrawals', 'withdrawals', params)
}

/** 导出全站钱包流水。 */
async function exportLedger(
  params: {
    user_id?: number
    action?: string
    account_id?: number
    request_id?: string
    start_at?: string
    end_at?: string
  } = {}
): Promise<SupplyExportFile> {
  return fetchSupplyExport('/admin/supply/export/ledger', 'ledger', params)
}

export const adminSupplyMarketAPI = {
  getOverview,
  listSuppliers,
  listAccounts,
  listLedger,
  listIncidents,
  getIncidentSummary,
  exportWithdrawals,
  exportLedger,
  getSettlementSettings,
  updateSettlementSettings,
  getPoolSettings,
  updatePoolSettings,
  getProbationSettings,
  updateProbationSettings,
  getOnboardingSettings,
  updateOnboardingSettings,
  getAgreementSettings,
  updateAgreementSettings,
  getWithdrawalSettings,
  getPayoutChainSettings,
  updatePayoutChainSettings,
  verifyPayoutChain,
  updateWithdrawalSettings,
  listWithdrawals,
  markWithdrawalPaid,
  rejectWithdrawal,
}

export default adminSupplyMarketAPI
