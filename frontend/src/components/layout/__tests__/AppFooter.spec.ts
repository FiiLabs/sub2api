// 页脚联系方式的守卫。
//
// 这里钉的是**对外可点的东西**：一个写错的社交账号不会让任何测试变红、
// 也不会报错，它只是安静地把访客送去别人的主页。所以链接本身要有断言。
//
// 同时钉住「几个入口在同一个容器里」这件事：把它们分回左右两端在视觉上
// 是个可以争论的选择，但那会让 justify-between 再次把同一组内容撑到
// 屏幕两头——2026-09-02 改成一条轨就是为了修这个。

import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import AppFooter from '../AppFooter.vue'
import AppFooterCompact from '../AppFooterCompact.vue'
import { socialLinks, supportEmail } from '../footerLinks'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

function mountFooter() {
  return mount(AppFooter, {
    global: {
      stubs: { Icon: { template: '<span data-testid="icon" />' } }
    }
  })
}

describe('footerLinks', () => {
  it('X 账号指向 Apex1_api', () => {
    const x = socialLinks.find((s) => s.label === 'x')
    expect(x).toBeDefined()
    expect(x!.href).toBe('https://x.com/Apex1_api')
  })

  it('Telegram 账号指向 apex1us', () => {
    const tg = socialLinks.find((s) => s.label === 'telegram')
    expect(tg!.href).toBe('https://t.me/apex1us')
  })

  // path 换成 24x24 纯字形之后，两个组件的 viewBox 都必须跟着改。
  // 漏掉一个的症状是图标缩成左上角一个几乎看不见的点——不会报错。
  it('所有 path 都是不含圆形底的纯字形', () => {
    for (const item of socialLinks) {
      expect(item.path).not.toMatch(/^M22 0c12\.15 0 22 9\.85 22 22/)
      expect(item.name.length).toBeGreaterThan(0)
    }
  })
})

describe('AppFooter 联系方式', () => {
  it('邮箱与所有社交入口在同一个容器里', () => {
    const wrapper = mountFooter()
    const email = wrapper.get('[data-testid="footer-email"]')

    // 找到装邮箱的那一层，社交入口必须也在它里面。
    const rail = email.element.parentElement
    expect(rail).not.toBeNull()

    for (const item of socialLinks) {
      const social = wrapper.get(`[data-testid="footer-social-${item.label}"]`)
      expect(rail!.contains(social.element)).toBe(true)
    }
  })

  it('每个社交入口都渲染出来且带可读名', () => {
    const wrapper = mountFooter()
    for (const item of socialLinks) {
      const a = wrapper.get(`[data-testid="footer-social-${item.label}"]`)
      expect(a.attributes('href')).toBe(item.href)
      expect(a.attributes('aria-label')).toBe(item.name)
      // 外链一律 noreferrer：这些是站外品牌页，不该把来源路径带过去。
      expect(a.attributes('rel')).toContain('noreferrer')
      expect(a.attributes('target')).toBe('_blank')
    }
  })

  it('邮箱仍是 mailto，不是被顺手改成了页面链接', () => {
    const wrapper = mountFooter()
    expect(wrapper.get('[data-testid="footer-email"]').attributes('href')).toBe(
      `mailto:${supportEmail}`
    )
  })

  // 「保持联系」从可见文案降级成 sr-only，但不能直接删——读屏器需要这个组名。
  it('保持联系的文案保留给读屏器', () => {
    expect(mountFooter().html()).toContain('sr-only')
  })
})

describe('AppFooterCompact', () => {
  it('复用同一份链接数据，不另抄一份', () => {
    const wrapper = mount(AppFooterCompact, {
      global: { stubs: { Icon: { template: '<span />' } } }
    })
    for (const item of socialLinks) {
      expect(wrapper.html()).toContain(item.href)
    }
  })
})
