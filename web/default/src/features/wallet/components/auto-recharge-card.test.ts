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
import React from 'react'
import i18next from 'i18next'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { before, describe, test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import {
  AutoRechargeCard,
  buildPresetCreatePayload,
  getAutoRechargeModeTitleKey,
  getInitialAutoRechargeFormState,
  getVisibleAutoRechargeModes,
  parseMoneyInput,
} from './auto-recharge-card'

const enMessages = (
  JSON.parse(
    readFileSync(
      new URL('../../../i18n/locales/en.json', import.meta.url),
      'utf8'
    )
  ) as { translation: Record<string, string> }
).translation

before(async () => {
  if (!i18next.isInitialized) {
    await i18next.use(initReactI18next).init({
      lng: 'en',
      fallbackLng: 'en',
      resources: {
        en: {
          translation: enMessages,
        },
      },
      interpolation: {
        escapeValue: false,
      },
    })
  }
})

function renderWithI18n(element: React.ReactElement) {
  return renderToStaticMarkup(
    React.createElement(I18nextProvider, { i18n: i18next }, element)
  )
}

describe('auto recharge card helpers', () => {
  test('uses English source i18n keys for mode titles', () => {
    assert.equal(getAutoRechargeModeTitleKey('scheduled'), 'Scheduled recharge')
    assert.equal(getAutoRechargeModeTitleKey('threshold'), 'Auto recharge')
  })

  test('treats blank money input as invalid while preserving explicit zero', () => {
    assert.equal(parseMoneyInput(''), null)
    assert.equal(parseMoneyInput('   '), null)
    assert.equal(parseMoneyInput('0'), 0)
    assert.equal(parseMoneyInput(' 0 '), 0)
    assert.equal(parseMoneyInput('12.9'), 12)
  })

  test('preserves explicit zero values when hydrating an active policy', () => {
    assert.deepEqual(
      getInitialAutoRechargeFormState({
        id: 7,
        type: 'threshold',
        target_type: 'user',
        target_id: 42,
        status: 'active',
        amount: 0,
        threshold_amount: 0,
        interval_value: 0,
        interval_unit: 'day',
        charge_immediately: false,
      }),
      {
        amount: '0',
        thresholdAmount: '0',
        intervalUnit: 'day',
        intervalValue: '0',
        chargeImmediately: false,
      }
    )
  })

  test('keeps existing Toss English locale values in en.json', () => {
    assert.equal(enMessages['Toss Gateway'], 'Toss Gateway')
    assert.equal(
      enMessages['Configuration for Toss Payments integration'],
      'Configuration for Toss Payments integration'
    )
  })

  test('renders organization-specific permission copy when provided', () => {
    const html = renderWithI18n(
      React.createElement(AutoRechargeCard, {
        mode: 'threshold',
        policies: [],
        presets: [],
        loading: false,
        processing: false,
        canManage: false,
        permissionMessageKey:
          'Only the organization owner can change auto payments',
        onCreateScheduled: async () => false,
        onCreateThreshold: async () => false,
        onCancel: async () => false,
      })
    )

    assert.match(html, /Only the organization owner can change auto payments/)
    assert.doesNotMatch(html, /You do not have permission to manage this\./)
  })

  test('renders active threshold policy with quota threshold', () => {
    const html = renderWithI18n(
      React.createElement(AutoRechargeCard, {
        mode: 'threshold',
        policies: [
          {
            id: 7,
            type: 'threshold',
            target_type: 'user',
            target_id: 42,
            status: 'active',
            amount: 10000,
            threshold_amount: 1000,
            threshold_quota: 500000,
          },
        ],
        presets: [],
        loading: false,
        processing: false,
        canManage: true,
        onCreateScheduled: async () => false,
        onCreateThreshold: async () => false,
        onCancel: async () => false,
      })
    )

    assert.match(html, /Threshold quota/)
    assert.ok(html.includes(formatQuota(500000)))
    assert.doesNotMatch(html, /Threshold balance/)
  })

  test('renders active scheduled test-period policy with monthly copy', () => {
    const html = renderWithI18n(
      React.createElement(AutoRechargeCard, {
        mode: 'scheduled',
        policies: [
          {
            id: 8,
            type: 'scheduled',
            target_type: 'user',
            target_id: 42,
            status: 'active',
            amount: 5000,
            interval_unit: 'custom',
            interval_value: 1,
            custom_seconds: 60,
          },
        ],
        presets: [],
        loading: false,
        processing: false,
        canManage: true,
        onCreateScheduled: async () => false,
        onCreateThreshold: async () => false,
        onCancel: async () => false,
      })
    )

    assert.match(html, /Charges on the 1st of every month/)
    assert.doesNotMatch(html, /Charge interval/)
    assert.doesNotMatch(html, /custom/)
  })
})

describe('auto recharge preset UI helpers', () => {
  test('keeps both modes visible until preset fetch resolves', () => {
    assert.deepEqual(getVisibleAutoRechargeModes([], [], false), [
      'scheduled',
      'threshold',
    ])
  })

  test('keeps both modes visible when presets resolve empty before policies resolve', () => {
    assert.deepEqual(getVisibleAutoRechargeModes([], [], true, false), [
      'scheduled',
      'threshold',
    ])
  })

  test('hides mode without preset and without existing policy', () => {
    assert.deepEqual(getVisibleAutoRechargeModes([], [], true, true), [])
  })

  test('shows scheduled mode when monthly preset exists', () => {
    assert.deepEqual(
      getVisibleAutoRechargeModes(
        [
          {
            type: 'scheduled',
            interval_unit: 'month',
            interval_value: 1,
          },
        ],
        [],
        true,
        true
      ),
      ['scheduled']
    )
  })

  test('shows scheduled mode when test-period preset exists', () => {
    assert.deepEqual(
      getVisibleAutoRechargeModes(
        [
          {
            type: 'scheduled',
            interval_unit: 'custom',
            interval_value: 1,
          },
        ],
        [],
        true,
        true
      ),
      ['scheduled']
    )
  })

  test('hides scheduled mode when only legacy non-monthly preset exists', () => {
    assert.deepEqual(
      getVisibleAutoRechargeModes(
        [
          {
            type: 'scheduled',
            interval_unit: 'day',
            interval_value: 1,
          },
        ],
        [],
        true,
        true
      ),
      []
    )
  })

  test('shows mode when existing policy exists even without preset', () => {
    assert.deepEqual(
      getVisibleAutoRechargeModes(
        [],
        [
          {
            id: 7,
            type: 'threshold',
            status: 'active',
            amount: 10000,
          } as never,
        ],
        true,
        true
      ),
      ['threshold']
    )
  })

  test('builds create payload with preset id only', () => {
    assert.deepEqual(buildPresetCreatePayload(12), { preset_id: 12 })
  })

  test('renders scheduled presets as monthly amount choices only', () => {
    const html = renderWithI18n(
      React.createElement(AutoRechargeCard, {
        mode: 'scheduled',
        policies: [],
        presets: [
          {
            id: 1,
            type: 'scheduled',
            target_scope: 'all',
            name: 'Daily 10000',
            amount: 10000,
            interval_unit: 'day',
            interval_value: 1,
            enabled: true,
          },
          {
            id: 2,
            type: 'scheduled',
            target_scope: 'all',
            name: 'Monthly 30000',
            amount: 30000,
            interval_unit: 'month',
            interval_value: 1,
            enabled: true,
          },
        ],
        loading: false,
        processing: false,
        canManage: true,
        onCreateScheduled: async () => false,
        onCreateThreshold: async () => false,
        onCancel: async () => false,
      })
    )

    assert.doesNotMatch(html, /Choose recharge period/)
    assert.doesNotMatch(html, /Daily/)
    assert.match(
      html,
      /Monthly recharge charges the selected amount on the 1st of every month\./
    )
    assert.doesNotMatch(html, /10000원/)
    assert.match(html, /30000원/)
  })

  test('renders scheduled test-period presets with monthly copy', () => {
    const html = renderWithI18n(
      React.createElement(AutoRechargeCard, {
        mode: 'scheduled',
        policies: [],
        presets: [
          {
            id: 3,
            type: 'scheduled',
            target_scope: 'all',
            name: 'Test 5000',
            amount: 5000,
            interval_unit: 'custom',
            interval_value: 1,
            custom_seconds: 60,
            enabled: true,
          },
        ],
        loading: false,
        processing: false,
        canManage: true,
        onCreateScheduled: async () => false,
        onCreateThreshold: async () => false,
        onCancel: async () => false,
      })
    )

    assert.doesNotMatch(html, /Choose recharge period/)
    assert.doesNotMatch(html, /Test period/)
    assert.match(
      html,
      /Monthly recharge charges the selected amount on the 1st of every month\./
    )
    assert.match(html, /5000원/)
    assert.match(html, /Register card and set auto recharge/)
  })

  test('hides scheduled period selector when only one period exists', () => {
    const html = renderWithI18n(
      React.createElement(AutoRechargeCard, {
        mode: 'scheduled',
        policies: [],
        presets: [
          {
            id: 1,
            type: 'scheduled',
            target_scope: 'all',
            name: 'Monthly 10000',
            amount: 10000,
            interval_unit: 'month',
            interval_value: 1,
            enabled: true,
          },
          {
            id: 2,
            type: 'scheduled',
            target_scope: 'all',
            name: 'Monthly 30000',
            amount: 30000,
            interval_unit: 'month',
            interval_value: 1,
            enabled: true,
          },
        ],
        loading: false,
        processing: false,
        canManage: true,
        onCreateScheduled: async () => false,
        onCreateThreshold: async () => false,
        onCancel: async () => false,
      })
    )

    assert.doesNotMatch(html, /Choose recharge period/)
    assert.match(
      html,
      /Monthly recharge charges the selected amount on the 1st of every month\./
    )
    assert.match(html, /10000/)
    assert.match(html, /30000/)
    assert.match(html, /Register card and set auto recharge/)
  })

  test('renders threshold quota heading once before recharge amount choices', () => {
    const html = renderWithI18n(
      React.createElement(AutoRechargeCard, {
        mode: 'threshold',
        policies: [],
        presets: [
          {
            id: 3,
            type: 'threshold',
            target_scope: 'all',
            name: 'Auto 10000',
            amount: 10000,
            threshold_amount: 0,
            threshold_quota: 500000,
            enabled: true,
          },
          {
            id: 4,
            type: 'threshold',
            target_scope: 'all',
            name: 'Auto 30000',
            amount: 30000,
            threshold_amount: 0,
            threshold_quota: 500000,
            enabled: true,
          },
        ],
        loading: false,
        processing: false,
        canManage: true,
        onCreateScheduled: async () => false,
        onCreateThreshold: async () => false,
        onCancel: async () => false,
      })
    )

    assert.equal(html.match(/When remaining quota is below/g)?.length ?? 0, 1)
    assert.match(html, /charge the following amount/)
    assert.ok(html.includes(formatQuota(500000)))
    assert.match(html, /Choose recharge amount/)
    assert.match(html, /10000원/)
    assert.match(html, /30000원/)
  })

  test('requires confirming the selected preset before creating auto recharge', () => {
    const html = renderWithI18n(
      React.createElement(AutoRechargeCard, {
        mode: 'threshold',
        policies: [],
        presets: [
          {
            id: 3,
            type: 'threshold',
            target_scope: 'all',
            name: 'Auto 10000',
            amount: 10000,
            threshold_amount: 0,
            threshold_quota: 500000,
            enabled: true,
          },
        ],
        loading: false,
        processing: false,
        canManage: true,
        onCreateScheduled: async () => false,
        onCreateThreshold: async () => false,
        onCancel: async () => false,
      })
    )

    assert.match(html, /Register card and set auto recharge/)
    assert.match(html, /disabled/)
  })

  test('disables scheduled preset creation when another payment setting is active', () => {
    const html = renderWithI18n(
      React.createElement(AutoRechargeCard, {
        mode: 'scheduled',
        policies: [],
        presets: [
          {
            id: 1,
            type: 'scheduled',
            target_scope: 'all',
            name: 'Monthly 10000',
            amount: 10000,
            interval_unit: 'month',
            interval_value: 1,
            enabled: true,
          },
        ],
        loading: false,
        processing: false,
        canManage: true,
        creationDisabled: true,
        creationDisabledMessageKey:
          'Cancel the current payment setting before choosing another one.',
        onCreateScheduled: async () => false,
        onCreateThreshold: async () => false,
        onCancel: async () => false,
      })
    )

    assert.match(
      html,
      /Cancel the current payment setting before choosing another one\./
    )
    assert.match(html, /disabled/)
  })

  test('does not block cancelling an existing policy when creation is locked', () => {
    const html = renderWithI18n(
      React.createElement(AutoRechargeCard, {
        mode: 'threshold',
        policies: [
          {
            id: 7,
            type: 'threshold',
            target_type: 'user',
            target_id: 1,
            status: 'active',
            amount: 10000,
            threshold_amount: 3000,
          },
        ],
        presets: [],
        loading: false,
        processing: false,
        canManage: true,
        creationDisabled: true,
        creationDisabledMessageKey:
          'Cancel the current payment setting before choosing another one.',
        onCreateScheduled: async () => false,
        onCreateThreshold: async () => false,
        onCancel: async () => false,
      })
    )

    assert.match(html, /Current policy/)
    assert.match(html, /Cancel/)
    assert.doesNotMatch(
      html,
      /Cancel the current payment setting before choosing another one\./
    )
  })
})
