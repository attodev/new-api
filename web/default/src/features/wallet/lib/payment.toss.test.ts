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
import {
  isTossPayment,
  getDefaultPaymentType,
  getMinTopupAmount,
  shouldOpenPaymentConfirmDialog,
} from './payment'
import { PAYMENT_TYPES, DEFAULT_MIN_TOPUP } from '../constants'
import type { TopupInfo } from '../types'

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
    const info = { enable_toss_topup: true, pay_methods: [] } as unknown as TopupInfo
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
    assert.equal(
      shouldOpenPaymentConfirmDialog(PAYMENT_TYPES.TOSS, 0),
      false
    )
  })

  test('keeps non-Toss confirmation behavior unchanged for zero amounts', () => {
    assert.equal(
      shouldOpenPaymentConfirmDialog(PAYMENT_TYPES.STRIPE, 0),
      true
    )
  })
})
