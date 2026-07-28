import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import AffiliateView from '../AffiliateView.vue'

const { copyToClipboard, getAffiliateDetail } = vi.hoisted(() => ({
  copyToClipboard: vi.fn(),
  getAffiliateDetail: vi.fn(),
}))

vi.mock('@/api/user', () => ({
  default: {
    getAffiliateDetail,
    transferAffiliateQuota: vi.fn(),
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    refreshUser: vi.fn(),
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard }),
}))

// Only the sharing-suggestion copy needs real text (the assertions match on it);
// every other key falls back to itself so the layout assertions stay readable.
const messages: Record<string, string> = {
  'affiliate.share.title': '推荐语参考',
  'affiliate.share.description': '选择一条适合你的推荐语，复制后可直接分享。',
  'affiliate.share.change': '换一条',
  'affiliate.share.copy': '复制推荐语和链接',
  'affiliate.share.copied': '推荐语和链接已复制',
  'affiliate.share.messages.fable': 'Claude Fable 5 现已支持。\n通过 ApexOne 即可开始调用。\n{inviteLink}',
  'affiliate.share.messages.price':
    'Claude Fable 5 API，ApexOne 按官方 API 价格五折计费。\n{inviteLink}',
  'affiliate.share.messages.privacy':
    'ApexOne 现已支持 Claude Fable 5。\n请求经可验证的 TEE 隐私路由处理，相关证据可以自己查。\n{inviteLink}',
}

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) => {
        const message = messages[key] ?? key
        return Object.entries(params ?? {}).reduce(
          (result, [name, value]) => result.replace(`{${name}}`, value),
          message,
        )
      },
    }),
  }
})

function mountAffiliateView() {
  return mount(AffiliateView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: true,
      },
    },
  })
}

describe('AffiliateView', () => {
  const affiliateCode = 'affiliate-code-that-is-long-enough-to-overflow-a-mobile-viewport'

  beforeEach(() => {
    vi.clearAllMocks()
    copyToClipboard.mockResolvedValue(true)
    getAffiliateDetail.mockResolvedValue({
      user_id: 1,
      aff_code: affiliateCode,
      inviter_id: null,
      aff_count: 0,
      aff_quota: 0,
      aff_frozen_quota: 0,
      aff_history_quota: 0,
      effective_rebate_rate_percent: 10,
      invitees: [],
    })
  })

  it('stacks long values and copy controls on mobile while retaining desktop rows', async () => {
    const wrapper = mountAffiliateView()

    await flushPromises()

    const values = wrapper.findAll('code')
    expect(values).toHaveLength(2)
    for (const value of values) {
      expect(value.classes()).toEqual(expect.arrayContaining([
        'min-w-0',
        'break-all',
        'sm:flex-1',
        'sm:truncate',
      ]))
      expect(Array.from(value.element.parentElement?.classList ?? [])).toEqual(expect.arrayContaining([
        'flex-col',
        'items-stretch',
        'sm:flex-row',
        'sm:items-center',
      ]))
    }

    const copyButtons = wrapper.findAll('button').filter((button) =>
      ['affiliate.copyCode', 'affiliate.copyLink'].includes(button.text()),
    )
    expect(copyButtons).toHaveLength(2)
    for (const button of copyButtons) {
      expect(button.classes()).toEqual(expect.arrayContaining([
        'w-full',
        'sm:w-auto',
        'sm:shrink-0',
      ]))
    }

    await copyButtons[0].trigger('click')
    await copyButtons[1].trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenNthCalledWith(1, affiliateCode, 'affiliate.codeCopied')
    expect(copyToClipboard).toHaveBeenNthCalledWith(
      2,
      `${window.location.origin}/register?aff=${encodeURIComponent(affiliateCode)}`,
      'affiliate.linkCopied',
    )
  })
})

describe('AffiliateView sharing suggestions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    copyToClipboard.mockResolvedValue(true)
    getAffiliateDetail.mockResolvedValue({
      user_id: 1,
      aff_code: 'AFF123',
      inviter_id: null,
      aff_count: 0,
      aff_quota: 0,
      aff_frozen_quota: 0,
      aff_history_quota: 0,
      effective_rebate_rate_percent: 10,
      invitees: [],
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('starts with a randomly selected suggestion and rotates to the next one', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0.99)
    const wrapper = mountAffiliateView()
    await flushPromises()

    expect(wrapper.get('[data-testid="affiliate-share-message"]').text()).toContain(
      '可验证的 TEE 隐私路由',
    )

    await wrapper.get('[data-testid="affiliate-next-share-message"]').trigger('click')

    expect(wrapper.get('[data-testid="affiliate-share-message"]').text()).toContain(
      'Claude Fable 5 现已支持',
    )
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
