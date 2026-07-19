import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UsageLatencyCell from '../UsageLatencyCell.vue'

const messages: Record<string, string> = {
  'usage.latencyFirstToken': 'First Token',
  'usage.latencyDuration': 'Total Duration',
  'monitorCommon.status.operational': 'Operational',
  'monitorCommon.status.degraded': 'Degraded',
  'common.warning': 'Warning',
  'common.critical': 'Critical'
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => messages[key] ?? key
  })
}))

describe('UsageLatencyCell', () => {
  it('combines first-token and total duration health in one cell', () => {
    const wrapper = mount(UsageLatencyCell, {
      props: {
        firstTokenMs: 10_000,
        durationMs: 180_000
      }
    })

    expect(wrapper.text()).toContain('First Token')
    expect(wrapper.text()).toContain('10.00s')
    expect(wrapper.text()).toContain('Total Duration')
    expect(wrapper.text()).toContain('3m 0s')
    expect(wrapper.text()).toContain('Warning')
    expect(wrapper.text()).toContain('Degraded')
    expect(wrapper.find('.bg-gradient-to-b').exists()).toBe(true)
  })

  it('renders unavailable values without assigning a health level', () => {
    const wrapper = mount(UsageLatencyCell)

    expect(wrapper.find('.bg-gray-300').exists()).toBe(true)
    expect(wrapper.findAll('.text-gray-400').length).toBeGreaterThanOrEqual(2)
  })
})
