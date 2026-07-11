import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import GrokQuotaProbeCell from '../GrokQuotaProbeCell.vue'
import type { Account } from '@/types'

const { adminQueryQuota, userQueryQuota } = vi.hoisted(() => ({
  adminQueryQuota: vi.fn(),
  userQueryQuota: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: { grok: { queryQuota: adminQueryQuota } }
}))

vi.mock('@/api/accounts', () => ({
  accountsAPI: { queryGrokQuota: userQueryQuota }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params?.time ? `${key}:${params.time}` : key
  })
}))

const account = {
  id: 42,
  name: 'Grok owner account',
  platform: 'grok',
  type: 'oauth'
} as Account

beforeEach(() => {
  vi.clearAllMocks()
})

describe('GrokQuotaProbeCell', () => {
  it('uses the user-owned account quota endpoint', async () => {
    userQueryQuota.mockResolvedValueOnce({
      source: 'active_probe',
      model: 'grok-4.3',
      headers_observed: true,
      reset_supported: false,
      fetched_at: 1,
      snapshot: {
        headers_observed: true,
        updated_at: '2026-07-11T00:00:00Z',
        requests: { limit: 10, remaining: 8 },
        tokens: { limit: 1000, remaining: 900 },
        retry_after_seconds: 90
      }
    })

    const wrapper = mount(GrokQuotaProbeCell, {
      props: { account, accountScope: 'user' }
    })
    await wrapper.get('button:not([disabled])').trigger('click')
    await flushPromises()

    expect(userQueryQuota).toHaveBeenCalledWith(42)
    expect(adminQueryQuota).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('8/10')
    expect(wrapper.text()).toContain('900/1000')
    expect(wrapper.text()).toContain('2m')
  })

  it('uses the admin quota endpoint by default', async () => {
    adminQueryQuota.mockResolvedValueOnce({
      source: 'active_probe',
      model: 'grok-4.3',
      headers_observed: false,
      reset_supported: false,
      fetched_at: 1,
      snapshot: null
    })

    const wrapper = mount(GrokQuotaProbeCell, { props: { account } })
    await wrapper.get('button:not([disabled])').trigger('click')
    await flushPromises()

    expect(adminQueryQuota).toHaveBeenCalledWith(42)
    expect(userQueryQuota).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.accounts.usageWindow.grokNoHeaders')
  })
})
