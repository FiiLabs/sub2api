/**
 * APEXONE-EXT: 供给侧 API 的线上契约。
 *
 * 这里盯的是"发出去的请求长什么样"，因为下线通道错一个字段就是一次
 * 不可撤销的误操作：mode 丢了后端按 graceful 兜底，用户点的却是"立即下线"。
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, del } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  del: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post, delete: del },
}))

import { supplyAPI } from '@/api/supply'
import { adminSupplyMarketAPI } from '@/api/admin/supplyMarket'

describe('supply api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    del.mockReset()
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
    del.mockResolvedValue({ data: { detached: true, upstream_revoke_required: true } })
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

  it('detaches with DELETE on the account itself, not a verb sub-path', async () => {
    // 解绑不是 pause 那样的状态变更，它把资源本身摘掉了。用 DELETE /accounts/:id
    // 而不是 POST /accounts/:id/detach，是为了让「这一步没有回头路」在路由层就成立：
    // 没有一个对称的 POST 能把它撤回来。
    await supplyAPI.detachAccount(42)

    expect(del).toHaveBeenCalledWith('/user/supply/accounts/42')
    expect(post).not.toHaveBeenCalled()
  })

  it('passes the upstream-revoke flag through instead of hard-coding it', async () => {
    // 后端将来真能远端撤销时，这个提示要能一次性关掉。前端写死 true 会让那天
    // 的改动变成"再找一遍所有提示文案"。
    del.mockResolvedValue({ data: { detached: true, upstream_revoke_required: false } })

    await expect(supplyAPI.detachAccount(7)).resolves.toEqual({
      detached: true,
      upstream_revoke_required: false,
    })
  })

  it('reads the agreement without any parameter', async () => {
    await supplyAPI.getAgreement()

    expect(get).toHaveBeenCalledWith('/user/supply/agreement')
  })

  it('sends the version the user actually saw when accepting', async () => {
    // 不能省掉 version 让服务端自己取「当前版本」：那样只能证明他点了按钮，
    // 证明不了他看的是哪一版。页面开着没刷新的人点下去，就会把新版协议
    // 记成"已同意"——协议的证据链在这一个字段上。
    await supplyAPI.acceptAgreement('v2')

    expect(post).toHaveBeenCalledWith('/user/supply/agreement/accept', { version: 'v2' })
  })

  it('reads the withdrawal form prerequisites in one call', async () => {
    // 余额、渠道、未决单数必须来自同一时刻。拆成三个请求会画出
    //「余额 100、渠道空着、按钮还亮着」这种自相矛盾的界面。
    await supplyAPI.getWithdrawalOptions()

    expect(get).toHaveBeenCalledWith('/user/supply/withdrawals/options')
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('sends the withdrawal amount as a number, not a string', async () => {
    // 后端的 amount 是 *float64 且不带 binding:"required"：0 会被明确拒成
    //「金额必须为正」，而不是当成"没填"。前端把它转成字符串会让那道校验失效。
    await supplyAPI.requestWithdrawal({
      amount: 50,
      payout_channel: 'USDT',
      payout_account: 'T-addr',
      user_note: 'x',
    })

    expect(post).toHaveBeenCalledWith('/user/supply/withdrawals', {
      amount: 50,
      payout_channel: 'USDT',
      payout_account: 'T-addr',
      user_note: 'x',
    })
    expect(typeof post.mock.calls[0][1].amount).toBe('number')
  })

  it('cancels a withdrawal with POST, not DELETE', async () => {
    // 单子不会消失，它变成 canceled 留在列表里——那行记录本身是对账凭据。
    // 用 DELETE 会让「撤回等于删掉」这个错误理解在路由层就成立。
    await supplyAPI.cancelWithdrawal(9)

    expect(post).toHaveBeenCalledWith('/user/supply/withdrawals/9/cancel')
    expect(del).not.toHaveBeenCalled()
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

  it('writes only the three editable agreement fields, never published/*_max_len', async () => {
    // published 和三个 *_max_len 都是后端算出来的只读字段。把它们 PUT 回去，
    // 管理员就在声称自己能决定协议算不算已发布——而那件事只由 version 是否为空决定。
    const put = vi.fn().mockResolvedValue({ data: {} })
    const client = (await import('@/api/client')).apiClient as unknown as Record<string, unknown>
    client.put = put

    await adminSupplyMarketAPI.getAgreementSettings()
    expect(get).toHaveBeenCalledWith('/admin/settings/supply-agreement')

    await adminSupplyMarketAPI.updateAgreementSettings({ version: 'v1', url: '', body: '正文' })
    expect(put).toHaveBeenCalledWith('/admin/settings/supply-agreement', {
      version: 'v1',
      url: '',
      body: '正文',
    })
  })

  it('writes the withdrawal notify recipients, never the derived notify_configured', async () => {
    // notify_emails 是配置，notify_configured 是后端由 enabled + 收件人算出来的
    // 只读结论。把后者 PUT 回去，管理员就在声称"通知已配好"——而那件事只由
    // 收件人列表是否为空决定。同理 available 与那两个 *_max。
    const put = vi.fn().mockResolvedValue({ data: {} })
    const client = (await import('@/api/client')).apiClient as unknown as Record<string, unknown>
    client.put = put

    await adminSupplyMarketAPI.updateWithdrawalSettings({
      enabled: true,
      min_amount: 100,
      max_pending: 3,
      channels: ['USDT'],
      notice: '',
      notify_emails: ['finance@example.com'],
    })

    expect(put).toHaveBeenCalledWith('/admin/settings/supply-withdrawal', {
      enabled: true,
      min_amount: 100,
      max_pending: 3,
      channels: ['USDT'],
      notice: '',
      notify_emails: ['finance@example.com'],
    })
  })

  it('keeps an empty recipient list in the payload — it means "notify nobody"', async () => {
    // 与 daily_overflow_limit: 0 同理，但后果更重：被过滤掉的话后端当成"没填"
    // 而保留旧收件人，于是管理员以为自己把通知关了，信还在发给已经离职的人。
    const put = vi.fn().mockResolvedValue({ data: {} })
    const client = (await import('@/api/client')).apiClient as unknown as Record<string, unknown>
    client.put = put

    await adminSupplyMarketAPI.updateWithdrawalSettings({
      enabled: false,
      min_amount: 100,
      max_pending: 3,
      channels: [],
      notice: '',
      notify_emails: [],
    })

    expect(put).toHaveBeenCalledWith(
      '/admin/settings/supply-withdrawal',
      expect.objectContaining({ notify_emails: [] })
    )
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

  it('exposes no write path for the ops view beyond settings and withdrawal review', () => {
    // 这一刀刻意不给管理端改供给侧数据的能力。多出来的任何一个写方法都该
    // 先经过一次设计讨论，而不是顺手加在这个 API 对象上。
    //
    // markWithdrawalPaid / rejectWithdrawal 是**唯一**动业务数据（而非配置）的两个：
    // 一张已经扣了钱的单子必须有人能推进它。它们进这份名单是一次明确的决定，
    // 不是这条规则松了口子——别的写方法仍然要先讨论。
    const writers = Object.keys(adminSupplyMarketAPI).filter((key) =>
      /^(update|create|delete|mark|reject|approve)/.test(key)
    )
    expect(writers.sort()).toEqual([
      'markWithdrawalPaid',
      'rejectWithdrawal',
      'updateAgreementSettings',
      'updatePoolSettings',
      'updateProbationSettings',
      'updateSettlementSettings',
      'updateWithdrawalSettings',
    ])
  })

  it('lists withdrawals through the same read path as the rest of the board', async () => {
    await adminSupplyMarketAPI.listWithdrawals({ status: 'pending', page: 2 })

    expect(get).toHaveBeenCalledWith('/admin/supply/withdrawals', {
      params: { status: 'pending', page: 2 },
    })
  })
})

// 提现审批是管理端唯一动业务数据的写路径，所以单开一个 describe：上面那个
// describe 的名字里写着 read-only，把两个 POST 塞进去会让那句话变成假的。
describe('admin withdrawal review (the one write path)', () => {
  beforeEach(() => {
    post.mockReset()
    post.mockResolvedValue({ data: { id: 12, status: 'paid' } })
  })

  it('rejects a withdrawal with a reason in the body', async () => {
    // note 后端强制必填：一个没有理由的拒绝，对供给者来说和系统故障没有区别。
    await adminSupplyMarketAPI.rejectWithdrawal(12, '收款账号与实名不符')

    expect(post).toHaveBeenCalledWith('/admin/supply/withdrawals/12/reject', {
      note: '收款账号与实名不符',
    })
  })

  it('marks paid with an empty body when no proof is given', async () => {
    // 不是每种渠道都有交易号。缺省成 {} 而不是 undefined，是为了让后端那边
    //「body 绑定失败当成空 body」的兜底不必被前端依赖。
    await adminSupplyMarketAPI.markWithdrawalPaid(12)

    expect(post).toHaveBeenCalledWith('/admin/supply/withdrawals/12/paid', {})
  })

  it('never routes payment through the reject endpoint', async () => {
    // 两个动作的退款语义正好相反（拒绝退钱、打款不退），路径写错一个字
    // 就是凭空发一笔钱出去。
    await adminSupplyMarketAPI.markWithdrawalPaid(12, { external_ref: 'tx-1' })

    expect(post).toHaveBeenCalledWith('/admin/supply/withdrawals/12/paid', { external_ref: 'tx-1' })
    expect(post).not.toHaveBeenCalledWith(
      expect.stringContaining('/reject'),
      expect.anything()
    )
  })
})
