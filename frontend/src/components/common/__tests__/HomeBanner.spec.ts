import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const { getPublicBanner } = vi.hoisted(() => ({ getPublicBanner: vi.fn() }))
vi.mock('@/api/banner', () => ({ getPublicBanner }))

const locale = ref('en')
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, locale }),
  }
})

import HomeBanner from '../HomeBanner.vue'

const activeBanner = {
  enabled: true,
  version: 'v1abc',
  text: 'Idle quota rewards are live',
  cta_text: 'Learn more',
  cta_url: 'https://docs.apex1.us/earn/idle-quota-rewards/',
  variant: 'promo' as const,
}

async function mountBanner() {
  const wrapper = mount(HomeBanner)
  await flushPromises()
  return wrapper
}

describe('HomeBanner', () => {
  beforeEach(() => {
    getPublicBanner.mockReset()
    localStorage.clear()
    locale.value = 'en'
  })

  it('renders the banner with the copy the backend picked', async () => {
    getPublicBanner.mockResolvedValue(activeBanner)
    const wrapper = await mountBanner()

    expect(wrapper.find('[data-testid="home-banner"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-banner-text"]').text()).toBe(activeBanner.text)
    const cta = wrapper.find('[data-testid="home-banner-cta"]')
    expect(cta.attributes('href')).toBe(activeBanner.cta_url)
    // 外链必须带 noopener，否则目标页能通过 window.opener 操纵首页。
    expect(cta.attributes('rel')).toContain('noopener')
  })

  // fail-soft 是这个组件最重要的性质：它是首页上的一个装饰件，
  // 不该因为它读不到而让首页出现错误、占位或空条。
  it('renders nothing when the API fails', async () => {
    getPublicBanner.mockRejectedValue(new Error('boom'))
    const wrapper = await mountBanner()
    expect(wrapper.find('[data-testid="home-banner"]').exists()).toBe(false)
  })

  it('renders nothing when disabled or when text is empty', async () => {
    getPublicBanner.mockResolvedValue({ ...activeBanner, enabled: false })
    expect((await mountBanner()).find('[data-testid="home-banner"]').exists()).toBe(false)

    getPublicBanner.mockResolvedValue({ ...activeBanner, text: '' })
    expect((await mountBanner()).find('[data-testid="home-banner"]').exists()).toBe(false)
  })

  it('hides the CTA when there is no link', async () => {
    getPublicBanner.mockResolvedValue({ ...activeBanner, cta_text: '', cta_url: '' })
    const wrapper = await mountBanner()
    expect(wrapper.find('[data-testid="home-banner"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="home-banner-cta"]').exists()).toBe(false)
  })

  it('stays dismissed across reloads', async () => {
    getPublicBanner.mockResolvedValue(activeBanner)
    const first = await mountBanner()
    await first.find('[data-testid="home-banner-close"]').trigger('click')
    expect(first.find('[data-testid="home-banner"]').exists()).toBe(false)

    // 关掉之后刷新又回来的横幅，比没有横幅更烦人。
    const second = await mountBanner()
    expect(second.find('[data-testid="home-banner"]').exists()).toBe(false)
  })

  // 记的是「这一期活动被关掉了」，不是「这个位置永久关掉」——否则下一期活动
  // 对所有关过横幅的老访客永远不可见，而那批人恰恰是回访率最高的。
  it('reappears when the campaign version changes', async () => {
    getPublicBanner.mockResolvedValue(activeBanner)
    const first = await mountBanner()
    await first.find('[data-testid="home-banner-close"]').trigger('click')

    getPublicBanner.mockResolvedValue({ ...activeBanner, version: 'v2xyz', text: 'A brand new campaign' })
    const second = await mountBanner()
    expect(second.find('[data-testid="home-banner"]').exists()).toBe(true)
  })

  // 回归：最初拿渲染文案当记忆键，切一次语言文案就变、键也跟着变——
  // 关掉中文横幅后切到英文它又冒出来，切回中文又消失，横幅忽隐忽现。
  // version 与语言无关，所以关过之后两种语言都该保持关闭。
  it('stays dismissed across a language switch', async () => {
    getPublicBanner.mockResolvedValue(activeBanner)
    const wrapper = await mountBanner()
    await wrapper.find('[data-testid="home-banner-close"]').trigger('click')
    expect(wrapper.find('[data-testid="home-banner"]').exists()).toBe(false)

    // 同一期活动的另一种语言：文案与链接都变了，version 没变。
    getPublicBanner.mockResolvedValue({
      ...activeBanner,
      text: '限时活动：共享闲置订阅额度',
      cta_text: '了解详情',
      cta_url: 'https://docs.apex1.us/zh-cn/earn/idle-quota-rewards/',
    })
    locale.value = 'zh'
    await flushPromises()
    expect(wrapper.find('[data-testid="home-banner"]').exists()).toBe(false)
  })

  // 没有 version 时只关这一次，不落盘——否则所有缺 version 的情况会共用同一个键，
  // 关掉任意一期就等于关掉了以后所有期。
  it('does not persist a dismissal without a version', async () => {
    getPublicBanner.mockResolvedValue({ ...activeBanner, version: '' })
    const first = await mountBanner()
    await first.find('[data-testid="home-banner-close"]').trigger('click')
    expect(first.find('[data-testid="home-banner"]').exists()).toBe(false)

    const second = await mountBanner()
    expect(second.find('[data-testid="home-banner"]').exists()).toBe(true)
  })

  it('refetches when the site language changes', async () => {
    getPublicBanner.mockResolvedValue(activeBanner)
    await mountBanner()
    expect(getPublicBanner).toHaveBeenCalledWith('en', expect.anything())

    // 后端按语言挑文案，不重拉就会停在上一种语言。
    locale.value = 'zh'
    await flushPromises()
    expect(getPublicBanner).toHaveBeenCalledWith('zh', expect.anything())
  })

  it('survives a storage that throws (private mode)', async () => {
    getPublicBanner.mockResolvedValue(activeBanner)
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    const wrapper = await mountBanner()
    await wrapper.find('[data-testid="home-banner-close"]').trigger('click')
    // 存不住没关系，关掉这一次就够了。
    expect(wrapper.find('[data-testid="home-banner"]').exists()).toBe(false)
    setItem.mockRestore()
  })
})

