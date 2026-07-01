import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import OpenAIQuotaResetCell from '../OpenAIQuotaResetCell.vue'
import type { Account } from '@/types'

const { queryOpenAIQuota, resetOpenAIQuota, userQueryOpenAIQuota, userResetOpenAIQuota } = vi.hoisted(() => ({
  queryOpenAIQuota: vi.fn(),
  resetOpenAIQuota: vi.fn(),
  userQueryOpenAIQuota: vi.fn(),
  userResetOpenAIQuota: vi.fn()
}))

vi.mock('@/api/admin/accounts', () => ({
  queryOpenAIQuota,
  resetOpenAIQuota
}))

vi.mock('@/api/accounts', () => ({
  accountsAPI: {
    queryOpenAIQuota: userQueryOpenAIQuota,
    resetOpenAIQuota: userResetOpenAIQuota
  }
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (!params) return key
        return `${key}:${JSON.stringify(params)}`
      }
    })
  }
})

function makeAccount(): Account {
  return {
    id: 10,
    name: 'openai-account',
    platform: 'openai',
    type: 'oauth',
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-06-17T00:00:00Z',
    updated_at: '2026-06-17T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null
  }
}

function mountCell(props: Partial<{ account: Account; accountScope: 'admin' | 'user' }> = {}) {
  return mount(OpenAIQuotaResetCell, {
    props: {
      account: makeAccount(),
      ...props
    },
    global: {
      stubs: {
        ConfirmDialog: {
          props: ['show', 'title', 'message', 'confirmText', 'cancelText', 'danger'],
          emits: ['confirm', 'cancel'],
          template: `
            <div v-if="show" class="confirm-dialog">
              <span class="confirm-message">{{ message }}</span>
              <button class="confirm-reset" type="button" @click="$emit('confirm')">{{ confirmText }}</button>
              <button class="cancel-reset" type="button" @click="$emit('cancel')">{{ cancelText }}</button>
            </div>
          `
        }
      }
    }
  })
}

describe('OpenAIQuotaResetCell', () => {
  beforeEach(() => {
    queryOpenAIQuota.mockReset()
    resetOpenAIQuota.mockReset()
    userQueryOpenAIQuota.mockReset()
    userResetOpenAIQuota.mockReset()
  })

  it('requires confirmation before consuming a reset credit', async () => {
    queryOpenAIQuota.mockResolvedValue({
      rate_limit_reset_credits: {
        available_count: 2
      }
    })
    resetOpenAIQuota.mockResolvedValue({
      code: 'ok',
      windows_reset: 1
    })

    const wrapper = mountCell()
    await wrapper.findAll('button')[0].trigger('click')
    await flushPromises()

    await wrapper.findAll('button')[1].trigger('click')
    expect(resetOpenAIQuota).not.toHaveBeenCalled()
    expect(wrapper.find('.confirm-dialog').exists()).toBe(true)
    expect(wrapper.find('.confirm-message').text()).toContain('openai-account')
    expect(wrapper.find('.confirm-message').text()).toContain('"count":2')

    await wrapper.find('.confirm-reset').trigger('click')
    await flushPromises()

    expect(resetOpenAIQuota).toHaveBeenCalledTimes(1)
    expect(resetOpenAIQuota).toHaveBeenCalledWith(10)
    expect(queryOpenAIQuota).toHaveBeenCalledTimes(2)
  })

  it('uses user-scoped quota APIs when account scope is user', async () => {
    userQueryOpenAIQuota.mockResolvedValue({
      rate_limit_reset_credits: {
        available_count: 1
      }
    })
    userResetOpenAIQuota.mockResolvedValue({
      code: 'ok',
      windows_reset: 1
    })

    const wrapper = mountCell({ accountScope: 'user' })
    await wrapper.findAll('button')[0].trigger('click')
    await flushPromises()
    await wrapper.findAll('button')[1].trigger('click')
    await wrapper.find('.confirm-reset').trigger('click')
    await flushPromises()

    expect(queryOpenAIQuota).not.toHaveBeenCalled()
    expect(resetOpenAIQuota).not.toHaveBeenCalled()
    expect(userQueryOpenAIQuota).toHaveBeenCalledTimes(2)
    expect(userResetOpenAIQuota).toHaveBeenCalledWith(10)
  })
})
