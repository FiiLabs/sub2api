// 页脚共享数据：完整营销页脚(AppFooter)与后台精简页脚(AppFooterCompact)共用，避免社交链接/邮箱重复维护。

export const supportEmail = 'support@apex1.us'

export interface FooterSocialLink {
  /** 稳定标识，用作 v-for key 与测试选择器，不随文案变 */
  label: string
  /** 展示用的品牌名，进 aria-label 与 title */
  name: string
  href: string
  /**
   * 24x24 viewBox 下的**纯字形** path，不含圆形底。
   *
   * 之前这里是 44x44、把圆底烘焙进 path 的整块图标。那种画法在「图标就是
   * 一个实心圆」的老版式里成立，但现在图标外面套了我们自己的圆角按钮，
   * 底再自带一个圆就成了圆套圆，hover 态也没法只作用在字形上。
   */
  path: string
}

export const socialLinks: readonly FooterSocialLink[] = [
  {
    label: 'x',
    name: 'X',
    href: 'https://x.com/Apex1_api',
    path: 'M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z'
  },
  {
    label: 'telegram',
    name: 'Telegram',
    href: 'https://t.me/apex1us',
    path: 'M23.91 3.79 20.3 20.84c-.25 1.21-.98 1.5-2 .94l-5.5-4.07-2.66 2.57c-.3.3-.55.56-1.1.56-.72 0-.6-.27-.84-.95L6.3 13.7l-5.45-1.7c-1.18-.35-1.19-1.16.26-1.75l21.26-8.2c.97-.43 1.9.24 1.53 1.73z'
  }
] as const
