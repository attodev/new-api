/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import {
  PAYMENT_TYPES,
  DEFAULT_PRESET_MULTIPLIERS,
  DEFAULT_PAYMENT_TYPE,
  DEFAULT_MIN_TOPUP,
} from '../constants'
import type {
  PresetAmount,
  TopupInfo,
  TossBillingAuthSession,
  TossPaymentSession,
} from '../types'

// ============================================================================
// Payment Processing Functions
// ============================================================================

/**
 * Check if browser is Safari
 */
function isSafariBrowser(): boolean {
  return (
    navigator.userAgent.indexOf('Safari') > -1 &&
    navigator.userAgent.indexOf('Chrome') < 1
  )
}

/**
 * Submit payment form (for non-Stripe payments)
 */
export function submitPaymentForm(
  url: string,
  params: Record<string, unknown>
): void {
  const form = document.createElement('form')
  form.action = url
  form.method = 'POST'

  // Don't open in new tab for Safari
  if (!isSafariBrowser()) {
    form.target = '_blank'
  }

  // Add form parameters
  Object.entries(params).forEach(([key, value]) => {
    const input = document.createElement('input')
    input.type = 'hidden'
    input.name = key
    input.value = String(value)
    form.appendChild(input)
  })

  document.body.appendChild(form)
  form.submit()
  document.body.removeChild(form)
}

/**
 * Check if payment method is Stripe
 */
export function isStripePayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.STRIPE
}

/**
 * Check if payment method is PayPal
 */
export function isPayPalPayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.PAYPAL
}

/**
 * Check if payment method is Waffo Pancake
 *
 * Pancake is a metered-style payment that goes through a dedicated checkout
 * URL flow rather than the generic epay form submission, so it must be
 * special-cased in payment dispatch logic.
 */
export function isWaffoPancakePayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.WAFFO_PANCAKE
}

/**
 * Check if payment method is Toss
 *
 * Toss uses the v2 SDK to open a payment window via requestPayment(),
 * which redirects the browser to the Toss checkout — it must be
 * special-cased in payment dispatch logic.
 */
export function isTossPayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.TOSS
}

/**
 * Toss minimums apply to the authoritative KRW quote returned by the server.
 * In quota mode a browser preview cannot include the user's group ratio and
 * amount discount, so it must never disable or short-circuit the quote request.
 */
export function shouldBlockPaymentMethodBeforeQuote(
  paymentType: string,
  paymentAmount: number,
  minimumAmount: number
): boolean {
  return !isTossPayment(paymentType) && paymentAmount < minimumAmount
}

// SDK v2 CARD opens the card/easy-pay unified window. Linked-account easy-pay
// requires at least 200 KRW even though a card-only payment starts at 100 KRW.
const TOSS_GENERAL_MINIMUM_KRW = 200
const TOSS_MAXIMUM_CHARGE_KRW = 2_147_483_647
const TOSS_ORDER_ID_PATTERN = /^[A-Za-z0-9_-]{6,64}$/
// The server Billing API accepts a wider key, but payment({ customerKey }) in
// the JavaScript SDK v2 is explicitly limited to 50 characters.
const TOSS_CUSTOMER_KEY_MAX_LENGTH = 50
const TOSS_CUSTOMER_KEY_PATTERN = /^[A-Za-z0-9_.=@-]{2,50}$/

function isNonBlankBoundedString(
  value: unknown,
  maxLength: number
): value is string {
  return (
    typeof value === 'string' &&
    value === value.trim() &&
    value.length > 0 &&
    value.length <= maxLength
  )
}

function isTossCustomerKey(value: unknown): value is string {
  return (
    isNonBlankBoundedString(value, TOSS_CUSTOMER_KEY_MAX_LENGTH) &&
    TOSS_CUSTOMER_KEY_PATTERN.test(value) &&
    /[-_=.@]/.test(value)
  )
}

type TossBillingCallbackPurpose = 'subscription' | 'wallet'

