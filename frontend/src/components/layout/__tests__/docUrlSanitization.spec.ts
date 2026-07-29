import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const headerSource = readFileSync(resolve(dir, '../AppHeader.vue'), 'utf8')
const homeViewSource = readFileSync(resolve(dir, '../../../views/HomeView.vue'), 'utf8')
const keyUsageViewSource = readFileSync(resolve(dir, '../../../views/KeyUsageView.vue'), 'utf8')

describe('doc_url sanitization', () => {
  it('AppHeader imports sanitizeUrl', () => {
    expect(headerSource).toContain("import { sanitizeUrl } from '@/utils/url'")
  })

  it('AppHeader applies sanitizeUrl to docUrl', () => {
    expect(headerSource).toContain('sanitizeUrl(appStore.docUrl)')
  })

  // HomeView 重写为 ApexOne 落地页后已不再渲染 doc_url（对 appStore 只剩
  // publicSettingsLoaded / fetchPublicSettings，模板里没有任何动态 :href），
  // 因此没有可消毒的东西。这条反向守卫保证它不会在无人注意时把 docUrl 重新
  // 引进来 —— 真要引入，就必须同时走 sanitizeUrl，届时改回正向断言。
  it('HomeView does not consume docUrl, so there is nothing to sanitize', () => {
    expect(homeViewSource).not.toContain('docUrl')
    expect(homeViewSource).not.toContain('doc_url')
  })

  it('KeyUsageView imports sanitizeUrl', () => {
    expect(keyUsageViewSource).toContain("import { sanitizeUrl } from '@/utils/url'")
  })

  it('KeyUsageView applies sanitizeUrl to docUrl', () => {
    expect(keyUsageViewSource).toContain('sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl')
  })
})
