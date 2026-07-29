import { describe, expect, it, vi } from 'vitest'

import fixture from './fixtures/live-quote-proof-v11.json'

/**
 * 用 2026-07-28 从线上 attestor 抓的真实 nonce 绑定 quote，锁住迁移到
 * `@phala/dcap-qvl` 之后的行为。
 *
 * 只把 `getCollateral` 换成夹具（否则要联网打 Intel PCCS），`verify` 用真的 ——
 * 也就是说签名链、TCB 判定、以及这次迁移真正的目的 QE Identity 签名校验
 * （CVE-2026-22696 里旧 `-web` 包缺失的那一步）都在这条测试里实际执行。
 *
 * `nowSecs` 取抓取当时的时间戳并写死：collateral 有 `nextUpdate` 过期检查，
 * 不冻结时钟的话这条测试会在几周后自己变红。
 */
const collateral = (() => {
  const byteFields = new Set(fixture.byteFields)
  const out: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(fixture.collateral)) {
    out[key] = byteFields.has(key)
      ? Uint8Array.from((value as string).match(/../g)!.map((b) => parseInt(b, 16)))
      : value
  }
  return out
})()

vi.mock('@phala/dcap-qvl', async () => {
  const actual = await vi.importActual<typeof import('@phala/dcap-qvl')>('@phala/dcap-qvl')
  return { ...actual, getCollateral: vi.fn(async () => collateral) }
})

const { verifyTdxQuote } = await import('../tdxVerify')

describe('verifyTdxQuote against a real attestor quote', () => {
  it('verifies the live proof-v11 quote and extracts every register', async () => {
    const result = await verifyTdxQuote(fixture.quoteHex, { nowSecs: fixture.nowSecs })

    expect(result.verified).toBe(true)
    expect(result.tcbStatus).toBe(fixture.expected.status)
    expect(result.advisoryIds).toEqual(fixture.expected.advisoryIds)

    expect(result.report).toEqual({
      kind: fixture.expected.kind,
      reportDataHex: fixture.expected.reportDataHex,
      mrTdHex: fixture.expected.mrTdHex,
      mrConfigIdHex: fixture.expected.mrConfigIdHex,
      rtMr0Hex: fixture.expected.rtMr0Hex,
      rtMr1Hex: fixture.expected.rtMr1Hex,
      rtMr2Hex: fixture.expected.rtMr2Hex,
      rtMr3Hex: fixture.expected.rtMr3Hex,
    })
  })

  it('binds the quote to the nonce we asked for', async () => {
    const { report } = await verifyTdxQuote(fixture.quoteHex, { nowSecs: fixture.nowSecs })
    // report_data 的前 32 字节就是请求里的 nonce，后 32 字节是 e2ee 公钥摘要。
    expect(report.reportDataHex.slice(0, 64)).toBe(fixture.nonceHex)
  })

  it('carries the published proof-v11 composeHash in mr_config_id', async () => {
    const { report } = await verifyTdxQuote(fixture.quoteHex, { nowSecs: fixture.nowSecs })
    // dstack 的构造是 mr_config_id = 01 || compose_hash || 0*，
    // 所以 composeHash 是第 2..34 字节。
    expect(report.mrConfigIdHex!.slice(0, 2)).toBe('01')
    expect(report.mrConfigIdHex!.slice(2, 66)).toBe(
      '0fa1adbf2fd01c067ede2cd98350b98a123b3f4ba68b989ee74ed17f94aa4e10',
    )
    expect(report.mrConfigIdHex!.slice(66)).toMatch(/^0+$/)
  })

  it('rejects a quote whose measurements were tampered with', async () => {
    // quote 布局：48 字节头 + 584 字节 TD report body + 签名区。翻掉 report body
    // 里的一个字节（第 100 字节落在 mrTd 上），ECDSA 签名必然对不上。
    // 这条守的是"验证确实发生了"——迁移的全部意义就在于此，若哪天验证被绕过，
    // 前面三条断言仍会全绿，只有这条会红。
    const BYTE_OFFSET = 100
    const i = BYTE_OFFSET * 2
    const original = fixture.quoteHex.slice(i, i + 2)
    const tampered =
      fixture.quoteHex.slice(0, i) + (original === 'ff' ? '00' : 'ff') + fixture.quoteHex.slice(i + 2)
    expect(tampered).not.toBe(fixture.quoteHex)

    await expect(verifyTdxQuote(tampered, { nowSecs: fixture.nowSecs })).rejects.toThrow()
  })
})
