import { describe, expect, it } from 'vitest'
import {
  customErrorCodeTokenIncludesCode,
  customErrorCodeTokensFromValue,
  customErrorCodeTokensToCodes,
  customErrorCodeTokensToPayload,
  parseCustomErrorCodeInput
} from '../customErrorCodePolicy'

describe('customErrorCodePolicy', () => {
  it('parses exact status codes and ranges', () => {
    const result = parseCustomErrorCodeInput('401, 403-407，500-503, 600, bad')

    expect(result.tokens).toEqual(['401', '403-407', '500-503'])
    expect(result.invalidTokens).toEqual(['600', 'bad'])
  })

  it('normalizes old numeric arrays and new mixed payloads', () => {
    expect(customErrorCodeTokensFromValue([429, '500-503', '401,403-407'])).toEqual([
      '401',
      '403-407',
      '429',
      '500-503'
    ])
  })

  it('keeps exact codes numeric in payload and ranges as strings', () => {
    expect(customErrorCodeTokensToPayload(['500-503', '401', '429'])).toEqual([
      401,
      429,
      '500-503'
    ])
  })

  it('expands ranges to exact status codes', () => {
    expect(customErrorCodeTokensToCodes(['500-503', '401', '429'])).toEqual([
      401,
      429,
      500,
      501,
      502,
      503
    ])
  })

  it('detects warning codes inside a range', () => {
    expect(customErrorCodeTokenIncludesCode('400-499', 429)).toBe(true)
    expect(customErrorCodeTokenIncludesCode('500-503', 529)).toBe(false)
  })
})
