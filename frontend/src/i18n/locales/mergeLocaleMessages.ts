type LocaleMessages = Record<string, unknown>

function isPlainObject(value: unknown): value is LocaleMessages {
  return Object.prototype.toString.call(value) === '[object Object]'
}

export function mergeLocaleMessages<T extends LocaleMessages, U extends LocaleMessages>(
  base: T,
  override: U
): T & U {
  const merged: LocaleMessages = { ...base }

  for (const [key, value] of Object.entries(override)) {
    const current = merged[key]
    merged[key] = isPlainObject(current) && isPlainObject(value)
      ? mergeLocaleMessages(current, value)
      : value
  }

  return merged as T & U
}
