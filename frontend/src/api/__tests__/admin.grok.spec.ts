import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { post } }))

import { createFromSSO, getGrokSSOImportTimeout } from '@/api/admin/grok'

describe('admin Grok SSO import API', () => {
  beforeEach(() => post.mockReset())

  it('uses the batch-aware timeout and backend route', async () => {
    const payload = { sso_tokens: ['one', 'two', 'three', 'four'], proxy_id: 7 }
    const response = { created: [], failed: [] }
    post.mockResolvedValue({ data: response })

    await expect(createFromSSO(payload)).resolves.toEqual(response)
    expect(post).toHaveBeenCalledWith(
      '/admin/grok/sso-to-oauth',
      payload,
      { timeout: getGrokSSOImportTimeout(4) }
    )
    expect(getGrokSSOImportTimeout(4)).toBe(300_000)
  })
})
