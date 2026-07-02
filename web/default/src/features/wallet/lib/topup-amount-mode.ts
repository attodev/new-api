import type { TopupAmountMode, TossTopupQuote } from '../types'

export const TOSS_KRW_PRESETS = [
  10000, 50000, 100000, 200000, 500000, 1000000,
]

export const TOSS_QUOTA_PRESETS = [10, 20, 50, 100, 200, 500]

export interface TossTopupPreview {
  chargeAmount: number
  creditAmount: number
}

export function normalizeTossUnitPrice(unitPrice: number | null | undefined) {
  return unitPrice && unitPrice > 0 ? unitPrice : 1
}

export function getTossPreview(
  amount: number,
  mode: TopupAmountMode,
  unitPrice: number | null | undefined
): TossTopupPreview {
  const unit = normalizeTossUnitPrice(unitPrice)
  if (mode === 'quota') {
    return {
      chargeAmount: Math.round(amount * unit),
      creditAmount: amount,
    }
  }
  return {
    chargeAmount: amount,
    creditAmount: amount / unit,
  }
}

export function formatWonAmount(amount: number): string {
  return `${new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 0,
  }).format(amount)}원`
}

export function parseTossQuoteData(data: unknown): Partial<TossTopupQuote> {
  if (typeof data === 'string' || typeof data === 'number') {
    const charge = Number(data)
    return Number.isFinite(charge) ? { charge_amount: charge } : {}
  }
  if (!data || typeof data !== 'object') {
    return {}
  }
  const record = data as Record<string, unknown>
  const mode =
    record.amount_mode === 'quota'
      ? 'quota'
      : record.amount_mode === 'krw'
        ? 'krw'
        : undefined
  const quote: Partial<TossTopupQuote> = {}
  if (mode) {
    quote.amount_mode = mode
  }
  if (typeof record.input_amount === 'number') {
    quote.input_amount = record.input_amount
  }
  if (typeof record.charge_amount === 'number') {
    quote.charge_amount = record.charge_amount
  }
  if (typeof record.credit_amount === 'number') {
    quote.credit_amount = record.credit_amount
  }
  if (typeof record.credit_quota === 'number') {
    quote.credit_quota = record.credit_quota
  }
  if (typeof record.unit_price === 'number') {
    quote.unit_price = record.unit_price
  }
  return quote
}
