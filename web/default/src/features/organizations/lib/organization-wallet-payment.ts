import {
  isPayPalPayment,
  isStripePayment,
  isTossPayment,
  isWaffoPancakePayment,
} from '@/features/wallet/lib'

export type OrganizationWalletPaymentFlow =
  | 'stripe'
  | 'paypal'
  | 'waffo_pancake'
  | 'toss'
  | 'generic'

export function getOrganizationWalletPaymentFlow(
  paymentType: string
): OrganizationWalletPaymentFlow {
  if (isStripePayment(paymentType)) return 'stripe'
  if (isPayPalPayment(paymentType)) return 'paypal'
  if (isWaffoPancakePayment(paymentType)) return 'waffo_pancake'
  if (isTossPayment(paymentType)) return 'toss'
  return 'generic'
}
