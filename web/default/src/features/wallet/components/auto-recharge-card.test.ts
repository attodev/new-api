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
})

describe('auto recharge preset UI helpers', () => {
  test('hides mode without preset and without existing policy', () => {
    assert.deepEqual(getVisibleAutoRechargeModes([], []), [])
  })

  test('shows mode when preset exists', () => {
    assert.deepEqual(
      getVisibleAutoRechargeModes(
        [
          {
            id: 1,
            type: 'scheduled',
            target_scope: 'all',
            name: '월 1회',
            amount: 10000,
            enabled: true,
          },
        ],
        []
      ),
      ['scheduled']
    )
  })

  test('shows mode when existing policy exists even without preset', () => {
    assert.deepEqual(
      getVisibleAutoRechargeModes(
        [],
        [{ id: 7, type: 'threshold', status: 'active', amount: 10000 } as never]
      ),
      ['threshold']
    )
  })

  test('builds create payload with preset id only', () => {
    assert.deepEqual(buildPresetCreatePayload(12), { preset_id: 12 })
  })
})
