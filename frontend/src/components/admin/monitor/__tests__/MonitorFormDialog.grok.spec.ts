import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: {}, showError: vi.fn() })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitorTemplate: { list: vi.fn().mockResolvedValue({ items: [] }) },
    channelMonitor: { create: vi.fn(), update: vi.fn() }
  }
}))

vi.mock('@/api/keys', () => ({ keysAPI: { list: vi.fn() } }))
vi.mock('@/api/groups', () => ({ userGroupsAPI: { getUserGroupRates: vi.fn() } }))

import MonitorFormDialog from '../MonitorFormDialog.vue'

const BaseDialogStub = {
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
}

describe('MonitorFormDialog Grok defaults', () => {
  it('fills and only clears untouched Grok endpoint/model defaults', async () => {
    const wrapper = mount(MonitorFormDialog, {
      props: { show: true, monitor: null },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          ProviderIcon: true,
          MonitorKeyPickerDialog: true,
          MonitorAdvancedRequestConfig: true,
          ModelTagInput: true,
          Select: true,
          Toggle: true
        }
      }
    })
    await flushPromises()

    await wrapper.get('[data-testid="monitor-provider-grok"]').trigger('click')
    expect((wrapper.get('[data-testid="monitor-endpoint"]').element as HTMLInputElement).value)
      .toBe('https://api.x.ai')
    expect((wrapper.get('[data-testid="monitor-primary-model"]').element as HTMLInputElement).value)
      .toBe('grok-4.5')

    await wrapper.get('[data-testid="monitor-provider-openai"]').trigger('click')
    expect((wrapper.get('[data-testid="monitor-endpoint"]').element as HTMLInputElement).value).toBe('')
    expect((wrapper.get('[data-testid="monitor-primary-model"]').element as HTMLInputElement).value).toBe('')

    await wrapper.get('[data-testid="monitor-provider-grok"]').trigger('click')
    await wrapper.get('[data-testid="monitor-endpoint"]').setValue('https://custom.example')
    await wrapper.get('[data-testid="monitor-primary-model"]').setValue('grok-custom')
    await wrapper.get('[data-testid="monitor-provider-openai"]').trigger('click')
    expect((wrapper.get('[data-testid="monitor-endpoint"]').element as HTMLInputElement).value)
      .toBe('https://custom.example')
    expect((wrapper.get('[data-testid="monitor-primary-model"]').element as HTMLInputElement).value)
      .toBe('grok-custom')
  })
})
