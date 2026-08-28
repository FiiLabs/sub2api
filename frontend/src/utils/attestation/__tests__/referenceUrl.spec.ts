/**
 * APEXONE-EXT: reference.json 的分支必须跟发布分支一致。
 *
 * 这条的由来是一次真实的错配：URL 指向 proof-solo，而镜像构建自
 * feat/two-sided-market。两边只靠「有人记得把同一个文件提交两次」维持一致，
 * 而一旦漂移，/proof 会把一个健康的部署报成 **enclave 被篡改**——
 * 一份描述着另一个构建的参考值，比没有参考值更糟。
 *
 * 所以两个 URL（权威源 + CDN 镜像）必须指向同一个分支。搬发布分支时，
 * 这条测试就是提醒你把 URL 一起搬的那个东西。
 */
import { describe, expect, it } from 'vitest'

import { REFERENCE_JSON_MIRROR_URLS, REFERENCE_JSON_URL } from '@/constants/attestation'

function branchOf(url: string): string {
  // raw.githubusercontent.com/<owner>/<repo>/<branch...>/deploy/...
  const raw = url.match(/raw\.githubusercontent\.com\/[^/]+\/[^/]+\/(.+?)\/deploy\//)
  if (raw) return raw[1]
  // cdn.jsdelivr.net/gh/<owner>/<repo>@<branch>/deploy/...
  const cdn = url.match(/cdn\.jsdelivr\.net\/gh\/[^/]+\/[^@]+@(.+?)\/deploy\//)
  if (cdn) return cdn[1]
  throw new Error(`无法从 URL 解析分支: ${url}`)
}

describe('reference.json 的来源', () => {
  it('权威源与所有 CDN 镜像指向同一个分支', () => {
    const authoritative = branchOf(REFERENCE_JSON_URL)
    for (const mirror of REFERENCE_JSON_MIRROR_URLS) {
      expect(branchOf(mirror)).toBe(authoritative)
    }
  })

  it('指向的是实际发布镜像的那个分支', () => {
    // 与 .github/workflows 里 publish-tee-images 的触发分支保持一致。
    // 换发布分支时这两处要一起改——这条断言就是那个提醒。
    expect(branchOf(REFERENCE_JSON_URL)).toBe('feat/two-sided-market')
  })

  it('都指向同一个仓库路径下的 reference.json', () => {
    for (const url of [REFERENCE_JSON_URL, ...REFERENCE_JSON_MIRROR_URLS]) {
      expect(url).toContain('/deploy/phala/reference.json')
      expect(url.startsWith('https://')).toBe(true)
    }
  })
})
