import type { WorkspaceSubject } from '@/types'

/** 工作区身份配色：所有类名为完整字面量，规避 Tailwind JIT purge。 */
export interface WorkspacePalette {
  key: string          // 色家族名（测试断言/水印用）
  chip: string         // 芯片底+字（含 dark:）
  icon: string         // 图标/圆点色（plain 变体用）
  border: string       // 边框色
  watermarkText: string// 水印文字色
}

export const PERSONAL_PALETTE: WorkspacePalette = {
  key: 'gray',
  chip: 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-dark-200',
  icon: 'text-gray-500 dark:text-dark-300',
  border: 'border-gray-200 dark:border-dark-600',
  watermarkText: 'text-gray-400 dark:text-dark-500'
}

/** 团队色盘：剔除 紫/靛(近 primary) 与 红(danger)；团队彼此可分。 */
export const TEAM_PALETTES: WorkspacePalette[] = [
  { key: 'emerald', chip: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300', icon: 'text-emerald-600 dark:text-emerald-400', border: 'border-emerald-200 dark:border-emerald-500/30', watermarkText: 'text-emerald-500 dark:text-emerald-400' },
  { key: 'teal',    chip: 'bg-teal-50 text-teal-700 dark:bg-teal-500/15 dark:text-teal-300',             icon: 'text-teal-600 dark:text-teal-400',       border: 'border-teal-200 dark:border-teal-500/30',       watermarkText: 'text-teal-500 dark:text-teal-400' },
  { key: 'sky',     chip: 'bg-sky-50 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300',                 icon: 'text-sky-600 dark:text-sky-400',         border: 'border-sky-200 dark:border-sky-500/30',         watermarkText: 'text-sky-500 dark:text-sky-400' },
  { key: 'blue',    chip: 'bg-blue-50 text-blue-700 dark:bg-blue-500/15 dark:text-blue-300',             icon: 'text-blue-600 dark:text-blue-400',       border: 'border-blue-200 dark:border-blue-500/30',       watermarkText: 'text-blue-500 dark:text-blue-400' },
  { key: 'amber',   chip: 'bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',         icon: 'text-amber-600 dark:text-amber-400',     border: 'border-amber-200 dark:border-amber-500/30',     watermarkText: 'text-amber-500 dark:text-amber-400' },
  { key: 'orange',  chip: 'bg-orange-50 text-orange-700 dark:bg-orange-500/15 dark:text-orange-300',     icon: 'text-orange-600 dark:text-orange-400',   border: 'border-orange-200 dark:border-orange-500/30',   watermarkText: 'text-orange-500 dark:text-orange-400' },
  { key: 'rose',    chip: 'bg-rose-50 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300',             icon: 'text-rose-600 dark:text-rose-400',       border: 'border-rose-200 dark:border-rose-500/30',       watermarkText: 'text-rose-500 dark:text-rose-400' },
  { key: 'fuchsia', chip: 'bg-fuchsia-50 text-fuchsia-700 dark:bg-fuchsia-500/15 dark:text-fuchsia-300', icon: 'text-fuchsia-600 dark:text-fuchsia-400', border: 'border-fuchsia-200 dark:border-fuchsia-500/30', watermarkText: 'text-fuchsia-500 dark:text-fuchsia-400' }
]

/** 个人→中性；团队→按 team_id（回退 billing_subject_id）稳定取色。 */
export function paletteForSubject(subject: WorkspaceSubject | null | undefined): WorkspacePalette {
  if (!subject || subject.type !== 'team') return PERSONAL_PALETTE
  const seed = subject.team_id ?? subject.billing_subject_id
  return TEAM_PALETTES[Math.abs(seed) % TEAM_PALETTES.length]
}
