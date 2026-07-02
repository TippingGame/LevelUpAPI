export type CustomErrorCodeToken = string
export type CustomErrorCodePayloadItem = number | string

export interface ParseCustomErrorCodeInputResult {
  tokens: CustomErrorCodeToken[]
  invalidTokens: string[]
}

interface ParsedStatusCodeToken {
  token: CustomErrorCodeToken
  start: number
  end: number
}

const HTTP_STATUS_MIN = 100
const HTTP_STATUS_MAX = 599

export function parseCustomErrorCodeInput(input: string): ParseCustomErrorCodeInputResult {
  const parts = input
    .replace(/，/g, ',')
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean)

  const tokens: CustomErrorCodeToken[] = []
  const invalidTokens: string[] = []
  for (const part of parts) {
    const parsed = parseStatusCodeToken(part)
    if (!parsed) {
      invalidTokens.push(part)
      continue
    }
    tokens.push(parsed.token)
  }

  return {
    tokens: sortCustomErrorCodeTokens(uniqueTokens(tokens)),
    invalidTokens
  }
}

export function customErrorCodeTokensFromValue(value: unknown): CustomErrorCodeToken[] {
  const tokens = collectCustomErrorCodeTokens(value)
  return sortCustomErrorCodeTokens(uniqueTokens(tokens))
}

export function customErrorCodeTokensToPayload(tokens: CustomErrorCodeToken[]): CustomErrorCodePayloadItem[] {
  return sortCustomErrorCodeTokens(uniqueTokens(tokens)).map((token) => {
    const parsed = parseStatusCodeToken(token)
    if (parsed && parsed.start === parsed.end) {
      return parsed.start
    }
    return token
  })
}

export function customErrorCodeTokensToCodes(tokens: CustomErrorCodeToken[]): number[] {
  const codes: number[] = []
  for (const token of sortCustomErrorCodeTokens(uniqueTokens(tokens))) {
    const parsed = parseStatusCodeToken(token)
    if (!parsed) {
      continue
    }
    for (let code = parsed.start; code <= parsed.end; code += 1) {
      codes.push(code)
    }
  }
  return codes
}

export function sortCustomErrorCodeTokens(tokens: CustomErrorCodeToken[]): CustomErrorCodeToken[] {
  return [...tokens].sort((a, b) => {
    const pa = parseStatusCodeToken(a)
    const pb = parseStatusCodeToken(b)
    if (!pa && !pb) return a.localeCompare(b)
    if (!pa) return 1
    if (!pb) return -1
    if (pa.start !== pb.start) return pa.start - pb.start
    if (pa.end !== pb.end) return pa.end - pb.end
    return pa.token.localeCompare(pb.token)
  })
}

export function customErrorCodeTokenIncludesCode(token: CustomErrorCodeToken, code: number): boolean {
  const parsed = parseStatusCodeToken(token)
  return Boolean(parsed && code >= parsed.start && code <= parsed.end)
}

export function customErrorCodeTokensIncludeCode(tokens: CustomErrorCodeToken[], code: number): boolean {
  return tokens.some((token) => customErrorCodeTokenIncludesCode(token, code))
}

function collectCustomErrorCodeTokens(value: unknown): CustomErrorCodeToken[] {
  if (value === null || value === undefined) {
    return []
  }
  if (typeof value === 'number') {
    return tokenFromNumber(value)
  }
  if (typeof value === 'string') {
    return parseCustomErrorCodeInput(value).tokens
  }
  if (Array.isArray(value)) {
    return value.flatMap((item) => collectCustomErrorCodeTokens(item))
  }
  return []
}

function parseStatusCodeToken(raw: string): ParsedStatusCodeToken | null {
  const token = raw.trim().replace(/\s+/g, '')
  if (!token) {
    return null
  }

  const rangeMatch = token.match(/^(\d{3})-(\d{3})$/)
  if (rangeMatch) {
    const start = Number(rangeMatch[1])
    const end = Number(rangeMatch[2])
    if (!isHttpStatusCode(start) || !isHttpStatusCode(end) || start > end) {
      return null
    }
    if (start === end) {
      return { token: String(start), start, end }
    }
    return { token: `${start}-${end}`, start, end }
  }

  if (!/^\d{3}$/.test(token)) {
    return null
  }
  const code = Number(token)
  if (!isHttpStatusCode(code)) {
    return null
  }
  return { token: String(code), start: code, end: code }
}

function tokenFromNumber(value: number): CustomErrorCodeToken[] {
  if (!Number.isInteger(value) || !isHttpStatusCode(value)) {
    return []
  }
  return [String(value)]
}

function uniqueTokens(tokens: CustomErrorCodeToken[]): CustomErrorCodeToken[] {
  return Array.from(new Set(tokens))
}

function isHttpStatusCode(code: number): boolean {
  return Number.isInteger(code) && code >= HTTP_STATUS_MIN && code <= HTTP_STATUS_MAX
}
