/**
 * 首页活动横幅 API（无鉴权，可匿名访问）。
 *
 * 它存在的理由是：首页上所有文案都写死在 i18n 分片里，改一个字要重新构建镜像、
 * 重新部署 CVM、重新发一份 attestation reference。为了挂一条「活动开始了」走完
 * 那一整套，代价与风险都不成比例——而活动是会反复办的。
 *
 * 后端**已经按语言挑好了**文案，所以这里只有一份 text/cta_text，没有中英两份。
 * 回退规则（只填了一种语言时另一种语言的访客看什么）只应该有一个落点，
 * 前端再实现一遍必然漂移。
 */

import { apiClient } from './client'

/** 横幅样式。promo = 活动（醒目），info = 通知（克制）。 */
export type HomepageBannerVariant = 'promo' | 'info'

/** 首页横幅（已按访客语言挑好文案）。 */
export interface PublicHomepageBanner {
  /** 总开关。false 时其余字段一律为空，前端只看这一个布尔。 */
  enabled: boolean
  /**
   * 这期内容的指纹，**与语言无关**（后端对两种语言的文案、两个链接与样式一起哈希）。
   *
   * 用它作「用户关掉过」的记忆键。**不要**改用渲染出来的 text——那会让切换语言
   * 变成「换了一期内容」：关掉中文横幅、切到英文它又冒出来，切回中文又消失。
   */
  version: string
  /** 正文。 */
  text: string
  /** 按钮文字。空 = 只显示文案、不显示按钮。 */
  cta_text: string
  /** 按钮跳转地址。后端保证是 http(s)，坏链接在读路径就被丢掉了。 */
  cta_url: string
  variant: HomepageBannerVariant
}

/**
 * 拉取首页横幅。
 *
 * lang 显式传入而不是靠 Accept-Language：站内的语言切换是前端状态，浏览器的
 * Accept-Language 不会跟着变——一个把界面切成英文的中文浏览器用户，
 * 应该看到英文横幅。
 *
 * 失败时调用方必须 fail-soft（隐藏整块）。这是首页上的一个装饰件，
 * 不该因为它读不到而让首页出问题。
 */
export async function getPublicBanner(
  lang: string,
  options?: { signal?: AbortSignal }
): Promise<PublicHomepageBanner> {
  const { data } = await apiClient.get<PublicHomepageBanner>('/home/banner', {
    params: { lang },
    signal: options?.signal,
  })
  return data
}

const bannerAPI = { getPublicBanner }

export default bannerAPI
