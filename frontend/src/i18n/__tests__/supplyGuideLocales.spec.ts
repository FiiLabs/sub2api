/**
 * APEXONE-EXT: 新手引导文案的三条底线。
 *
 * 1) zh/en 键完全一致——缺键在 vue-i18n 里不是报错，是把 key 本身画到界面上，
 *    而且只有切到那个语言的人才看得见。引导是新用户读到的第一段话，
 *    在这里露出一个 "supply.guide.step2.title" 比不做引导还糟。
 * 2) 步骤表里引用的每一个键都真的存在——SUPPLY_GUIDE_STEPS 里的键是拼出来的
 *    字符串，TS 不会替我们检查它们指向哪里。
 * 3) 文案里不出现任何金额承诺。这条不是风格偏好：我们能兑现的只有分成比例和
 *    提现方式，一个"月入 X"会在兑现不了的那天赔掉整个供给侧的信任。
 */
import { describe, expect, it } from 'vitest'

import { SUPPLY_GUIDE_STEPS } from '@/constants/supplyGuide'
import en from '../locales/en'
import zh from '../locales/zh'

function collectKeys(node: unknown, prefix = ''): string[] {
  if (node === null || typeof node !== 'object') return [prefix]
  return Object.entries(node as Record<string, unknown>).flatMap(([key, value]) =>
    collectKeys(value, prefix ? `${prefix}.${key}` : key)
  )
}

function resolve(locale: Record<string, unknown>, path: string): unknown {
  return path.split('.').reduce<unknown>((node, key) => (node as Record<string, unknown>)?.[key], locale)
}

describe('supply onboarding guide locales', () => {
  it('keeps the zh and en key sets identical', () => {
    expect(collectKeys(en.supply.guide).sort()).toEqual(collectKeys(zh.supply.guide).sort())
  })

  it('resolves every key the shared step table points at', () => {
    for (const locale of [zh, en] as unknown as Record<string, unknown>[]) {
      for (const step of SUPPLY_GUIDE_STEPS) {
        for (const key of [step.titleKey, step.descKey]) {
          const value = resolve(locale, key)
          expect({ key, type: typeof value }).toEqual({ key, type: 'string' })
          expect((value as string).length).toBeGreaterThan(0)
        }
      }
    }
  })

  it('numbers the steps 1..3 without gaps', () => {
    // 编号是文案的一部分：跳号的清单会让人以为自己漏读了一步。
    expect(SUPPLY_GUIDE_STEPS.map((step) => step.n)).toEqual([1, 2, 3])
  })

  it('always ships the "earnings may be limited" line', () => {
    for (const locale of [zh, en]) {
      expect(typeof locale.supply.guide.after3).toBe('string')
      expect(locale.supply.guide.after3.length).toBeGreaterThan(20)
    }
    // 两种语言各自钉一个关键片段：翻译可以润色，但"早期/有限"和"比例不是金额"
    // 这两层意思一层都不能在润色里蒸发。
    expect(zh.supply.guide.after3).toContain('分成比例')
    expect(zh.supply.guide.after3).toContain('有限')
    expect(en.supply.guide.after3).toContain('revenue share')
    expect(en.supply.guide.after3).toContain('limited')
  })

  it('promises no amounts anywhere in the guide', () => {
    // 分成"比例"可以出现（那是我们能兑现的），但金额、月收入、预估数字不行。
    const forbidden = [
      /\$\s*\d/,
      /\d+\s*(美元|元)/,
      /月入/,
      /预计收益/,
      /per month/i,
      /a month/i,
      /estimated earnings/i,
    ]
    for (const locale of [zh, en]) {
      const guide = locale.supply.guide as unknown as Record<string, unknown>
      for (const key of collectKeys(guide)) {
        const text = String(resolve(guide, key))
        for (const pattern of forbidden) {
          // 断言里带上 key 和文本：真的踩线时，报错要能直接指出是哪一句。
          expect({ key, matched: pattern.test(text), text }).toEqual({ key, matched: false, text })
        }
      }
    }
  })

  it('points the docs link at the public earn guide, identically in both languages', () => {
    // 两种语言指向同一篇：文档站只有一份，各写各的迟早会有一边指向 404。
    expect(zh.supply.guide.docsHref).toBe('https://docs.apex1.us/earn/share-subscription/')
    expect(en.supply.guide.docsHref).toBe(zh.supply.guide.docsHref)
  })
})
