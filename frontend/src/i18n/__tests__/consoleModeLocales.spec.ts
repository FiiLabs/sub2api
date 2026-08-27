/**
 * APEXONE-EXT: 双入口 + 双模式的文案对齐。
 *
 * 缺键在 vue-i18n 里不是报错，是把 key 本身画到界面上——而且只有切到那个语言
 * 才看得见。首页 hero 是第一屏，控制台模式名是一个只有两个词的开关：
 * 这两处漏一个键，看到的人不会觉得"少了段翻译"，只会觉得这个站没做完。
 */
import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

function collectKeys(node: unknown, prefix = ''): string[] {
  if (node === null || typeof node !== 'object') return [prefix]
  return Object.entries(node as Record<string, unknown>).flatMap(([key, value]) =>
    collectKeys(value, prefix ? `${prefix}.${key}` : key)
  )
}

describe('two-sided entry & console mode locales', () => {
  it('keeps the hero key sets identical across zh and en', () => {
    expect(collectKeys(en.home.landing.hero).sort()).toEqual(collectKeys(zh.home.landing.hero).sort())
  })

  it('gives both hero entry cards a title, a subline and a call to action', () => {
    for (const locale of [zh, en]) {
      const hero = locale.home.landing.hero
      // 消费侧的 CTA 复用 home.getStarted / home.goToDashboard，所以只有三条自己的。
      for (const value of [hero.useAI, hero.useAIDesc, hero.shareSubscription, hero.shareSubscriptionDesc, hero.shareSubscriptionCta]) {
        expect(typeof value).toBe('string')
        expect((value as string).length).toBeGreaterThan(0)
      }
      // 副文案不能退化成标题的复读：卡片上两行说同一句话，不如只放一行。
      expect(hero.useAIDesc).not.toBe(hero.useAI)
      expect(hero.shareSubscriptionDesc).not.toBe(hero.shareSubscription)
    }
  })

  it('names the two modes distinctly in both languages', () => {
    for (const locale of [zh, en]) {
      const console_ = locale.supply.console
      expect(console_.usageMode).toBeTruthy()
      expect(console_.sharingMode).toBeTruthy()
      expect(console_.usageMode).not.toBe(console_.sharingMode)
    }
  })

  it('calls the same pot of money by the same name on both pages', () => {
    // 「待提现收益」在 /supply 和 /dashboard 上必须是同一个词。不同名的话，
    // 同时供给又消费的人会以为自己有两笔钱。
    for (const locale of [zh, en]) {
      expect(locale.supply.console.available).toBe(locale.supply.wallet.available)
    }
  })
})
