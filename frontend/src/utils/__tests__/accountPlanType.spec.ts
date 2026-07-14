import { describe, expect, it } from 'vitest'

import { resolveAccountPlanType } from '../format'

describe('resolveAccountPlanType', () => {
  it('prefers observed Grok billing and current usage snapshots', () => {
    expect(resolveAccountPlanType({
      platform: 'grok',
      credentials: { subscription_tier: 'FREE', plan_type: 'legacy' },
      extra: {
        grok_billing_snapshot: { plan: 'SuperGrok Heavy' },
        grok_usage_snapshot: { subscription_tier: 'SuperGrok' },
        subscription_tier: 'BASIC'
      }
    })).toBe('SuperGrok Heavy')

    expect(resolveAccountPlanType({
      platform: 'grok',
      credentials: { subscription_tier: 'FREE' },
      extra: { grok_usage_snapshot: { subscription_tier: 'SuperGrok' } }
    })).toBe('SuperGrok')
  })

  it('supports Grok metadata fallbacks and leaves other platforms unchanged', () => {
    expect(resolveAccountPlanType({
      platform: 'grok',
      credentials: { subscription_tier: 'BASIC' }
    })).toBe('BASIC')
    expect(resolveAccountPlanType({
      platform: 'openai',
      credentials: { plan_type: 'plus', subscription_tier: 'ignored' },
      parent_plan_type: 'team'
    })).toBe('plus')
  })
})
