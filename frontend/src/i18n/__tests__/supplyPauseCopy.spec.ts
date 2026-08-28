/**
 * APEXONE-EXT: 下线相关文案必须说清「不会自动恢复」。
 *
 * 由来是一个真实的供给者提问：「take offline 之后，账号是不是不自己上线、
 * 得我手动开回来？」——他问了，说明界面没讲清楚。
 *
 * 这个误解的代价是不对称的：以为会自动恢复的人，下线之后就走开了，
 * 然后白白少赚好几天；而知道要手动挂回的人，最多多点一次按钮。
 * 所以这几处文案必须**主动**说出这件事，不能留给用户推断。
 *
 * 这条测试守的是「下一次有人精简文案时，先让它变红」。
 */
import { describe, expect, it } from 'vitest'

import en from '../locales/en/supply'
import zh from '../locales/zh/supply'

const enAccounts = en.supply.accounts as Record<string, string>
const zhAccounts = zh.supply.accounts as Record<string, string>

describe('下线文案必须说明不会自动恢复', () => {
  // 英文：把「直到你手动挂回」这层意思落到具体措辞上。
  it.each([
    ['pauseTitle', /stays offline until you put it back/i],
    ['pauseNowTitle', /stays offline until you put it back/i],
    ['pauseConfirm', /does not come back automatically/i],
    ['pauseHint', /neither one resumes on its own/i],
  ])('en.%s 说明了不会自动恢复', (key, pattern) => {
    expect(enAccounts[key]).toMatch(pattern)
  })

  it.each([
    ['pauseTitle', /不会自己恢复/],
    ['pauseNowTitle', /直到你手动挂回/],
    ['pauseConfirm', /不会自动恢复/],
    ['pauseHint', /都不会自己恢复/],
  ])('zh.%s 说明了不会自动恢复', (key, pattern) => {
    expect(zhAccounts[key]).toMatch(pattern)
  })

  // 「立即下线」还要额外说清不可撤销——它是两个里代价更高的那个，
  // 而且按钮上写的就是 (can't undo)，文案不能与按钮自相矛盾。
  it('立即下线明确写了不可撤销', () => {
    expect(enAccounts.pauseNowTitle).toMatch(/cannot be undone/i)
    expect(zhAccounts.pauseNowTitle).toMatch(/不可撤销/)
    expect(enAccounts.pauseNow).toMatch(/can't undo/i)
    expect(zhAccounts.pauseNow).toMatch(/不可撤销/)
  })

  // 10 分钟窗口是「可取消」窗口，不是「到点自动回来」的计时器。
  // 文案里不该出现任何暗示自动恢复的说法。
  it('不出现暗示自动恢复的措辞', () => {
    for (const key of ['pauseTitle', 'pauseNowTitle', 'pauseConfirm', 'pauseHint']) {
      expect(enAccounts[key]).not.toMatch(/automatically (come|comes|resume|resumes) back online/i)
      expect(zhAccounts[key]).not.toMatch(/自动重新上线|自动恢复接单/)
    }
  })
})
