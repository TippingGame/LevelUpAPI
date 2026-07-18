import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GrokFreeIcon from '../GrokFreeIcon.vue'
import PlatformTypeBadge from '../PlatformTypeBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

describe('PlatformTypeBadge Grok plans', () => {
  it('renders FREE and BASIC as Grok Free without an expiry date', async () => {
    const wrapper = mount(PlatformTypeBadge, {
      props: {
        platform: 'grok',
        type: 'oauth',
        planType: 'BASIC',
        subscriptionExpiresAt: '2027-01-01T00:00:00Z'
      }
    })

    expect(wrapper.text()).toContain('Grok Free')
    expect(wrapper.findComponent(GrokFreeIcon).exists()).toBe(true)
    expect(wrapper.find('[data-testid="grok-plan-icon"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('2027-01-01')

    await wrapper.setProps({ planType: 'FREE' })
    expect(wrapper.text()).toContain('Grok Free')
  })

  it('normalizes and marks paid SuperGrok plans', async () => {
    const wrapper = mount(PlatformTypeBadge, {
      props: { platform: 'grok', type: 'oauth', planType: 'SuperGrok Heavy' }
    })

    expect(wrapper.text()).toContain('SuperGrok Heavy')
    expect(wrapper.find('[data-testid="grok-plan-icon"]').exists()).toBe(true)

    await wrapper.setProps({ platform: 'openai', planType: 'free' })
    expect(wrapper.text()).toContain('Free')
    expect(wrapper.text()).not.toContain('Grok Free')
    expect(wrapper.find('[data-testid="grok-plan-icon"]').exists()).toBe(false)
  })
})

describe('PlatformTypeBadge OpenAI authentication modes', () => {
  it('distinguishes Agent Identity, PAT, and OAuth accounts', async () => {
    const wrapper = mount(PlatformTypeBadge, {
      props: {
        platform: 'openai',
        type: 'oauth',
        authMode: 'agentIdentity',
      },
    })

    expect(wrapper.text()).toContain('Agent Identity')

    await wrapper.setProps({ authMode: 'personalAccessToken' })
    expect(wrapper.text()).toContain('PAT')
    expect(wrapper.text()).not.toContain('Agent Identity')

    await wrapper.setProps({ authMode: undefined })
    expect(wrapper.text()).toContain('OAuth')
  })
})
