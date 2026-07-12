import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupOptionItem from '../GroupOptionItem.vue'
import GroupBadge from '../GroupBadge.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('GroupOptionItem platform colors', () => {
  it.each([
    ['antigravity', 'bg-purple-100', 'text-purple-600'],
    ['grok', 'bg-zinc-100', 'text-zinc-600']
  ] as const)('uses the %s platform colors for the pool and rate', (platform, badgeClass, rateClass) => {
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: `${platform} pool`,
        platform,
        rateMultiplier: 1
      }
    })

    expect(wrapper.getComponent(GroupBadge).classes()).toContain(badgeClass)
    expect(wrapper.get('span.rounded-full').classes()).toContain(rateClass)
  })
})
