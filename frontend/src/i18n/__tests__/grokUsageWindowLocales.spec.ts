import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe('Grok usage window locale keys', () => {
  it('contains the Chinese quota status labels', () => {
    expect(zh.admin.accounts.usageWindow.grokUnknown).toContain('Grok 配额')
    expect(zh.admin.accounts.usageWindow.grokLastStatus).toContain('{status}')
    expect(zh.admin.accounts.usageWindow.grokLastProbe).toContain('{time}')
    expect(zh.admin.accounts.usageWindow.grokLastHeadersSeen).toContain('{time}')
  })

  it('contains the English quota status labels', () => {
    expect(en.admin.accounts.usageWindow.grokUnknown).toContain('Grok quota')
    expect(en.admin.accounts.usageWindow.grokLastStatus).toContain('{status}')
    expect(en.admin.accounts.usageWindow.grokLastProbe).toContain('{time}')
    expect(en.admin.accounts.usageWindow.grokLastHeadersSeen).toContain('{time}')
  })
})