function parseTossCallbackUrl(value: unknown): URL | null {
  if (!isNonBlankBoundedString(value, 2048)) return null

  try {
    const parsed = new URL(value)
    if (parsed.username || parsed.password || parsed.hash) return null
    if (parsed.protocol === 'https:') return parsed
    if (parsed.protocol !== 'http:') return null

    const hostname = parsed.hostname.toLowerCase().replace(/^\[|\]$/g, '')
    return hostname === 'localhost' ||
      hostname === '127.0.0.1' ||
      hostname === '::1'
      ? parsed
      : null
  } catch {
    return null
  }
}

function hasOnlyTradeNoQuery(callback: URL, tradeNo: string): boolean {
  const entries = Array.from(callback.searchParams.entries())
  return (
    entries.length === 1 &&
    entries[0]?.[0] === 'trade_no' &&
    entries[0]?.[1] === tradeNo
  )
}

function isExpectedTossCallbackPair(
  successValue: unknown,
  failValue: unknown,
  purpose: 'payment' | TossBillingCallbackPurpose,
  tradeNo?: string
): boolean {
  const success = parseTossCallbackUrl(successValue)
  const fail = parseTossCallbackUrl(failValue)
  if (!success || !fail || success.origin !== fail.origin) return false

  if (purpose === 'payment') {
    return (
      success.pathname.endsWith('/api/toss/confirm') &&
      fail.pathname.endsWith('/api/toss/fail') &&
      success.search === '' &&
      fail.search === ''
    )
  }

  if (!tradeNo) return false
  if (purpose === 'subscription') {
    return (
      success.pathname.endsWith(`/api/subscription/toss/confirm/${tradeNo}`) &&
      fail.pathname.endsWith(`/api/subscription/toss/fail/${tradeNo}`) &&
      success.search === '' &&
      fail.search === ''
    )
  }

  return (
    success.pathname.endsWith('/api/wallet/auto-recharge/toss/confirm') &&
    fail.pathname.endsWith('/api/wallet/auto-recharge/toss/fail') &&
    hasOnlyTradeNoQuery(success, tradeNo) &&
    hasOnlyTradeNoQuery(fail, tradeNo)
  )
}

export type TossPaymentWindowTargetOptions =
  | Record<string, never>
  | { windowTarget: 'self' }

/**
 * Toss's redirect iframe requires callbacks to share the page's origin. Keep
 * the SDK's responsive default (iframe on PC, self on mobile) when possible;
 * use a full-page redirect when the configured callback server is cross-origin.
 */
export function getTossPaymentWindowTargetOptions(
  successValue: unknown,
  failValue: unknown,
  browserOrigin: string = typeof window === 'undefined'
    ? ''
    : window.location.origin
): TossPaymentWindowTargetOptions {
  const success = parseTossCallbackUrl(successValue)
  const fail = parseTossCallbackUrl(failValue)
  let currentOrigin = ''
  try {
    currentOrigin = new URL(browserOrigin).origin
  } catch {
    return { windowTarget: 'self' }
  }
  if (
    !success ||
    !fail ||
    success.origin !== currentOrigin ||
    fail.origin !== currentOrigin
  ) {
    return { windowTarget: 'self' }
  }
  return {}
}

/**
 * Validate the server-created checkout session before handing it to the Toss
 * SDK. The backend remains authoritative; this guard prevents malformed or
 * stale API responses from opening a checkout with a different amount/order.
 */
export function isValidTossPaymentSession(
  value: unknown
): value is TossPaymentSession {
  if (!value || typeof value !== 'object') return false
  const session = value as Record<string, unknown>

  return (
    isNonBlankBoundedString(session.client_key, 2048) &&
    isTossCustomerKey(session.customer_key) &&
    typeof session.order_id === 'string' &&
    TOSS_ORDER_ID_PATTERN.test(session.order_id) &&
    isNonBlankBoundedString(session.order_name, 100) &&
    Array.from(session.order_name).length <= 100 &&
    typeof session.amount === 'number' &&
    Number.isSafeInteger(session.amount) &&
    session.amount >= TOSS_GENERAL_MINIMUM_KRW &&
    session.amount <= TOSS_MAXIMUM_CHARGE_KRW &&
    isExpectedTossCallbackPair(session.success_url, session.fail_url, 'payment')
  )
}

