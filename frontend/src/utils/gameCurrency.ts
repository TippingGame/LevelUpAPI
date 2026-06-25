export interface GameCurrencyFormatOptions {
  minimumFractionDigits?: number
  maximumFractionDigits?: number
  signed?: boolean
}

const DEFAULT_LOCALE = 'en-US'
export const GAME_CURRENCY_UNIT = 'coins'

export function formatGameCurrencyNumber(
  value: number | string | null | undefined,
  options: GameCurrencyFormatOptions = {},
): string {
  const amount = Number(value || 0)
  const safeAmount = Number.isFinite(amount) ? amount : 0
  const minimumFractionDigits = options.minimumFractionDigits ?? 2
  const maximumFractionDigits = options.maximumFractionDigits ?? minimumFractionDigits
  const sign = options.signed && safeAmount > 0 ? '+' : ''

  return `${sign}${new Intl.NumberFormat(DEFAULT_LOCALE, {
    minimumFractionDigits,
    maximumFractionDigits,
  }).format(safeAmount)}`
}

export function formatGameCoins(
  value: number | string | null | undefined,
  options: GameCurrencyFormatOptions = {},
): string {
  return `${formatGameCurrencyNumber(value, options)} ${GAME_CURRENCY_UNIT}`
}

export function formatGameCoinRange(
  min: number | string | null | undefined,
  max: number | string | null | undefined,
): string {
  return `${formatGameCoins(min)} - ${formatGameCoins(max)}`
}
