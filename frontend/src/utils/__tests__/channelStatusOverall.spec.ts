import { describe, expect, it } from 'vitest'
import type { AccountQuotaDimensionSummary, AccountQuotaGroupSummary } from '@/types'
import { resolveChannelStatusOverallStatus } from '@/utils/channelStatusOverall'

const emptyDimension: AccountQuotaDimensionSummary = {
  enabled_account_count: 0,
  exhausted_account_count: 0,
  limit: 0,
  used: 0,
  remaining: 0,
  utilization: 0,
}

function quotaGroup(overrides: Partial<AccountQuotaGroupSummary> = {}): AccountQuotaGroupSummary {
  return {
    group_id: 1,
    group_name: 'PRO shared pool',
    group_status: 'active',
    platform: 'openai',
    account_count: 1,
    active_account_count: 1,
    schedulable_account_count: 1,
    rate_limited_account_count: 0,
    codex_quota_protected_account_count: 0,
    error_account_count: 0,
    disabled_account_count: 0,
    quota_account_count: 0,
    unlimited_account_count: 1,
    total: emptyDimension,
    daily: emptyDimension,
    weekly: emptyDimension,
    usage_windows: [],
    ...overrides,
  }
}

describe('resolveChannelStatusOverallStatus', () => {
  it('downgrades instead of marking unavailable when quota capacity is healthy but a monitor probe fails', () => {
    expect(resolveChannelStatusOverallStatus(
      [quotaGroup()],
      [{ primary_status: 'failed' }]
    )).toBe('degraded')
  })

  it('keeps unavailable when the quota pool has no schedulable accounts', () => {
    expect(resolveChannelStatusOverallStatus(
      [quotaGroup({ schedulable_account_count: 0 })],
      [{ primary_status: 'success' }]
    )).toBe('unavailable')
  })

  it('keeps monitor failures unavailable when there is no quota capacity evidence', () => {
    expect(resolveChannelStatusOverallStatus(
      [],
      [{ primary_status: 'error' }]
    )).toBe('unavailable')
  })
})