export function isValidTossBillingAuthSession(
  value: unknown,
  purpose: TossBillingCallbackPurpose
): value is TossBillingAuthSession {
  if (!value || typeof value !== 'object') return false
  const session = value as Record<string, unknown>
  const tradeNo = getValidTossBillingAuthTradeNo(session)

  return (
    isNonBlankBoundedString(session.client_key, 2048) &&
    isTossCustomerKey(session.customer_key) &&
    tradeNo !== null &&
    isExpectedTossCallbackPair(
      session.success_url,
      session.fail_url,
      purpose,
      tradeNo
    )
  )
}

export function getValidTossBillingAuthTradeNo(value: unknown): string | null {
  if (!value || typeof value !== 'object') return null
  const tradeNo = (value as Record<string, unknown>).trade_no
  return typeof tradeNo === 'string' && TOSS_ORDER_ID_PATTERN.test(tradeNo)
    ? tradeNo
    : null
}

export function isTossUserCancellation(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const code = (error as { code?: unknown }).code
  return (
    code === 'USER_CANCEL' ||
    code === 'PAY_PROCESS_CANCELED' ||
    code === 'PAYMENT_REQUEST_ABORTED'
  )
}

export function shouldOpenPaymentConfirmDialog(
  paymentType: string,
  paymentAmount: number
): boolean {
  if (isTossPayment(paymentType)) {
    return paymentAmount > 0
  }

  return true
}

/**
 * Get default payment type from topup info
 */
export function getDefaultPaymentType(topupInfo: TopupInfo | null): string {
  if (!topupInfo) {
    return DEFAULT_PAYMENT_TYPE
  }

  // Return first available payment method or default
  if (topupInfo.pay_methods?.length > 0) {
    return topupInfo.pay_methods[0].type
  }

  if (topupInfo.enable_stripe_topup) {
    return PAYMENT_TYPES.STRIPE
  }

  if (topupInfo.enable_waffo_topup) {
    return PAYMENT_TYPES.WAFFO
  }

  if (topupInfo.enable_waffo_pancake_topup) {
    return PAYMENT_TYPES.WAFFO_PANCAKE
  }

  if (topupInfo.enable_toss_topup) {
    return PAYMENT_TYPES.TOSS
  }

  return DEFAULT_PAYMENT_TYPE
}

/**
 * Get minimum topup amount from topup info
 */
export function getMinTopupAmount(topupInfo: TopupInfo | null): number {
  if (!topupInfo) {
    return DEFAULT_MIN_TOPUP
  }

  if (topupInfo.enable_online_topup) {
    return topupInfo.min_topup
  }

  if (topupInfo.enable_stripe_topup) {
    return topupInfo.stripe_min_topup
  }

  if (topupInfo.enable_paypal_topup) {
    return topupInfo.paypal_min_topup || DEFAULT_MIN_TOPUP
  }

  if (topupInfo.enable_waffo_topup) {
    return topupInfo.waffo_min_topup || DEFAULT_MIN_TOPUP
  }

  if (topupInfo.enable_waffo_pancake_topup) {
    return topupInfo.waffo_pancake_min_topup || DEFAULT_MIN_TOPUP
  }

  if (topupInfo.enable_toss_topup) {
    return topupInfo.toss_min_topup || DEFAULT_MIN_TOPUP
  }

  return DEFAULT_MIN_TOPUP
}

/**
 * Generate preset amounts based on minimum topup
 */
export function generatePresetAmounts(minAmount: number): PresetAmount[] {
  return DEFAULT_PRESET_MULTIPLIERS.map((multiplier) => ({
    value: minAmount * multiplier,
  }))
}

/**
 * Merge custom preset amounts with discounts
 */
export function mergePresetAmounts(
  amountOptions: number[],
  discounts: Record<number, number>
): PresetAmount[] {
  if (!amountOptions || amountOptions.length === 0) {
    return []
  }

  return amountOptions.map((amount) => ({
    value: amount,
    discount: discounts[amount] || 1.0,
  }))
}
