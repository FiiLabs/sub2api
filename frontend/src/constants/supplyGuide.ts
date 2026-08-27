/**
 * APEXONE-EXT: 供给侧「三步开始赚钱」的步骤表。
 *
 * 抽出来共享，是因为这三步会在两处露面（/supply 的完整版、控制台共享模式的
 * 精简版）。抄两份的代价不是多几行 markup，而是编号与文案的对应关系存了两份：
 * 哪天加一步或换顺序，漏改的那一处会画出「2. 等待核验 / 3. 接入订阅」这种
 * 自相矛盾的清单——而这正是新用户唯一会照着做的东西。
 *
 * 这里只放 i18n 键不放文案：两处的语言切换必须同时生效。
 */
export interface SupplyGuideStep {
  /** 展示用的序号。写死而不是用数组下标：它是文案的一部分，要能被搜到。 */
  readonly n: number
  readonly titleKey: string
  readonly descKey: string
}

export const SUPPLY_GUIDE_STEPS: readonly SupplyGuideStep[] = [
  { n: 1, titleKey: 'supply.guide.step1.title', descKey: 'supply.guide.step1.desc' },
  { n: 2, titleKey: 'supply.guide.step2.title', descKey: 'supply.guide.step2.desc' },
  { n: 3, titleKey: 'supply.guide.step3.title', descKey: 'supply.guide.step3.desc' }
]
