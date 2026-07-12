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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { PlanRecord } from '../types'
import {
  createTossSubscriptionPaymentConfirmation,
  tossSubscriptionSessionMatchesConfirmation,
} from './toss-checkout'

const record: PlanRecord = {
  plan: {
    id: 7,
    title: 'Pro',
    price_amount: 10,
    currency: 'USD',
    duration_unit: 'month',
    duration_value: 1,
    quota_reset_period: 'monthly',
    enabled: true,
    sort_order: 0,
    max_purchase_per_user: 1,
    total_amount: 5000000,
  },
  toss_checkout: {
    plan_id: 7,
    plan_title: 'Pro',
    price_amount: 10,
    price_currency: 'USD',
    provider_amount: 13000,
    provider_currency: 'KRW',
    snapshot_fingerprint: 'a'.repeat(40),
  },
}

describe('Toss subscription checkout confirmation', () => {
  test('freezes the exact public plan and provider quote', () => {
    const confirmation = createTossSubscriptionPaymentConfirmation(record)
    assert.ok(confirmation)
    assert.equal(Object.isFrozen(confirmation), true)
    assert.equal(Object.isFrozen(confirmation.plan), true)
    assert.equal(Object.isFrozen(confirmation.checkout), true)
    assert.equal(confirmation.checkout.provider_amount, 13000)
  })

  test('rejects metadata that does not describe the rendered plan', () => {
    assert.equal(
      createTossSubscriptionPaymentConfirmation({
        ...record,
        toss_checkout: { ...record.toss_checkout!, price_amount: 11 },
      }),
      null
    )
  })

  test('rejects provider amounts outside the Toss card contract', () => {
    assert.equal(
      createTossSubscriptionPaymentConfirmation({
        ...record,
        toss_checkout: { ...record.toss_checkout!, provider_amount: 99 },
      }),
      null
    )
    assert.equal(
      createTossSubscriptionPaymentConfirmation({
        ...record,
        toss_checkout: {
          ...record.toss_checkout!,
          provider_amount: 2_147_483_648,
        },
      }),
      null
    )
  })

  test('matches every order-bound monetary field and snapshot fingerprint', () => {
    const confirmation = createTossSubscriptionPaymentConfirmation(record)
    assert.ok(confirmation)
    const session = {
      client_key: 'test_ck_billing_example',
      customer_key: 'customer_example',
      trade_no: 'toss_sub_order_123456',
      success_url: 'https://example.com/api/subscription/toss/confirm',
      fail_url: 'https://example.com/api/subscription/toss/fail',
      toss_checkout: { ...record.toss_checkout! },
    }

    assert.equal(
      tossSubscriptionSessionMatchesConfirmation(session, confirmation),
      true
    )
    assert.equal(
      tossSubscriptionSessionMatchesConfirmation(
        {
          ...session,
          toss_checkout: { ...session.toss_checkout, provider_amount: 14000 },
        },
        confirmation
      ),
      false
    )
    assert.equal(
      tossSubscriptionSessionMatchesConfirmation(
        {
          ...session,
          toss_checkout: {
            ...session.toss_checkout,
            snapshot_fingerprint: 'b'.repeat(40),
          },
        },
        confirmation
      ),
      false
    )
  })
})
