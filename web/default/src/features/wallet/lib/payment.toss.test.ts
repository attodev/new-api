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
import { PAYMENT_TYPES, DEFAULT_MIN_TOPUP } from '../constants'
import type { TopupInfo } from '../types'
import {
  getTossPaymentWindowTargetOptions,
  isTossPayment,
  getDefaultPaymentType,
  getMinTopupAmount,
  isTossUserCancellation,
  isValidTossBillingAuthSession,
  isValidTossPaymentSession,
  shouldBlockPaymentMethodBeforeQuote,
  shouldOpenPaymentConfirmDialog,
} from './payment'

describe('Toss callback window target', () => {
  test('keeps the SDK default for same-origin callbacks', () => {
    assert.deepEqual(
      getTossPaymentWindowTargetOptions(
        'https://app.example/api/toss/confirm',
        'https://app.example/api/toss/fail',
        'https://app.example'
      ),
      {}
    )
  })

  test('uses self for cross-origin callbacks to avoid iframe CORS', () => {
    assert.deepEqual(
      getTossPaymentWindowTargetOptions(
        'https://api.example/api/toss/confirm',
        'https://api.example/api/toss/fail',
        'https://app.example'
      ),
      { windowTarget: 'self' }
    )
  })
})

describe('isTossPayment', () => {
  test('true for toss', () => {
    assert.equal(isTossPayment(PAYMENT_TYPES.TOSS), true)
  })
  test('false for others', () => {
    assert.equal(isTossPayment(PAYMENT_TYPES.STRIPE), false)
  })
})

describe('getDefaultPaymentType (toss)', () => {
  test('returns toss when only toss is enabled', () => {
    const info = {
      enable_toss_topup: true,
      pay_methods: [],
    } as unknown as TopupInfo
    assert.equal(getDefaultPaymentType(info), PAYMENT_TYPES.TOSS)
  })
})

describe('getMinTopupAmount (toss)', () => {
  test('returns toss_min_topup when set', () => {
    const info = {
      enable_toss_topup: true,
      toss_min_topup: 5000,
    } as unknown as TopupInfo
    assert.equal(getMinTopupAmount(info), 5000)
  })
  test('falls back to DEFAULT_MIN_TOPUP when toss_min_topup is 0', () => {
    const info = {
      enable_toss_topup: true,
      toss_min_topup: 0,
    } as unknown as TopupInfo
    assert.equal(getMinTopupAmount(info), DEFAULT_MIN_TOPUP)
  })
})

describe('shouldOpenPaymentConfirmDialog', () => {
  test('blocks Toss confirmation when quote calculation returns no payable amount', () => {
    assert.equal(shouldOpenPaymentConfirmDialog(PAYMENT_TYPES.TOSS, 0), false)
  })

  test('keeps non-Toss confirmation behavior unchanged for zero amounts', () => {
    assert.equal(shouldOpenPaymentConfirmDialog(PAYMENT_TYPES.STRIPE, 0), true)
  })
})

describe('client minimum gate', () => {
  test('always lets Toss request the authoritative server quote', () => {
    assert.equal(
      shouldBlockPaymentMethodBeforeQuote(PAYMENT_TYPES.TOSS, 100, 200),
      false
    )
  })

  test('keeps the existing client minimum gate for other providers', () => {
    assert.equal(
      shouldBlockPaymentMethodBeforeQuote(PAYMENT_TYPES.STRIPE, 100, 200),
      true
    )
  })
})

const validPaymentSession = {
  client_key: 'test_ck_example',
  customer_key: 'cust_random_123',
  order_id: 'toss_order_123',
  order_name: 'Credit top-up',
  amount: 200,
  success_url: 'https://pay.example.com/api/toss/confirm',
  fail_url: 'https://pay.example.com/api/toss/fail',
}

describe('Toss SDK session validation', () => {
  test('accepts an authoritative card checkout session', () => {
    assert.equal(isValidTossPaymentSession(validPaymentSession), true)
  })

  test('rejects unsafe amounts, malformed order IDs, and public HTTP callbacks', () => {
    assert.equal(
      isValidTossPaymentSession({ ...validPaymentSession, amount: 199 }),
      false
    )
    assert.equal(
      isValidTossPaymentSession({
        ...validPaymentSession,
        order_id: 'contains space',
      }),
      false
    )
    assert.equal(
      isValidTossPaymentSession({
        ...validPaymentSession,
        success_url: 'http://pay.example.com/api/toss/confirm',
      }),
      false
    )

    assert.equal(
      isValidTossPaymentSession({
        ...validPaymentSession,
        customer_key: `cust_${'x'.repeat(46)}`,
      }),
      false
    )
  })

  test('allows localhost HTTP callbacks for local development', () => {
    assert.equal(
      isValidTossPaymentSession({
        ...validPaymentSession,
        success_url: 'http://localhost:3000/api/toss/confirm',
        fail_url: 'http://localhost:3000/api/toss/fail',
      }),
      true
    )
  })

  test('requires the expected callback endpoints on one origin', () => {
    assert.equal(
      isValidTossPaymentSession({
        ...validPaymentSession,
        fail_url: 'https://other.example.com/api/toss/fail',
      }),
      false
    )
    assert.equal(
      isValidTossPaymentSession({
        ...validPaymentSession,
        success_url: 'https://pay.example.com/api/toss/fail',
      }),
      false
    )
  })

  test('requires a complete billing authorization correlation record', () => {
    const tradeNo = 'toss_sub_123456'
    const billingSession = {
      client_key: 'test_ck_billing',
      customer_key: 'cust_random_123',
      trade_no: tradeNo,
      success_url: `https://pay.example.com/api/subscription/toss/confirm/${tradeNo}`,
      fail_url: `https://pay.example.com/api/subscription/toss/fail/${tradeNo}`,
    }
    assert.equal(
      isValidTossBillingAuthSession(billingSession, 'subscription'),
      true
    )
    assert.equal(
      isValidTossBillingAuthSession(
        { ...billingSession, trade_no: '' },
        'subscription'
      ),
      false
    )
    assert.equal(
      isValidTossBillingAuthSession(
        {
          ...billingSession,
          success_url:
            'https://pay.example.com/api/subscription/toss/confirm/another_trade',
        },
        'subscription'
      ),
      false
    )
    assert.equal(isValidTossBillingAuthSession(billingSession, 'wallet'), false)
  })
})

describe('Toss SDK cancellation errors', () => {
  test('recognizes SDK, fail-redirect, and lifecycle cancellation codes', () => {
    assert.equal(isTossUserCancellation({ code: 'USER_CANCEL' }), true)
    assert.equal(isTossUserCancellation({ code: 'PAY_PROCESS_CANCELED' }), true)
    assert.equal(
      isTossUserCancellation({ code: 'PAYMENT_REQUEST_ABORTED' }),
      true
    )
  })

  test('does not hide code-less or non-cancellation failures', () => {
    assert.equal(isTossUserCancellation(new Error('network error')), false)
    assert.equal(isTossUserCancellation({ code: 'UNKNOWN' }), false)
  })
})
