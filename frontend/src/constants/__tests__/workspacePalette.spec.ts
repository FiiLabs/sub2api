import { describe, it, expect } from 'vitest'
import { paletteForSubject, PERSONAL_PALETTE, TEAM_PALETTES } from '@/constants/workspacePalette'
import type { WorkspaceSubject } from '@/types'

const team = (id: number): WorkspaceSubject =>
  ({ billing_subject_id: id, type: 'team', team_id: id, name: `T${id}`, role: 'admin', permissions: {}, balance: 0 })
const personal: WorkspaceSubject =
  { billing_subject_id: 99, type: 'user', name: 'Me', role: 'owner', permissions: {}, balance: 0 }

describe('paletteForSubject', () => {
  it('个人 / 空 → 中性灰', () => {
    expect(paletteForSubject(personal)).toBe(PERSONAL_PALETTE)
    expect(paletteForSubject(null)).toBe(PERSONAL_PALETTE)
    expect(paletteForSubject(undefined)).toBe(PERSONAL_PALETTE)
  })
  it('团队 → 按 team_id 稳定取色', () => {
    const p = paletteForSubject(team(1))
    expect(p).toBe(TEAM_PALETTES[1 % TEAM_PALETTES.length])
    expect(paletteForSubject(team(1))).toBe(p)
  })
  it('团队 → 优先用 team_id 而非 billing_subject_id', () => {
    const s = { billing_subject_id: 99, type: 'team', team_id: 2, name: 'T', role: 'admin', permissions: {}, balance: 0 } as WorkspaceSubject
    expect(paletteForSubject(s)).toBe(TEAM_PALETTES[2 % TEAM_PALETTES.length])
  })
  it('团队缺 team_id → 回退 billing_subject_id', () => {
    const s = { billing_subject_id: 5, type: 'team', team_id: undefined, name: 'T', role: 'admin', permissions: {}, balance: 0 } as WorkspaceSubject
    expect(paletteForSubject(s)).toBe(TEAM_PALETTES[5 % TEAM_PALETTES.length])
  })
  it('色盘剔除品牌紫与危险红', () => {
    const keys = [PERSONAL_PALETTE, ...TEAM_PALETTES].map((p) => p.key)
    for (const banned of ['violet', 'purple', 'indigo', 'red', 'fuchsia']) {
      expect(keys).not.toContain(banned)
    }
  })
})