// ---------------------------------------------------------------------------
// 层级不变量
// ---------------------------------------------------------------------------

/**
 * 这一组读的是**源码文本**，不是渲染结果——因为 jsdom 不做布局，也不解析 Tailwind，
 * `getComputedStyle().zIndex` 在这里永远是空的，真正的层叠关系测不出来。
 *
 * 能测的是那条不变量本身：**横幅必须排在 Header 之下**。它挡不住所有遮挡问题，
 * 但挡得住最可能复发的那一种——有人顺手把横幅的 z 调到和 Header 一样。
 *
 * 这不是假想的风险。第一版横幅取了 z-20，与 Header 同级，而 DOM 在后 → 画在上面 →
 * 盖住了语言切换的下拉菜单。用户报的现象是「切语言没反应」，因为点击被横幅吃掉了，
 * 离根因隔着好几层，很难往层级上想。
 *
 * 比对的是**两边的实际数值**而不是硬写 "z-10"：Header 哪天升到 z-30，这条测试
 * 应该继续通过，而不是变成一次需要同步修改的噪音。
 */
function zIndexesIn(source: string): number[] {
  return [...source.matchAll(/\bz-(\d+)\b/g)].map((m) => Number(m[1]))
}

describe('HomeBanner stacking order', () => {
  const headerSource = readFileSync(
    resolve(process.cwd(), 'src/components/layout/Header.vue'),
    'utf8',
  )
  const bannerSource = readFileSync(
    resolve(process.cwd(), 'src/components/common/HomeBanner.vue'),
    'utf8',
  )

  it('sits below the header, so header dropdowns stay clickable', () => {
    // Header 的根元素——不是移动端抽屉（那个是 z-40/z-50，且不覆盖横幅所在区域）。
    const headerRoot = headerSource.match(/<header[^>]*class="([^"]+)"/)
    expect(headerRoot, '找不到 Header 根元素的 class').not.toBeNull()
    const headerZ = zIndexesIn(headerRoot![1])
    expect(headerZ, 'Header 根元素应当有一个 z-index').toHaveLength(1)

    // 横幅的两种样式各有一个 z，都要比 Header 低。
    const wrapper = bannerSource.match(/const wrapperClass = computed\([\s\S]*?\)\n/)
    expect(wrapper, '找不到 wrapperClass').not.toBeNull()
    const bannerZ = zIndexesIn(wrapper![0])
    expect(bannerZ.length, '横幅的每种样式都该显式带一个 z-index').toBeGreaterThanOrEqual(2)

    for (const z of bannerZ) {
      expect(
        z,
        `横幅 z-${z} 不低于 Header z-${headerZ[0]}：横幅会盖住顶栏的下拉菜单，` +
          `点击被吃掉，现象是「切语言没反应」`,
      ).toBeLessThan(headerZ[0])
    }
  })
})
