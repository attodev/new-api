import type {
  TopupAmountMode,
  TossPaymentConfirmation,
  TossPaymentSession,
  TossTopupQuote,
} from '../types'

export const TOSS_KRW_PRESETS = [10000, 50000, 100000, 200000, 500000, 1000000]

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
  return `${new Intl.NumberFormat('ko-KR', {
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

function isPositiveFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
}

function isPositiveSafeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0
}

/**
 * Captures the exact authoritative quote shown in the confirmation dialog.
 * Legacy/partial amount responses are deliberately rejected: without every
 * pricing field there is no safe way to prove that the later checkout session
 * still represents what the buyer reviewed.
 */
export function createTossPaymentConfirmation(
  inputAmount: number,
  amountMode: TopupAmountMode,
  quote: Partial<TossTopupQuote> | null | undefined
): Readonly<TossPaymentConfirmation> | null {
  const chargeAmount = quote?.charge_amount
  const creditAmount = quote?.credit_amount
  const creditQuota = quote?.credit_quota
  const unitPrice = quote?.unit_price
  if (
    !isPositiveSafeInteger(inputAmount) ||
    quote?.input_amount !== inputAmount ||
    quote.amount_mode !== amountMode ||
    !isPositiveSafeInteger(chargeAmount) ||
    !isPositiveFiniteNumber(creditAmount) ||
    !isPositiveSafeInteger(creditQuota) ||
    !isPositiveFiniteNumber(unitPrice)
  ) {
    return null
  }

  return Object.freeze({
    input_amount: inputAmount,
    amount_mode: amountMode,
    charge_amount: chargeAmount,
    credit_amount: creditAmount,
    credit_quota: creditQuota,
    unit_price: unitPrice,
  })
}

/**
 * The backend creates the durable order and recomputes its quote in `/toss/pay`.
 * Only an exact match may proceed; a price/group/configuration change requires
 * a new buyer confirmation instead of silently charging the new amount.
 */
export function tossPaymentSessionMatchesConfirmation(
  session: TossPaymentSession,
  confirmation: Readonly<TossPaymentConfirmation>
): boolean {
  return (
    session.amount === confirmation.charge_amount &&
    session.charge_amount === confirmation.charge_amount &&
    session.credit_amount === confirmation.credit_amount &&
    session.credit_quota === confirmation.credit_quota &&
    session.unit_price === confirmation.unit_price &&
    session.amount_mode === confirmation.amount_mode
  )
}
