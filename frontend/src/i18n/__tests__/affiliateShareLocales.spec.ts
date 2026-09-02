/**
 * APEXONE-EXT: 邀请分享文案里的对外宣称。
 *
 * 这三条推荐语跟落地页文案是同一类东西——用户点一下复制，然后**发给别人看**。
 * 区别在于落地页有人天天看，而这里藏在返利页的一个折叠区里：写错了没人会
 * 当场发现，它只是被一次次地转发出去。
 *
 * 2026-09-02 就是这么烂掉的：倍率在 2026-08 从 0.22 改到 0.14，落地页、
 * 文档站、GitBook 都跟着改了，唯独这三条一直挂着更早的「五折 / half the
 * official API price」。等于每分享一次就对外报一次我们不收的价。
 *
 * 所以这里钉的不是措辞，是**数字**：现价必须出现，任何历史口径都不许残留。
 * 下次调价时这个 spec 会红，那正是它的用途。
 */
import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

type Dict = Record<string, any>

const zhMessages = (zh as Dict).affiliate.share.messages as Record<string, string>
const enMessages = (en as Dict).affiliate.share.messages as Record<string, string>

/** 当前对外口径。改价时改这两个常量，测试会把该跟着动的地方全部指出来。 */
const CURRENT_DISCOUNT_ZH = '1.4 折'
const CURRENT_DISCOUNT_EN = '14%'

/**
 * 历史价格口径。只增不删——留着才能保证旧数字不会从某个角落被复制回来。
 * 「五折 / half the price」来自最早那版，「2.2 折 / 22%」是 2026-08 之前的。
 */
const STALE_PRICING_ZH = ['五折', '五 折', '2.2 折', '22%', '2.2折']
const STALE_PRICING_EN = [
  'half the official',
  'half the price',
  'half of the official',
  '50% of the official',
  '22% of the official',
  '22% of official'
]

describe('邀请分享文案', () => {
  it('zh 与 en 的条目一一对应', () => {
    expect(Object.keys(zhMessages).sort()).toEqual(Object.keys(enMessages).sort())
  })

  // 少了占位符，分享出去的就是一段没有链接的推荐语——返利也就无从归因。
  it('每条都带 {inviteLink} 占位符', () => {
    for (const [key, text] of Object.entries({ ...zhMessages })) {
      expect(text, `zh.${key}`).toContain('{inviteLink}')
    }
    for (const [key, text] of Object.entries({ ...enMessages })) {
      expect(text, `en.${key}`).toContain('{inviteLink}')
    }
  })

  it('提到价格的那条用的是现价', () => {
    expect(zhMessages.price).toContain(CURRENT_DISCOUNT_ZH)
    expect(enMessages.price).toContain(CURRENT_DISCOUNT_EN)
  })

  it('任何历史价格口径都不残留', () => {
    const zhAll = Object.values(zhMessages).join('\n')
    const enAll = Object.values(enMessages).join('\n')

    for (const stale of STALE_PRICING_ZH) {
      expect(zhAll, `zh 里出现了旧口径「${stale}」`).not.toContain(stale)
    }
    for (const stale of STALE_PRICING_EN) {
      expect(enAll.toLowerCase(), `en 里出现了旧口径「${stale}」`).not.toContain(stale.toLowerCase())
    }
  })

  // 价格只与「官方 API」比——与 i18n/locales/*/home.ts 顶部那条红线同源：
  // 点名对手会把定价决策暴露成对某一家的应激反应。这里刻意不列竞品名单，
  // 只要求出现「官方」这个参照物。
  it('价格的参照物是官方 API', () => {
    expect(zhMessages.price).toContain('官方')
    expect(enMessages.price.toLowerCase()).toContain('official')
  })

  // 模型版本也是对外宣称。写着一个我们没上线的型号，或者停在一个早已过时的
  // 型号，都会让转发出去的话失真。
  it('模型版本与线上一致', () => {
    const zhAll = Object.values(zhMessages).join('\n')
    const enAll = Object.values(enMessages).join('\n')

    expect(zhAll).toContain('Claude Fable 5.1')
    expect(enAll).toContain('Claude Fable 5.1')
    // 「Fable 5」后面不跟 .1 就是旧型号。
    expect(zhAll).not.toMatch(/Fable 5(?!\.1)/)
    expect(enAll).not.toMatch(/Fable 5(?!\.1)/)
  })
})
