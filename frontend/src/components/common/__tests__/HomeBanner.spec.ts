import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'

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
  text: 'Idle quota rewards are live',
  cta_text: 'Learn more',
  cta_url: 'https://docs.apex1.us/earn/share-subscription/',
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

  // 记的是「这条文案被关掉了」，不是「这个位置永久关掉」——否则下一期活动
  // 对所有关过横幅的老访客永远不可见，而那批人恰恰是回访率最高的。
  it('reappears when the copy changes', async () => {
    getPublicBanner.mockResolvedValue(activeBanner)
    const first = await mountBanner()
    await first.find('[data-testid="home-banner-close"]').trigger('click')

    getPublicBanner.mockResolvedValue({ ...activeBanner, text: 'A brand new campaign' })
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
