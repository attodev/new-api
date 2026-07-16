import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildOrganizationWalletPaymentSettingTabs,
  getOrganizationAmountModeRequest,
  shouldBlockOrganizationPaymentMethodBeforeQuote,
} from './organization-wallet'

describe('organization wallet topup amount mode request', () => {
  test('includes amount mode only for Toss requests', () => {
    assert.deepEqual(getOrganizationAmountModeRequest(10, 'toss', 'quota'), {
      amount: 10,
      amount_mode: 'quota',
    })
    assert.deepEqual(getOrganizationAmountModeRequest(10, 'stripe', 'quota'), {
      amount: 10,
    })
  })
})

describe('organization wallet pre-quote minimum gate', () => {
  test('always lets Toss request the authoritative organization quote', () => {
    assert.equal(
      shouldBlockOrganizationPaymentMethodBeforeQuote(
        { type: 'toss', min_topup: 5000 },
        100,
        5000
      ),
      false
    )
  })

  test('keeps the client minimum gate for non-Toss methods', () => {
    assert.equal(
      shouldBlockOrganizationPaymentMethodBeforeQuote(
        { type: 'stripe', min_topup: 5000 },
        100,
        5000
      ),
      true
    )
  })
})

describe('organization wallet payment setting tabs', () => {
  test('matches personal wallet payment settings without showing subscription', () => {
    const tabs = buildOrganizationWalletPaymentSettingTabs({
      visibleAutoRechargeModes: ['threshold', 'scheduled'],
      policies: [{ type: 'threshold', status: 'active' }],
    })

    assert.deepEqual(
      tabs.map((tab) => [tab.kind, tab.disabled]),
      [
        ['threshold', false],
        ['scheduled', true],
      ]
    )
  })
})
