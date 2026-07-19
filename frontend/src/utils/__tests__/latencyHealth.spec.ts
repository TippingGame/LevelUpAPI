import { describe, expect, it } from 'vitest'

import { durationSeverity, firstTokenSeverity, formatUsageDuration } from '../latencyHealth'

describe('latencyHealth', () => {
  it('classifies first-token latency at 10s/30s/60s boundaries', () => {
    expect(firstTokenSeverity(0)).toBe('good')
    expect(firstTokenSeverity(9_999)).toBe('good')
    expect(firstTokenSeverity(10_000)).toBe('warn')
    expect(firstTokenSeverity(29_999)).toBe('warn')
    expect(firstTokenSeverity(30_000)).toBe('slow')
    expect(firstTokenSeverity(59_999)).toBe('slow')
    expect(firstTokenSeverity(60_000)).toBe('critical')
  })

  it('classifies total duration at 1min/3min/5min boundaries', () => {
    expect(durationSeverity(0)).toBe('good')
    expect(durationSeverity(59_999)).toBe('good')
    expect(durationSeverity(60_000)).toBe('warn')
    expect(durationSeverity(179_999)).toBe('warn')
    expect(durationSeverity(180_000)).toBe('slow')
    expect(durationSeverity(299_999)).toBe('slow')
    expect(durationSeverity(300_000)).toBe('critical')
  })

  it('formats latency values for compact table display', () => {
    expect(formatUsageDuration(null)).toBe('-')
    expect(formatUsageDuration(Number.NaN)).toBe('-')
    expect(formatUsageDuration(999.4)).toBe('999ms')
    expect(formatUsageDuration(10_000)).toBe('10.00s')
    expect(formatUsageDuration(180_000)).toBe('3m 0s')
    expect(formatUsageDuration(3_900_000)).toBe('1h 5m')
  })
})
