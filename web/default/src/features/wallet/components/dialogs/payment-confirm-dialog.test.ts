import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { getTossConfirmAmounts } from './payment-confirm-dialog'

describe('getTossConfirmAmounts', () => {
  test('uses backend Toss quote credit amount when it differs from local preview', () => {
    const amounts = getTossConfirmAmounts({
      topupAmount: 10000,
      paymentAmount: 10000,
      amountMode: 'krw',
      tossUnitPrice: 1000,
      tossQuote: {
        amount_mode: 'krw',
        input_amount: 10000,
        charge_amount: 10000,
        credit_amount: 20,
        credit_quota: 10000000,
        unit_price: 1000,
      },
    })

    assert.equal(amounts.chargeAmount, 10000)
    assert.equal(amounts.creditAmount, 20)
  })
})
