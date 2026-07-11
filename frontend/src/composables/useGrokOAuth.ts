import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { accountsAPI } from '@/api/accounts'
import type { GrokTokenInfo } from '@/api/admin/grok'
import type { AccountApiScope } from '@/composables/useAccountOAuth'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'

export function useGrokOAuth(scope: AccountApiScope = 'admin') {
  const appStore = useAppStore()
  const { t } = useI18n()

  const authUrl = ref('')
  const sessionId = ref('')
  const state = ref('')
  const loading = ref(false)
  const error = ref('')

  const resetState = () => {
    authUrl.value = ''
    sessionId.value = ''
    state.value = ''
    loading.value = false
    error.value = ''
  }

  const generateAuthUrl = async (proxyId?: number | null): Promise<boolean> => {
    loading.value = true
    resetState()
    loading.value = true
    try {
      const payload = proxyId ? { proxy_id: proxyId } : {}
      const result = scope === 'user'
        ? await accountsAPI.generateGrokOAuthUrl(payload)
        : await adminAPI.grok.generateAuthUrl(payload)
      authUrl.value = result.auth_url
      sessionId.value = result.session_id
      state.value = result.state || ''
      return true
    } catch (err: unknown) {
      error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.grok.failedToGenerateUrl'))
      appStore.showError(error.value)
      return false
    } finally {
      loading.value = false
    }
  }

  const exchangeAuthCode = async (params: {
    code: string
    sessionId: string
    state: string
    proxyId?: number | null
  }): Promise<GrokTokenInfo | null> => {
    const code = params.code.trim()
    if (!code || !params.sessionId || !params.state) {
      error.value = t('admin.accounts.oauth.grok.missingExchangeParams')
      return null
    }
    loading.value = true
    error.value = ''
    try {
      const payload = {
        session_id: params.sessionId,
        state: params.state,
        code,
        proxy_id: params.proxyId || undefined
      }
      return scope === 'user'
        ? await accountsAPI.exchangeGrokOAuthCode(payload)
        : await adminAPI.grok.exchangeCode(payload)
    } catch (err: unknown) {
      error.value = extractI18nErrorMessage(
        err,
        t,
        'admin.accounts.oauth.grok.errors',
        t('admin.accounts.oauth.grok.failedToExchangeCode')
      )
      appStore.showError(error.value)
      return null
    } finally {
      loading.value = false
    }
  }

  const validateRefreshTokens = async (
    refreshTokens: string[],
    proxyId?: number | null
  ): Promise<GrokTokenInfo[]> => {
    const tokens = [...new Set(refreshTokens.map((token) => token.trim()).filter(Boolean))]
    if (tokens.length === 0) {
      error.value = t('admin.accounts.oauth.grok.pleaseEnterRefreshToken')
      return []
    }
    loading.value = true
    error.value = ''
    try {
      const result = scope === 'user'
        ? await accountsAPI.refreshGrokToken(tokens, proxyId)
        : await adminAPI.grok.refreshGrokTokens(tokens, proxyId)
      if ('tokens' in result && Array.isArray(result.tokens)) {
        return result.tokens as GrokTokenInfo[]
      }
      return [result as GrokTokenInfo]
    } catch (err: unknown) {
      error.value = extractI18nErrorMessage(
        err,
        t,
        'admin.accounts.oauth.grok.errors',
        t('admin.accounts.oauth.grok.failedToValidateRT')
      )
      appStore.showError(error.value)
      return []
    } finally {
      loading.value = false
    }
  }

  const validateRefreshToken = async (refreshToken: string, proxyId?: number | null) => {
    const results = await validateRefreshTokens([refreshToken], proxyId)
    return results[0] || null
  }

  const buildCredentials = (tokenInfo: GrokTokenInfo): Record<string, unknown> => {
    const credentials: Record<string, unknown> = {
      access_token: tokenInfo.access_token,
      refresh_token: tokenInfo.refresh_token,
      token_type: tokenInfo.token_type,
      id_token: tokenInfo.id_token,
      expires_at: tokenInfo.expires_at,
      client_id: tokenInfo.client_id,
      scope: tokenInfo.scope,
      email: tokenInfo.email,
      subscription_tier: tokenInfo.subscription_tier,
      entitlement_status: tokenInfo.entitlement_status
    }
    return Object.fromEntries(Object.entries(credentials).filter(([, value]) => value !== undefined && value !== ''))
  }

  const buildExtraInfo = (tokenInfo: GrokTokenInfo): Record<string, unknown> => {
    const extra: Record<string, unknown> = {}
    if (tokenInfo.email) extra.email = tokenInfo.email
    if (tokenInfo.subscription_tier) extra.subscription_tier = tokenInfo.subscription_tier
    if (tokenInfo.entitlement_status) extra.entitlement_status = tokenInfo.entitlement_status
    return extra
  }

  return {
    authUrl,
    sessionId,
    state,
    loading,
    error,
    resetState,
    generateAuthUrl,
    exchangeAuthCode,
    validateRefreshToken,
    validateRefreshTokens,
    buildCredentials,
    buildExtraInfo
  }
}
