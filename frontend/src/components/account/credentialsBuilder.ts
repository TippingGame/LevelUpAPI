export function applyInterceptWarmup(
  credentials: Record<string, unknown>,
  enabled: boolean,
  mode: 'create' | 'edit'
): void {
  if (enabled) {
    credentials.intercept_warmup_requests = true
  } else if (mode === 'edit') {
    delete credentials.intercept_warmup_requests
  }
}

export const HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY = 'header_override_enabled'
export const HEADER_OVERRIDES_CREDENTIAL_KEY = 'header_overrides'

export interface HeaderOverrideRow {
  name: string
  value: string
}

export function isHeaderOverrideCapable(platform: string, type: string): boolean {
  if (platform === 'anthropic' || platform === 'openai') return type === 'apikey'
  if (platform === 'grok') return type === 'apikey' || type === 'oauth'
  return false
}

const HEADER_OVERRIDE_BLOCKED_NAMES = new Set([
  'host', 'content-length', 'content-type', 'transfer-encoding', 'connection',
  'keep-alive', 'proxy-authenticate', 'proxy-authorization', 'proxy-connection',
  'te', 'trailer', 'upgrade', 'authorization', 'x-api-key', 'x-goog-api-key',
  'cookie', 'accept-encoding', 'sec-websocket-key', 'sec-websocket-version',
  'sec-websocket-extensions', 'sec-websocket-protocol', 'sec-websocket-accept',
  'session_id', 'conversation_id', 'x-codex-turn-state', 'x-codex-turn-metadata',
  'chatgpt-account-id', 'x-claude-code-session-id', 'x-client-request-id', 'x-grok-conv-id'
])
const HEADER_NAME_PATTERN = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/
// eslint-disable-next-line no-control-regex
const HEADER_VALUE_INVALID_PATTERN = /[\x00-\x08\x0a-\x1f\x7f]/
const HEADER_OVERRIDE_MAX_ENTRIES = 64
const HEADER_OVERRIDE_MAX_NAME_LENGTH = 200
const HEADER_OVERRIDE_MAX_VALUE_LENGTH = 8192
const HEADER_TEXT_ENCODER = new TextEncoder()

function utf8ByteLength(value: string): number {
  return HEADER_TEXT_ENCODER.encode(value).length
}

export function validateHeaderOverrideRows(
  rows: HeaderOverrideRow[]
): 'invalidName' | 'blockedName' | 'duplicateName' | 'invalidValue' | 'tooManyEntries' | null {
  const seen = new Set<string>()
  for (const row of rows) {
    const name = row.name.trim()
    const value = row.value.trim()
    if (!name) {
      if (value) return 'invalidName'
      continue
    }
    if (!HEADER_NAME_PATTERN.test(name) || name.length > HEADER_OVERRIDE_MAX_NAME_LENGTH) return 'invalidName'
    const lower = name.toLowerCase()
    if (HEADER_OVERRIDE_BLOCKED_NAMES.has(lower)) return 'blockedName'
    if (seen.has(lower)) return 'duplicateName'
    if (HEADER_VALUE_INVALID_PATTERN.test(value) || utf8ByteLength(value) > HEADER_OVERRIDE_MAX_VALUE_LENGTH) {
      return 'invalidValue'
    }
    seen.add(lower)
  }
  return seen.size > HEADER_OVERRIDE_MAX_ENTRIES ? 'tooManyEntries' : null
}

export function buildHeaderOverridesObject(rows: HeaderOverrideRow[]): Record<string, string> {
  const result: Record<string, string> = {}
  for (const row of rows) {
    const name = row.name.trim().toLowerCase()
    if (name) result[name] = row.value.trim()
  }
  return result
}

export function splitHeaderOverridesObject(record: unknown): HeaderOverrideRow[] {
  if (!record || typeof record !== 'object' || Array.isArray(record)) return []
  return Object.entries(record as Record<string, unknown>)
    .filter(([, value]) => typeof value === 'string')
    .map(([name, value]) => ({ name, value: value as string }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

export function parseHeaderOverridesJson(text: string): HeaderOverrideRow[] | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null
  const rows: HeaderOverrideRow[] = []
  for (const [rawName, rawValue] of Object.entries(parsed as Record<string, unknown>)) {
    const name = rawName.trim()
    if (!name) continue
    if (typeof rawValue !== 'string' && typeof rawValue !== 'number' && typeof rawValue !== 'boolean') return null
    rows.push({ name, value: String(rawValue).trim() })
  }
  return rows.sort((a, b) => a.name.localeCompare(b.name))
}

export function serializeHeaderOverrideRows(rows: HeaderOverrideRow[]): string {
  return JSON.stringify(buildHeaderOverridesObject(rows), null, 2)
}

const GROK_OFFICIAL_BASE_URL_HOSTS = new Set(['api.x.ai', 'cli-chat-proxy.grok.com'])

export function isCustomGrokBaseUrl(value: unknown): boolean {
  if (typeof value !== 'string' || !value.trim()) return false
  try {
    const parsed = new URL(value.trim())
    return !GROK_OFFICIAL_BASE_URL_HOSTS.has(parsed.hostname.toLowerCase())
  } catch {
    return false
  }
}

export function applyHeaderOverride(
  credentials: Record<string, unknown>,
  enabled: boolean,
  rows: HeaderOverrideRow[],
  mode: 'create' | 'edit'
): void {
  if (enabled) {
    credentials[HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY] = true
    credentials[HEADER_OVERRIDES_CREDENTIAL_KEY] = buildHeaderOverridesObject(rows)
  } else if (mode === 'edit') {
    delete credentials[HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY]
    delete credentials[HEADER_OVERRIDES_CREDENTIAL_KEY]
  }
}
