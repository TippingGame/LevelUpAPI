import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CreateAccountModal from '../CreateAccountModal.vue'

const { showError, createAccount } = vi.hoisted(() => ({
  showError: vi.fn(),
  createAccount: vi.fn()
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
      create: vi.fn(),
      checkMixedChannelRisk: vi.fn()
    },
    settings: {
      getSettings: vi.fn(),
      getWebSearchEmulationConfig: vi.fn()
    },
    tlsFingerprintProfiles: { list: vi.fn() },
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

const mountModal = () => mount(CreateAccountModal, {
  props: {
    show: true,
    accountScope: 'user',
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
})
