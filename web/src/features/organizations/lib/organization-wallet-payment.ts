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
  isPayPalPayment,
  isStripePayment,
  isWaffoPancakePayment,
} from '@/features/wallet/lib'

export type OrganizationWalletPaymentFlow =
  | 'stripe'
  | 'paypal'
  | 'waffo_pancake'
  | 'generic'

export function getOrganizationWalletPaymentFlow(
  paymentType: string
): OrganizationWalletPaymentFlow {
  if (isStripePayment(paymentType)) return 'stripe'
  if (isPayPalPayment(paymentType)) return 'paypal'
  if (isWaffoPancakePayment(paymentType)) return 'waffo_pancake'
  return 'generic'
}
