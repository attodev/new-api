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
import type {
  PlanRecord,
  SubscriptionPlan,
  TossSubscriptionBillingAuthSession,
  TossSubscriptionCheckoutSnapshot,
} from '../types'

const SNAPSHOT_FINGERPRINT_PATTERN = /^[a-f0-9]{40}$/
const TOSS_CARD_MINIMUM_KRW = 100
const TOSS_MAXIMUM_CHARGE_KRW = 2_147_483_647

export interface TossSubscriptionPaymentConfirmation {
  plan: Readonly<SubscriptionPlan>
  checkout: Readonly<TossSubscriptionCheckoutSnapshot>
}

function normalizeCheckoutSnapshot(
  value: TossSubscriptionCheckoutSnapshot | null | undefined
): Readonly<TossSubscriptionCheckoutSnapshot> | null {
  if (
    !value ||
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
    typeof value.snapshot_fingerprint !== 'string' ||
    !SNAPSHOT_FINGERPRINT_PATTERN.test(value.snapshot_fingerprint)
  ) {
    return null
  }

  return Object.freeze({ ...value })
}

/** Captures exactly the plan and KRW terms rendered when the dialog opens. */
export function createTossSubscriptionPaymentConfirmation(
  record: PlanRecord | null | undefined
): Readonly<TossSubscriptionPaymentConfirmation> | null {
  const plan = record?.plan
  const checkout = normalizeCheckoutSnapshot(record?.toss_checkout)
  if (
    !plan ||
    !checkout ||
    plan.id !== checkout.plan_id ||
    plan.title !== checkout.plan_title ||
    plan.price_amount !== checkout.price_amount ||
    plan.currency.trim().toUpperCase() !== checkout.price_currency
  ) {
    return null
  }

  return Object.freeze({
    plan: Object.freeze({ ...plan }),
    checkout,
  })
}

/**
 * Compares the order-bound server response with the immutable terms the buyer
 * saw. The fingerprint also covers duration, quota, reset, and upgrade terms.
 */
export function tossSubscriptionSessionMatchesConfirmation(
  session: TossSubscriptionBillingAuthSession,
  confirmation: Readonly<TossSubscriptionPaymentConfirmation>
): boolean {
  const checkout = normalizeCheckoutSnapshot(session.toss_checkout)
  const expected = confirmation.checkout
  return (
    checkout !== null &&
    checkout.plan_id === expected.plan_id &&
    checkout.plan_title === expected.plan_title &&
    checkout.price_amount === expected.price_amount &&
    checkout.price_currency === expected.price_currency &&
    checkout.provider_amount === expected.provider_amount &&
    checkout.provider_currency === expected.provider_currency &&
    checkout.snapshot_fingerprint === expected.snapshot_fingerprint
  )
}
