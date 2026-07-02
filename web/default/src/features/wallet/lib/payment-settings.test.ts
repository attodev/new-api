import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildWalletPaymentSettingTabs,
  getConfiguredAutoRechargeTypes,
  getInitialWalletPaymentSetting,
} from './payment-settings'

describe('wallet payment setting helpers', () => {
  test('hides subscription when there is no active subscription', () => {
    const tabs = buildWalletPaymentSettingTabs({
      hasActiveSubscription: false,
      visibleAutoRechargeModes: [],
      policies: [],
    })

    assert.deepEqual(tabs, [])
    assert.equal(getInitialWalletPaymentSetting(tabs), null)
  })

  test('shows subscription and disables unconfigured recharge choices when subscription is active', () => {
    const tabs = buildWalletPaymentSettingTabs({
      hasActiveSubscription: true,
      visibleAutoRechargeModes: ['threshold', 'scheduled'],
      policies: [],
    })

    assert.deepEqual(
      tabs.map((tab) => [tab.kind, tab.disabled]),
      [
        ['subscription', false],
        ['threshold', true],
        ['scheduled', true],
      ]
    )
    assert.equal(getInitialWalletPaymentSetting(tabs), 'subscription')
  })

  test('keeps configured auto recharge enabled and disables scheduled recharge', () => {
    const tabs = buildWalletPaymentSettingTabs({
      hasActiveSubscription: false,
      visibleAutoRechargeModes: ['threshold', 'scheduled'],
      policies: [{ id: 1, type: 'threshold', status: 'active' } as never],
    })

    assert.deepEqual(
      tabs.map((tab) => [tab.kind, tab.disabled]),
      [
        ['threshold', false],
        ['scheduled', true],
      ]
    )
    assert.equal(getInitialWalletPaymentSetting(tabs), 'threshold')
  })

  test('keeps every already configured exception tab enabled so users can cancel', () => {
    const tabs = buildWalletPaymentSettingTabs({
      hasActiveSubscription: true,
      visibleAutoRechargeModes: ['threshold', 'scheduled'],
      policies: [
        { id: 1, type: 'threshold', status: 'pending' },
        { id: 2, type: 'scheduled', status: 'active' },
      ] as never,
    })

    assert.deepEqual(
      tabs.map((tab) => [tab.kind, tab.disabled]),
      [
        ['subscription', false],
        ['threshold', false],
        ['scheduled', false],
      ]
    )
  })

  test('ignores cancelled and failed auto recharge policies', () => {
    assert.deepEqual(
      getConfiguredAutoRechargeTypes([
        { id: 1, type: 'threshold', status: 'cancelled' },
        { id: 2, type: 'scheduled', status: 'failed' },
      ] as never),
      []
    )
  })
})
