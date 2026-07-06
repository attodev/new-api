// @ts-nocheck
import { describe, expect, it } from 'bun:test'
import { PAYMENT_TYPES } from '@/features/wallet/constants'
import { getOrganizationWalletPaymentFlow } from './organization-wallet-payment'

describe('getOrganizationWalletPaymentFlow', () => {
  it('routes Toss through the Toss SDK flow', () => {
    expect(getOrganizationWalletPaymentFlow(PAYMENT_TYPES.TOSS)).toBe('toss')
  })

  it('keeps generic payment types on the generic form flow', () => {
    expect(getOrganizationWalletPaymentFlow('alipay')).toBe('generic')
  })
})
