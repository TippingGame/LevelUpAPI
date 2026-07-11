import { beforeEach, describe, expect, it, vi } from 'vitest'

const { showError, adminGenerateAuthUrl, adminExchangeCode, adminRefreshTokens,
  userGenerateAuthUrl, userExchangeCode, userRefreshToken } = vi.hoisted(() => ({
  showError: vi.fn(),
  adminGenerateAuthUrl: vi.fn(),
  adminExchangeCode: vi.fn(),
  adminRefreshTokens: vi.fn(),
  userGenerateAuthUrl: vi.fn(),
  userExchangeCode: vi.fn(),
  userRefreshToken: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    grok: {
      generateAuthUrl: adminGenerateAuthUrl,
      exchangeCode: adminExchangeCode,
      refreshGrokTokens: adminRefreshTokens
    }
  }
}))

vi.mock('@/api/accounts', () => ({
  accountsAPI: {
    generateGrokOAuthUrl: userGenerateAuthUrl,
    exchangeGrokOAuthCode: userExchangeCode,
    refreshGrokToken: userRefreshToken
  }
}))

import { useGrokOAuth } from '@/composables/useGrokOAuth'

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useGrokOAuth', () => {
  it('uses the user-scoped PKCE endpoints and keeps the returned state', async () => {
    userGenerateAuthUrl.mockResolvedValueOnce({
      auth_url: 'https://accounts.x.ai/oauth/authorize',
      session_id: 'user-session',
      state: 'user-state'
    })
    userExchangeCode.mockResolvedValueOnce({ access_token: 'at', refresh_token: 'rt' })

    const oauth = useGrokOAuth('user')

    expect(await oauth.generateAuthUrl(17)).toBe(true)
    expect(userGenerateAuthUrl).toHaveBeenCalledWith({ proxy_id: 17 })
    expect(adminGenerateAuthUrl).not.toHaveBeenCalled()
    expect(oauth.sessionId.value).toBe('user-session')
    expect(oauth.state.value).toBe('user-state')

    await oauth.exchangeAuthCode({
      code: ' code ',
      sessionId: oauth.sessionId.value,
      state: oauth.state.value,
      proxyId: 17
    })
    expect(userExchangeCode).toHaveBeenCalledWith({
      code: 'code',
      session_id: 'user-session',
      state: 'user-state',
      proxy_id: 17
    })
  })

  it('uses the admin endpoints and normalizes batch refresh tokens', async () => {
    adminRefreshTokens.mockResolvedValueOnce({
      tokens: [
        { access_token: 'at-1', refresh_token: 'rt-1' },
        { access_token: 'at-2', refresh_token: 'rt-2' }
      ]
    })

    const oauth = useGrokOAuth('admin')
    const result = await oauth.validateRefreshTokens([' rt-1 ', 'rt-2', 'rt-1', ''], 9)

    expect(adminRefreshTokens).toHaveBeenCalledWith(['rt-1', 'rt-2'], 9)
    expect(userRefreshToken).not.toHaveBeenCalled()
    expect(result.map((token) => token.access_token)).toEqual(['at-1', 'at-2'])
  })

  it('builds official OAuth credentials without accepting a custom base URL', () => {
    const oauth = useGrokOAuth('user')
    const credentials = oauth.buildCredentials({
      access_token: 'at',
      refresh_token: 'rt',
      client_id: 'client-id',
      email: 'owner@example.com',
      base_url: 'https://untrusted.example/v1'
    })

    expect(credentials).toMatchObject({
      access_token: 'at',
      refresh_token: 'rt',
      client_id: 'client-id',
      email: 'owner@example.com'
    })
    expect(credentials).not.toHaveProperty('base_url')
  })
})
