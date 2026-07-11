import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { copyToClipboard, getAffiliateDetail, refreshUser } = vi.hoisted(() => ({
  copyToClipboard: vi.fn().mockResolvedValue(true),
  getAffiliateDetail: vi.fn(),
  refreshUser: vi.fn().mockResolvedValue(undefined)
}))

const messages: Record<string, string> = {
  'affiliate.share.title': '推荐语参考',
  'affiliate.share.description': '选择一条适合你的推荐语，复制后可直接分享。',
  'affiliate.share.change': '换一条',
  'affiliate.share.copy': '复制推荐语和链接',
  'affiliate.share.copied': '推荐语和链接已复制',
  'affiliate.share.messages.fable': 'Claude Fable 5 现已支持。\n通过 ApexOne 即可开始调用。\n{inviteLink}',
  'affiliate.share.messages.price': 'Claude Fable 5 API，ApexOne 按官方 API 价格五折计费。\n{inviteLink}',
  'affiliate.share.messages.privacy': 'ApexOne 现已支持 Claude Fable 5。\n请求经可验证的 TEE 隐私路由处理，相关证据可以自己查。\n{inviteLink}'
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) => {
        const message = messages[key] ?? key
        return Object.entries(params ?? {}).reduce(
          (result, [name, value]) => result.replace(`{${name}}`, value),
          message
        )
      }
    })
  }
})

vi.mock('@/api/user', () => ({
  default: {
    getAffiliateDetail,
    transferAffiliateQuota: vi.fn()
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ refreshUser })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard })
}))

import AffiliateView from '../AffiliateView.vue'

function mountAffiliateView() {
  return mount(AffiliateView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Icon: true
      }
    }
  })
}

describe('AffiliateView sharing suggestions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getAffiliateDetail.mockResolvedValue({
      user_id: 1,
      aff_code: 'AFF123',
      aff_count: 0,
      aff_quota: 0,
      aff_frozen_quota: 0,
      aff_history_quota: 0,
      effective_rebate_rate_percent: 10,
      invitees: []
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('starts with a randomly selected suggestion and rotates to the next one', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0.99)
    const wrapper = mountAffiliateView()
    await flushPromises()

    expect(wrapper.get('[data-testid="affiliate-share-message"]').text()).toContain('可验证的 TEE 隐私路由')

    await wrapper.get('[data-testid="affiliate-next-share-message"]').trigger('click')

    expect(wrapper.get('[data-testid="affiliate-share-message"]').text()).toContain('Claude Fable 5 现已支持')
  })

  it('copies the selected suggestion together with the current invite link', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0.5)
    const wrapper = mountAffiliateView()
    await flushPromises()

    await wrapper.get('[data-testid="affiliate-copy-share-message"]').trigger('click')
    await flushPromises()

    const [copiedText, successMessage] = copyToClipboard.mock.calls[0]
    expect(copiedText).toContain('官方 API 价格五折')
    expect(copiedText).toContain(`${window.location.origin}/register?aff=AFF123`)
    expect(successMessage).toBe('推荐语和链接已复制')
  })
})
