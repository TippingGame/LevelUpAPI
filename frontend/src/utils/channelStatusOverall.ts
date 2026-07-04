import type { AccountQuotaGroupSummary } from '@/types'
import {
  accountQuotaGroupHealthRank,
  resolveAccountQuotaGroupHealth,
  type AccountQuotaGroupHealth,
} from '@/utils/accountQuotaHealth'

export type ChannelStatusOverallStatus = 'operational' | 'degraded' | 'constrained' | 'unavailable'

export interface ChannelStatusMonitorSummary {
  primary_status?: string | null
}

export function resolveChannelStatusOverallStatus(
  groupSummaries: AccountQuotaGroupSummary[] = [],
  monitorItems: ChannelStatusMonitorSummary[] = []
): ChannelStatusOverallStatus {
  let quotaStatus: AccountQuotaGroupHealth = 'normal'
  for (const summary of groupSummaries) {
    const status = resolveAccountQuotaGroupHealth(summary)
    if (accountQuotaGroupHealthRank(status) > accountQuotaGroupHealthRank(quotaStatus)) {
      quotaStatus = status
    }
  }

  if (quotaStatus === 'unavailable') return 'unavailable'

  const hasMonitorFailure = monitorItems.some((item) => {
    return item.primary_status === 'failed' || item.primary_status === 'error'
  })
  if (hasMonitorFailure) {
    return groupSummaries.length > 0 ? 'degraded' : 'unavailable'
  }

  return quotaStatus === 'normal' ? 'operational' : quotaStatus
}
