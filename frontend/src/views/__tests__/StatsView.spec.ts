import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'

import en from '@/i18n/locales/en'
import type { PublicHomepageStats } from '@/api/stats'
import StatsView from '../public/StatsView.vue'

const { getPublicStats } = vi.hoisted(() => ({ getPublicStats: vi.fn() }))

vi.mock('@/api/stats', () => ({ getPublicStats }))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => {
        const value = key.split('.').reduce<unknown>((current, part) => {
          if (current && typeof current === 'object' && part in current) {
            return (current as Record<string, unknown>)[part]
          }
          return undefined
        }, en)
        return typeof value === 'string' ? value : key
      },
      locale: ref('en')
    })
  }
})

function mountStats() {
  return mount(StatsView, {
    global: {
      stubs: {
        Header: true,
        StatsGrowthChart: true,
        StatsSupplyDonut: true,
        CountUp: { props: ['value', 'format'], template: '<span>{{ value }}</span>' },
        RouterLink: {
          props: ['to'],
          template: '<a :href="typeof to === \'string\' ? to : \'#\'"><slot /></a>'
        }
      }
    }
  })
}

const enabledStats: PublicHomepageStats = {
  enabled: true,
  shared_accounts: 1200,
  active_users: 3400,
  total_requests: 1_200_000,
  total_tokens: 89_000_000,
  contributor_earnings_usdt: 48000,
  supply_by_platform: { anthropic: 8, openai: 4 }
}

describe('StatsView public metrics dashboard', () => {
  beforeEach(() => {
    getPublicStats.mockReset()
  })

  it('renders KPIs, growth chart and supply donut when enabled', async () => {
    getPublicStats.mockResolvedValue(enabledStats)
    const wrapper = mountStats()
    await flushPromises()

    expect(wrapper.find('[data-testid="stats-empty"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stats-kpis"]').exists()).toBe(true)
    // 5 个 KPI 卡
    expect(wrapper.findAll('[data-testid="stats-kpis"] > div')).toHaveLength(5)
    expect(wrapper.findComponent({ name: 'StatsGrowthChart' }).exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'StatsSupplyDonut' }).exists()).toBe(true)
  })

  it('shows the empty state when disabled', async () => {
    getPublicStats.mockResolvedValue({
      enabled: false,
      shared_accounts: 0,
      active_users: 0,
      total_requests: 0,
      total_tokens: 0,
      contributor_earnings_usdt: 0
    })
    const wrapper = mountStats()
    await flushPromises()

    expect(wrapper.find('[data-testid="stats-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="stats-kpis"]').exists()).toBe(false)
  })

  it('shows the empty state (fail-soft) when the fetch fails', async () => {
    getPublicStats.mockRejectedValue(new Error('network down'))
    const wrapper = mountStats()
    await flushPromises()

    expect(wrapper.find('[data-testid="stats-empty"]').exists()).toBe(true)
  })
})
