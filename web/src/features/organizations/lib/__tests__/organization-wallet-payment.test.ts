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
import { describe, expect, test } from 'vitest'

import { PAYMENT_TYPES } from '@/features/wallet/constants'

import { getOrganizationWalletPaymentFlow } from '../organization-wallet-payment'

describe('getOrganizationWalletPaymentFlow', () => {
  test('keeps generic payment types on the generic form flow', () => {
    expect(getOrganizationWalletPaymentFlow('alipay')).toBe('generic')
  })

  test('routes Stripe through the Stripe flow', () => {
    expect(getOrganizationWalletPaymentFlow(PAYMENT_TYPES.STRIPE)).toBe(
      'stripe'
    )
  })

  test('routes PayPal through the PayPal flow', () => {
    expect(getOrganizationWalletPaymentFlow(PAYMENT_TYPES.PAYPAL)).toBe(
      'paypal'
    )
  })

  test('routes Waffo Pancake through its own flow', () => {
    expect(getOrganizationWalletPaymentFlow(PAYMENT_TYPES.WAFFO_PANCAKE)).toBe(
      'waffo_pancake'
    )
  })
})
