import { describe, expect, it } from 'vitest'
import {
  availableKeyGroupPlatforms,
  localizedKeyGroupDescription
} from '@/utils/keyGroupSelection'
import type { Group } from '@/types'

const makeGroup = (overrides: Partial<Group>): Group => ({
  id: 1,
  name: 'group',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '',
  updated_at: '',
  ...overrides
})

describe('keyGroupSelection', () => {
  it('returns only available platforms in a stable product order', () => {
    const groups = [
      makeGroup({ id: 1, platform: 'grok' }),
      makeGroup({ id: 2, platform: 'anthropic' }),
      makeGroup({ id: 3, platform: 'antigravity' })
    ]

    expect(availableKeyGroupPlatforms(groups)).toEqual(['anthropic', 'antigravity', 'grok'])
  })

  it('localizes existing private group descriptions from metadata', () => {
    const group = makeGroup({
      platform: 'antigravity',
      scope: 'user_private',
      owner_user_id: 2,
      description: 'Private subscription group for user 2 on antigravity.'
    })
    const zh = (key: string, params?: Record<string, unknown>) => {
      if (key === 'admin.groups.platforms.antigravity') return 'Antigravity'
      if (key === 'keys.privateGroupDescription') {
        return `用户 ${params?.userId} 的 ${params?.platform} 私有订阅号池。`
      }
      return key
    }
    const en = (key: string, params?: Record<string, unknown>) => {
      if (key === 'admin.groups.platforms.antigravity') return 'Antigravity'
      if (key === 'keys.privateGroupDescription') {
        return `Private ${params?.platform} subscription pool for user ${params?.userId}.`
      }
      return key
    }

    expect(localizedKeyGroupDescription(group, zh)).toBe('用户 2 的 Antigravity 私有订阅号池。')
    expect(localizedKeyGroupDescription(group, en)).toBe('Private Antigravity subscription pool for user 2.')
  })

  it('keeps public group descriptions unchanged', () => {
    const group = makeGroup({ description: 'Fast shared pool', scope: 'public' })
    expect(localizedKeyGroupDescription(group, (key) => key)).toBe('Fast shared pool')
  })
})
