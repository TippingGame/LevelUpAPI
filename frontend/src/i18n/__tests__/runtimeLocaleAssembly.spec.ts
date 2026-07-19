import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe.each([
  ['zh', zh, '上游声明倍率', '当前并发', '部署与运营合规确认'],
  ['en', en, 'Upstream Declared Rate', 'Current Concurrency', 'Deployment and Operation Compliance Acknowledgment']
] as const)('runtime %s locale assembly', (_locale, messages, billingRate, concurrency, complianceTitle) => {
  it('includes modular admin and dashboard messages', () => {
    expect(messages.admin.accounts.columns.upstreamBillingRate).toBe(billingRate)
    expect(messages.keys.currentConcurrency).toBe(concurrency)
    expect(messages.adminCompliance.title).toBe(complianceTitle)
  })

  it('preserves LevelUp-only legacy messages', () => {
    expect(messages.agentExperience.title).toBeTruthy()
  })
})
