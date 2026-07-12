import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import MonitorCard from '../MonitorCard.vue'
import type { UserMonitorView } from '@/api/channelMonitor'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

function monitor(provider: UserMonitorView['provider'], groupName: string): UserMonitorView {
  return {
    id: 1,
    name: 'Primary monitor',
    provider,
    group_name: groupName,
    primary_model: 'model',
    primary_status: 'operational',
    primary_latency_ms: 100,
    primary_ping_latency_ms: 50,
    availability_7d: 99.9,
    extra_models: [],
    timeline: []
  }
}

describe('MonitorCard pool name colors', () => {
  it.each([
    ['openai', 'OpenAI 共享号池', 'bg-emerald-100'],
    ['anthropic', 'Claude 共享号池', 'bg-orange-100'],
    ['gemini', 'Gemini 共享号池', 'bg-blue-100']
  ] as const)('uses %s platform color and exposes the full pool name', (provider, name, colorClass) => {
    const wrapper = mount(MonitorCard, {
      props: {
        item: monitor(provider, name),
        window: '7d',
        availabilityValue: 99.9,
        countdownSeconds: 30
      },
      global: {
        stubs: {
          ProviderIcon: true,
          MonitorMetricPair: true,
          MonitorAvailabilityRow: true,
          MonitorTimeline: true
        }
      }
    })

    const poolName = wrapper.get(`[title="${name}"]`)
    expect(poolName.classes()).toContain(colorClass)
    expect(poolName.text()).toBe(name)
  })
})
