import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

const { listSystemLogs, cleanupSystemLogs, getSystemLogSinkHealth, getRuntimeLogConfig } = vi.hoisted(() => ({
  listSystemLogs: vi.fn(),
  cleanupSystemLogs: vi.fn(),
  getSystemLogSinkHealth: vi.fn(),
  getRuntimeLogConfig: vi.fn()
}))

vi.mock('@/api/admin/ops', () => ({
  opsAPI: { listSystemLogs, cleanupSystemLogs, getSystemLogSinkHealth, getRuntimeLogConfig }
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() })
}))

import OpsSystemLogTable from '../OpsSystemLogTable.vue'

const SelectStub = defineComponent({
  props: { modelValue: { type: [String, Number], default: '' } },
  emits: ['update:modelValue'],
  template: '<div />'
})

describe('OpsSystemLogTable host filtering', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    listSystemLogs.mockResolvedValue({
      items: [{
        id: 1,
        created_at: '2026-07-14T00:00:00Z',
        host: 'api-node-1',
        level: 'warn',
        component: 'app',
        message: 'failed'
      }],
      total: 1,
      page: 1,
      page_size: 20
    })
    cleanupSystemLogs.mockResolvedValue({ deleted: 1 })
    getSystemLogSinkHealth.mockResolvedValue({
      queue_depth: 0,
      queue_capacity: 5000,
      dropped_count: 0,
      write_failed_count: 0,
      written_count: 1,
      avg_write_delay_ms: 0
    })
    getRuntimeLogConfig.mockResolvedValue({
      level: 'info',
      enable_sampling: false,
      sampling_initial: 100,
      sampling_thereafter: 100,
      caller: true,
      stacktrace_level: 'error',
      retention_days: 30
    })
  })

  it('renders host and sends it to list and cleanup requests', async () => {
    const wrapper = mount(OpsSystemLogTable, {
      global: { stubs: { Select: SelectStub, Pagination: true } }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('api-node-1')

    const hostLabel = wrapper.findAll('label').find(label => label.text().includes('主机'))
    expect(hostLabel).toBeDefined()
    await hostLabel!.find('input').setValue(' api-node-2 ')
    await wrapper.findAll('button').find(button => button.text() === '查询')!.trigger('click')
    await flushPromises()
    expect(listSystemLogs).toHaveBeenLastCalledWith(expect.objectContaining({ host: 'api-node-2' }))

    await wrapper.findAll('button').find(button => button.text() === '按当前筛选清理')!.trigger('click')
    await flushPromises()
    expect(cleanupSystemLogs).toHaveBeenCalledWith(expect.objectContaining({ host: 'api-node-2' }))
  })
})
