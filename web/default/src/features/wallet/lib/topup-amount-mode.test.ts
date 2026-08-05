import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  TOSS_KRW_PRESETS,
  TOSS_QUOTA_PRESETS,
  createTossPaymentConfirmation,
  formatWonAmount,
  getTossPreview,
  parseTossQuoteData,
  tossPaymentSessionMatchesConfirmation,
} from './topup-amount-mode'

describe('topup amount mode helpers', () => {
  test('uses requested KRW and quota preset values', () => {
    assert.deepEqual(
      TOSS_KRW_PRESETS,
      [10000, 50000, 100000, 200000, 500000, 1000000]
    )
    assert.deepEqual(TOSS_QUOTA_PRESETS, [10, 20, 50, 100, 200, 500])
  })

  test('previews KRW mode as payment fixed and credit estimated', () => {
    const preview = getTossPreview(13000, 'krw', 1300)
    assert.equal(preview.chargeAmount, 13000)
    assert.equal(preview.creditAmount, 10)
  })

  test('previews quota mode as credit fixed and payment estimated', () => {
    const preview = getTossPreview(10, 'quota', 1300)
    assert.equal(preview.chargeAmount, 13000)
    assert.equal(preview.creditAmount, 10)
  })

  test('formats won amounts with unit suffix', () => {
    assert.equal(formatWonAmount(10000), '10,000원')
  })

  test('parses structured and legacy Toss amount responses', () => {
    assert.deepEqual(parseTossQuoteData('13000'), {
      charge_amount: 13000,
    })
    assert.deepEqual(
      parseTossQuoteData({
        charge_amount: 13000,
        credit_amount: 10,
        credit_quota: 5000000,
        unit_price: 1300,
        amount_mode: 'quota',
      }),
      {
        charge_amount: 13000,
        credit_amount: 10,
        credit_quota: 5000000,
        unit_price: 1300,
        amount_mode: 'quota',
      }
    )
  })

  test('captures only a complete quote for the exact input and mode', () => {
    const confirmation = createTossPaymentConfirmation(10, 'quota', {
      amount_mode: 'quota',
      input_amount: 10,
      charge_amount: 13000,
      credit_amount: 10,
      credit_quota: 5000000,
      unit_price: 1300,
    })

    assert.deepEqual(confirmation, {
      amount_mode: 'quota',
      input_amount: 10,
      charge_amount: 13000,
      credit_amount: 10,
      credit_quota: 5000000,
      unit_price: 1300,
    })
    assert.equal(Object.isFrozen(confirmation), true)
    assert.equal(
      createTossPaymentConfirmation(11, 'quota', {
        amount_mode: 'quota',
        input_amount: 10,
        charge_amount: 13000,
      }),
      null
    )
  })

  test('requires the checkout session to match every confirmed price field', () => {
    const confirmation = createTossPaymentConfirmation(10000, 'krw', {
      amount_mode: 'krw',
      input_amount: 10000,
      charge_amount: 10000,
      credit_amount: 10,
      credit_quota: 5000000,
      unit_price: 1000,
    })
    assert.ok(confirmation)

    const session = {
      client_key: 'test_ck_example',
      customer_key: 'customer_example',
      order_id: 'order_123456',
      order_name: 'credit top-up',
      amount: 10000,
      charge_amount: 10000,
      credit_amount: 10,
      credit_quota: 5000000,
      unit_price: 1000,
      amount_mode: 'krw' as const,
      success_url: 'https://example.com/api/toss/confirm',
      fail_url: 'https://example.com/api/toss/fail',
    }

    assert.equal(
      tossPaymentSessionMatchesConfirmation(session, confirmation),
      true
    )
    assert.equal(
      tossPaymentSessionMatchesConfirmation(
        { ...session, amount: 11000, charge_amount: 11000 },
        confirmation
      ),
      false
    )
    assert.equal(
      tossPaymentSessionMatchesConfirmation(
        { ...session, credit_quota: 4999999 },
        confirmation
      ),
      false
    )
    assert.equal(
      tossPaymentSessionMatchesConfirmation(
        { ...session, unit_price: 1100 },
        confirmation
      ),
      false
    )
  })
})
