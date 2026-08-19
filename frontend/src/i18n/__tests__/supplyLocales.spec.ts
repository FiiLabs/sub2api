/**
 * APEXONE-EXT: 双边市场文案的两条底线。
 *
 * 1) zh/en 键必须完全一致——缺键在 vue-i18n 里不是报错而是把 key 本身画到界面上，
 *    只有切到那个语言才看得见。
 * 2) supply 模块不能吃掉上游命名空间——locale 模块是被 spread 进同一个对象的，
 *    顶层键撞名（比如再写一个 nav）会整段替换掉原来的，静默且大面积。
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

describe('supply locale modules', () => {
  it('has identical key sets in zh and en', () => {
    for (const ns of ['supply', 'supplyAdmin'] as const) {
      const zhKeys = collectKeys(zh[ns]).sort()
      const enKeys = collectKeys(en[ns]).sort()
      expect({ ns, keys: enKeys }).toEqual({ ns, keys: zhKeys })
    }
  })

  it('does not clobber the upstream nav namespace', () => {
    // supply 的菜单文案刻意放在自己的命名空间里（supply.navLabel），
    // 如果哪天有人挪进 common.ts 的 nav，这条会先炸。
    for (const locale of [zh, en]) {
      expect(locale.nav.dashboard).toBeTruthy()
      expect(locale.nav.settings).toBeTruthy()
      expect(locale.supply.navLabel).toBeTruthy()
      expect(locale.supplyAdmin.navLabel).toBeTruthy()
    }
  })
})
