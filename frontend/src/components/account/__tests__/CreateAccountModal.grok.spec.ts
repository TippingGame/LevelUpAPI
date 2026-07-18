import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CreateAccountModal from '../CreateAccountModal.vue'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/account/CreateAccountModal.vue'),
  'utf8'
)

const { showError, createAccount, adminCreateAccount } = vi.hoisted(() => ({
  showError: vi.fn(),
  createAccount: vi.fn(),
  adminCreateAccount: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess: vi.fn(),
    showWarning: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isSimpleMode: false })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: adminCreateAccount,
      checkMixedChannelRisk: vi.fn()
    },
    settings: {
      getSettings: vi.fn().mockResolvedValue({}),
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] })
    },
    tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) },
    grok: {
      generateAuthUrl: vi.fn(),
      exchangeCode: vi.fn(),
      refreshGrokTokens: vi.fn()
    }
  }
}))

vi.mock('@/api/accounts', () => ({
  accountsAPI: {
    create: createAccount,
    generateGrokOAuthUrl: vi.fn(),
    exchangeGrokOAuthCode: vi.fn(),
    refreshGrokToken: vi.fn()
  }
}))

const dialogStub = {
  template: '<div><slot /><slot name="footer" /></div>'
}

const mountModal = (accountScope: 'admin' | 'user' = 'user') => mount(CreateAccountModal, {
  props: {
    show: true,
    accountScope,
    proxies: [],
    groups: []
  },
  global: {
    stubs: {
      BaseDialog: dialogStub,
      ConfirmDialog: true,
      Icon: true,
      PlatformIcon: true,
      ProxySelector: true,
      GroupSelector: true,
      Select: true,
      ModelWhitelistSelector: true,
      QuotaLimitCard: true,
      UserProxyQuickCreatePanel: true,
      OAuthAuthorizationFlow: true
    }
  }
})

beforeEach(() => {
  vi.clearAllMocks()
  adminCreateAccount.mockResolvedValue({})
})

describe('CreateAccountModal Grok user accounts', () => {
  it('keeps normal private/public sharing, requires a visible proxy, and fixes concurrency to one', async () => {
    const wrapper = mountModal()
    const grokButton = wrapper.findAll('button').find((button) => button.text().trim() === 'Grok')
    expect(grokButton).toBeDefined()
    await grokButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('admin.accounts.oauth.grok.oauthOnlyHint')

    const concurrencyInput = wrapper.find('input[type="number"][disabled]')
    expect(concurrencyInput.exists()).toBe(true)
    expect((concurrencyInput.element as HTMLInputElement).value).toBe('1')

    const publicButton = wrapper.findAll('button')
      .find((button) => button.text().includes('userAccounts.publicMode'))
    expect(publicButton).toBeDefined()
    await publicButton!.trigger('click')
    expect(publicButton!.classes()).toContain('border-primary-400')

    await wrapper.get('input[type="text"]').setValue('My Grok account')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('userAccounts.importProxyRequired')
    expect(createAccount).not.toHaveBeenCalled()
  })

  it('offers API Key only to admins and submits the official xAI default', async () => {
    const wrapper = mountModal('admin')
    const grokButton = wrapper.findAll('button').find((button) => button.text().trim() === 'Grok')
    expect(grokButton).toBeDefined()
    await grokButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('admin.accounts.oauth.grok.oauthOnlyHint')
    await wrapper.get('[data-testid="grok-account-type-api-key"]').trigger('click')
    await flushPromises()

    await wrapper.get('input[data-tour="account-form-name"]').setValue('Admin Grok API Key')
    const baseURLInput = wrapper.get('input[placeholder="https://api.x.ai/v1"]')
    expect((baseURLInput.element as HTMLInputElement).value).toBe('https://api.x.ai/v1')
    await wrapper.get('input[placeholder="xai-..."]').setValue('xai-test-key')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(adminCreateAccount).toHaveBeenCalledTimes(1)
    expect(adminCreateAccount.mock.calls[0]?.[0]).toMatchObject({
      name: 'Admin Grok API Key',
      platform: 'grok',
      type: 'apikey',
      credentials: {
        api_key: 'xai-test-key',
        base_url: 'https://api.x.ai/v1'
      }
    })
    expect(createAccount).not.toHaveBeenCalled()
  })

  it('exposes custom upstream URL and header override for the admin OAuth create flow', () => {
    expect(source).toContain('data-testid="grok-custom-base-url-toggle"')
    expect(source).toContain('data-testid="grok-custom-base-url-input"')
    expect(source).toContain("!isUserScope && form.platform === 'grok' && accountCategory === 'oauth-based'")
  })

  it('validates and applies upstream config on all three Grok OAuth create paths', () => {
    // 授权码兑换 / RT 批量 / SSO 批量 3 处调用（定义为箭头函数，不计入）
    expect(source.match(/validateGrokOAuthUpstreamConfig\(\)/g)?.length).toBe(3)
    expect(source.match(/applyGrokOAuthUpstreamConfig\(credentials\)/g)?.length).toBe(3)
  })
})
