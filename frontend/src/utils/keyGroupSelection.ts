import type { Group, GroupPlatform } from '@/types'

export const KEY_GROUP_PLATFORM_ORDER: GroupPlatform[] = [
  'anthropic',
  'openai',
  'gemini',
  'antigravity',
  'grok'
]

type Translate = (key: string, params?: Record<string, unknown>) => string

export function availableKeyGroupPlatforms(groups: Group[]): GroupPlatform[] {
  const available = new Set(groups.map((group) => group.platform))
  return KEY_GROUP_PLATFORM_ORDER.filter((platform) => available.has(platform))
}

export function localizedKeyGroupDescription(group: Group, t: Translate): string | null {
  if (group.scope !== 'user_private') return group.description

  const platform = t(`admin.groups.platforms.${group.platform}`)
  if (group.owner_user_id != null) {
    return t('keys.privateGroupDescription', {
      userId: group.owner_user_id,
      platform
    })
  }
  return t('keys.privateGroupDescriptionWithoutOwner', { platform })
}
