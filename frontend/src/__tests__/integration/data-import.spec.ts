import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ImportDataModal from '@/components/admin/account/ImportDataModal.vue'

const showError = vi.fn()
const showSuccess = vi.fn()
const showWarning = vi.fn()

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showWarning
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      importData: vi.fn(),
      importCredentialContents: vi.fn()
    }
  }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const mountModal = () =>
  mount(ImportDataModal, {
    props: { show: true },
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
      }
    }
  })

const makeJsonFile = (name: string, content: string) => {
  const file = new File([content], name, { type: 'application/json' })
  Object.defineProperty(file, 'text', {
    value: () => Promise.resolve(content)
  })
  return file
}

const setInputFiles = (element: Element, files: File[]) => {
  Object.defineProperty(element, 'files', {
    value: files,
    configurable: true
  })
}

describe('ImportDataModal', () => {
  beforeEach(async () => {
    showError.mockReset()
    showSuccess.mockReset()
    showWarning.mockReset()
    const { adminAPI } = await import('@/api/admin')
    vi.mocked(adminAPI.accounts.importData).mockReset()
    vi.mocked(adminAPI.accounts.importCredentialContents).mockReset()
  })

  it('未选择文件时提示错误', async () => {
    const wrapper = mountModal()

    await wrapper.find('form').trigger('submit')
    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportSelectFile')
  })

  it('无效 JSON 时按文件提示解析失败', async () => {
    const { adminAPI } = await import('@/api/admin')
    const wrapper = mountModal()

    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [makeJsonFile('data.json', 'invalid json')])

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportParseFailedFile')
    expect(adminAPI.accounts.importData).not.toHaveBeenCalled()
    expect(adminAPI.accounts.importCredentialContents).not.toHaveBeenCalled()
  })

  it('合并多个导出数据文件后一次导入', async () => {
    const { adminAPI } = await import('@/api/admin')
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 1,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 2,
      account_failed: 0
    })
    const wrapper = mountModal()
    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'first.json',
        JSON.stringify({ exported_at: '2026-07-01T00:00:00Z', proxies: [], accounts: [{ name: 'a' }] })
      ),
      makeJsonFile(
        'second.json',
        JSON.stringify({
          exported_at: '2026-07-02T00:00:00Z',
          proxies: [{ proxy_key: 'p' }],
          accounts: [{ name: 'b' }]
        })
      )
    ])

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenCalledWith({
      data: expect.objectContaining({
        proxies: [{ proxy_key: 'p' }],
        accounts: [{ name: 'a' }, { name: 'b' }]
      }),
      skip_default_group_bind: true
    })
    expect(wrapper.emitted('imported')).toHaveLength(1)
  })

  it('拖拽多个账号凭证文件会走批量凭证导入', async () => {
    const { adminAPI } = await import('@/api/admin')
    vi.mocked(adminAPI.accounts.importCredentialContents).mockResolvedValue({
      total: 2,
      created: 2,
      failed: 0,
      errors: []
    })
    const wrapper = mountModal()
    const first = JSON.stringify({ platform: 'grok', refresh_token: 'rt-1' })
    const second = JSON.stringify({ platform: 'grok', refresh_token: 'rt-2' })

    await wrapper.get('[data-testid="data-import-drop-zone"]').trigger('drop', {
      dataTransfer: { files: [makeJsonFile('first.json', first), makeJsonFile('second.json', second)] }
    })
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importCredentialContents).toHaveBeenCalledWith(
      expect.objectContaining({ contents: [first, second] })
    )
    expect(adminAPI.accounts.importData).not.toHaveBeenCalled()
  })

  it('拒绝在同一批次混合导出数据与账号凭证', async () => {
    const { adminAPI } = await import('@/api/admin')
    const wrapper = mountModal()
    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'bundle.json',
        JSON.stringify({ exported_at: '2026-07-01T00:00:00Z', proxies: [], accounts: [] })
      ),
      makeJsonFile('credential.json', JSON.stringify({ platform: 'grok', refresh_token: 'rt' }))
    ])

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportMixedFormats')
    expect(adminAPI.accounts.importData).not.toHaveBeenCalled()
    expect(adminAPI.accounts.importCredentialContents).not.toHaveBeenCalled()
  })

  it('逐文件校验导出格式版本，避免合并后绕过后端校验', async () => {
    const { adminAPI } = await import('@/api/admin')
    const wrapper = mountModal()
    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'unsupported.json',
        JSON.stringify({ type: 'other-export', version: 99, exported_at: '2026-07-01T00:00:00Z', proxies: [], accounts: [] })
      )
    ])

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportInvalidFile')
    expect(adminAPI.accounts.importData).not.toHaveBeenCalled()
  })

  it('部分成功时关闭弹窗会通知父组件刷新', async () => {
    const { adminAPI } = await import('@/api/admin')
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 1
    })
    const wrapper = mountModal()
    const input = wrapper.find('input[type="file"]')
    setInputFiles(input.element, [
      makeJsonFile(
        'partial.json',
        JSON.stringify({ exported_at: '2026-07-01T00:00:00Z', proxies: [], accounts: [{ name: 'a' }] })
      )
    ])

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.emitted('imported')).toBeUndefined()

    await wrapper.findAll('button.btn-secondary')[1]!.trigger('click')

    expect(wrapper.emitted('imported')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
