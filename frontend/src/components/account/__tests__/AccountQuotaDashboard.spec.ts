import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AccountQuotaDashboard from '../AccountQuotaDashboard.vue'
import type { AccountQuotaDashboard as AccountQuotaDashboardData, AccountQuotaDimensionSummary, AccountQuotaSummary } from '@/types'
import en from '@/i18n/locales/en'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en }
})

const emptyDimension: AccountQuotaDimensionSummary = {
  enabled_account_count: 0,
  exhausted_account_count: 0,
  limit: 0,
  used: 0,
  remaining: 0,
  utilization: 0
}

const totals: AccountQuotaSummary = {
  platform: 'all',
  type: 'all',
  account_count: 1,
  active_account_count: 1,
  schedulable_account_count: 1,
  rate_limited_account_count: 0,
  codex_quota_protected_account_count: 0,
  error_account_count: 0,
  disabled_account_count: 0,
  quota_account_count: 0,
  unlimited_account_count: 1,
  total: emptyDimension,
  daily: emptyDimension,
  weekly: emptyDimension,
  usage_windows: []
}

function dashboard(platform: 'antigravity' | 'grok', name: string): AccountQuotaDashboardData {
  return {
    generated_at: new Date(0).toISOString(),
    summaries: [],
    totals,
    group_summaries: [{
      ...totals,
      group_id: platform === 'grok' ? 2 : 1,
      group_name: name,
      group_status: 'active',
      platform,
      usage_windows: []
    }]
  }
}

describe('AccountQuotaDashboard pool name colors', () => {
  it.each([
    ['antigravity', 'Antigravity 私有号池', 'bg-purple-100'],
    ['grok', 'Grok 共享号池', 'bg-zinc-100']
  ] as const)('uses %s platform color and exposes the full pool name', (platform, name, colorClass) => {
    const wrapper = mount(AccountQuotaDashboard, {
      props: {
        dashboard: dashboard(platform, name),
        loading: false,
        error: false,
        showSummaryBreakdown: false
      },
      global: {
        plugins: [i18n],
        stubs: {
          Icon: true,
          PlatformIcon: true,
          HelpTooltip: { template: '<div><slot /></div>' }
        }
      }
    })

    const poolName = wrapper.get(`[title="${name}"]`)
    expect(poolName.classes()).toContain(colorClass)
    expect(poolName.text()).toContain(name)
  })
})
