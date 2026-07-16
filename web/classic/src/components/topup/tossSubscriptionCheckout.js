/*
Copyright (C) 2025 QuantumNous

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

const TOSS_ORDER_ID_PATTERN = /^[A-Za-z0-9_-]{6,64}$/;
const TOSS_CUSTOMER_KEY_MAX_LENGTH = 50;
const TOSS_CUSTOMER_KEY_PATTERN = /^[A-Za-z0-9_.=@-]{2,50}$/;
const TOSS_CHECKOUT_FINGERPRINT_PATTERN = /^[a-f0-9]{40}$/;
const TOSS_CARD_MINIMUM_KRW = 100;
const TOSS_MAXIMUM_CHARGE_KRW = 2147483647;

function isNonBlankBoundedString(value, maxLength) {
  return (
    typeof value === 'string' &&
    value === value.trim() &&
    value.length > 0 &&
    value.length <= maxLength
  );
}

function isTossCustomerKey(value) {
  return (
    isNonBlankBoundedString(value, TOSS_CUSTOMER_KEY_MAX_LENGTH) &&
    TOSS_CUSTOMER_KEY_PATTERN.test(value) &&
    /[-_=.@]/.test(value)
  );
}

function parseTossCallbackUrl(value) {
  if (!isNonBlankBoundedString(value, 2048)) return null;
  try {
    const parsed = new URL(value);
    if (parsed.username || parsed.password || parsed.hash) return null;
    if (parsed.protocol === 'https:') return parsed;
    if (parsed.protocol !== 'http:') return null;
    const hostname = parsed.hostname.toLowerCase().replace(/^\[|\]$/g, '');
    return hostname === 'localhost' ||
      hostname === '127.0.0.1' ||
      hostname === '::1'
      ? parsed
      : null;
  } catch {
    return null;
  }
}

function hasOnlyTradeNoQuery(callback, tradeNo) {
  const entries = Array.from(callback.searchParams.entries());
  return (
    entries.length === 1 &&
    entries[0]?.[0] === 'trade_no' &&
    entries[0]?.[1] === tradeNo
  );
}

export function isTossCallbackPair(
  successValue,
  failValue,
  purpose,
  tradeNo = '',
) {
  const success = parseTossCallbackUrl(successValue);
  const fail = parseTossCallbackUrl(failValue);
  if (!success || !fail || success.origin !== fail.origin) return false;

  if (purpose === 'payment') {
    return (
      success.pathname.endsWith('/api/toss/confirm') &&
      fail.pathname.endsWith('/api/toss/fail') &&
      success.search === '' &&
      fail.search === ''
    );
  }

  if (!tradeNo) return false;
  if (purpose === 'subscription') {
    return (
      success.pathname.endsWith(`/api/subscription/toss/confirm/${tradeNo}`) &&
      fail.pathname.endsWith(`/api/subscription/toss/fail/${tradeNo}`) &&
      success.search === '' &&
      fail.search === ''
    );
  }

  if (purpose !== 'wallet') return false;
  return (
    success.pathname.endsWith('/api/wallet/auto-recharge/toss/confirm') &&
    fail.pathname.endsWith('/api/wallet/auto-recharge/toss/fail') &&
    hasOnlyTradeNoQuery(success, tradeNo) &&
    hasOnlyTradeNoQuery(fail, tradeNo)
  );
}

export function getTossPaymentWindowTargetOptions(
  successValue,
  failValue,
  browserOrigin = typeof window === 'undefined' ? '' : window.location.origin,
) {
  const success = parseTossCallbackUrl(successValue);
  const fail = parseTossCallbackUrl(failValue);
  let currentOrigin = '';
  try {
    currentOrigin = new URL(browserOrigin).origin;
  } catch {
    return { windowTarget: 'self' };
  }
  if (
    !success ||
    !fail ||
    success.origin !== currentOrigin ||
    fail.origin !== currentOrigin
  ) {
    return { windowTarget: 'self' };
  }
  return {};
}

function normalizeTossSubscriptionCheckout(value) {
  if (
    !value ||
    typeof value !== 'object' ||
    !Number.isSafeInteger(value.plan_id) ||
    value.plan_id <= 0 ||
    typeof value.plan_title !== 'string' ||
    value.plan_title.trim().length === 0 ||
    typeof value.price_amount !== 'number' ||
    !Number.isFinite(value.price_amount) ||
    value.price_amount <= 0 ||
    typeof value.price_currency !== 'string' ||
    value.price_currency !== value.price_currency.trim().toUpperCase() ||
    value.price_currency.length === 0 ||
    !Number.isSafeInteger(value.provider_amount) ||
    value.provider_amount < TOSS_CARD_MINIMUM_KRW ||
    value.provider_amount > TOSS_MAXIMUM_CHARGE_KRW ||
    value.provider_currency !== 'KRW' ||
    !TOSS_CHECKOUT_FINGERPRINT_PATTERN.test(value.snapshot_fingerprint)
  ) {
    return null;
  }

  return Object.freeze({ ...value });
}

export function createTossSubscriptionConfirmation(record) {
  const plan = record?.plan;
  const checkout = normalizeTossSubscriptionCheckout(record?.toss_checkout);
  if (
    !plan ||
    !checkout ||
    plan.id !== checkout.plan_id ||
    plan.title !== checkout.plan_title ||
    plan.price_amount !== checkout.price_amount ||
    typeof plan.currency !== 'string' ||
    plan.currency.trim().toUpperCase() !== checkout.price_currency
  ) {
    return null;
  }

  return Object.freeze({
    plan: Object.freeze({ ...plan }),
    checkout,
  });
}

export function getValidTossBillingTradeNo(value) {
  const tradeNo = value?.trade_no;
  return typeof tradeNo === 'string' && TOSS_ORDER_ID_PATTERN.test(tradeNo)
    ? tradeNo
    : '';
}

export function isTossBillingAuthSession(value, purpose) {
  const tradeNo = getValidTossBillingTradeNo(value);
  return (
    value &&
    typeof value === 'object' &&
    isNonBlankBoundedString(value.client_key, 2048) &&
    isTossCustomerKey(value.customer_key) &&
    tradeNo !== '' &&
    isTossCallbackPair(value.success_url, value.fail_url, purpose, tradeNo)
  );
}

export function isTossSubscriptionBillingSession(value) {
  return (
    isTossBillingAuthSession(value, 'subscription') &&
    normalizeTossSubscriptionCheckout(value.toss_checkout) !== null
  );
}

export function tossSubscriptionSessionMatchesConfirmation(
  session,
  confirmation,
) {
  const actual = normalizeTossSubscriptionCheckout(session?.toss_checkout);
  const expected = confirmation?.checkout;
  return (
    actual !== null &&
    expected &&
    actual.plan_id === expected.plan_id &&
    actual.plan_title === expected.plan_title &&
    actual.price_amount === expected.price_amount &&
    actual.price_currency === expected.price_currency &&
    actual.provider_amount === expected.provider_amount &&
    actual.provider_currency === expected.provider_currency &&
    actual.snapshot_fingerprint === expected.snapshot_fingerprint
  );
}

export function isTossUserCancellation(error) {
  return (
    error &&
    typeof error === 'object' &&
    (error.code === 'USER_CANCEL' ||
      error.code === 'PAY_PROCESS_CANCELED' ||
      error.code === 'PAYMENT_REQUEST_ABORTED')
  );
}
