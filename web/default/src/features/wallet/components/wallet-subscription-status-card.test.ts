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
import { WalletSubscriptionStatusCard } from './wallet-subscription-status-card'

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
      resources: { en: { translation: enMessages } },
      interpolation: { escapeValue: false },
    })
  }
})

function renderWithI18n(element: React.ReactElement) {
  return renderToStaticMarkup(
    React.createElement(I18nextProvider, { i18n: i18next }, element)
  )
}

describe('WalletSubscriptionStatusCard', () => {
  test('renders active subscription status without purchase controls', () => {
    const html = renderWithI18n(
      React.createElement(WalletSubscriptionStatusCard, {
        activeSubscriptions: [
          {
            subscription: {
              id: 11,
              plan_id: 3,
              status: 'active',
              amount_total: 1000,
              amount_used: 250,
              end_time: Math.floor(Date.now() / 1000) + 86400,
              auto_renew: true,
            },
            plan: { id: 3, title: 'Pro Plan' },
          },
        ] as never,
        allSubscriptions: [],
        billingPreference: 'subscription_first',
        refreshing: false,
        cancellingAutoRenew: false,
        onRefresh: async () => undefined,
        onBillingPreferenceChange: async () => undefined,
        onCancelTossAutoRenew: async () => undefined,
      })
    )

    assert.match(html, /My Subscriptions/)
    assert.match(html, /Pro Plan/)
    assert.match(html, /Auto-renew active/)
    assert.doesNotMatch(html, /Subscribe Now/)
    assert.doesNotMatch(html, /No plans available/)
  })

  test('renders nothing without active subscriptions', () => {
    const html = renderWithI18n(
      React.createElement(WalletSubscriptionStatusCard, {
        activeSubscriptions: [],
        allSubscriptions: [],
        billingPreference: 'wallet_first',
        refreshing: false,
        cancellingAutoRenew: false,
        onRefresh: async () => undefined,
        onBillingPreferenceChange: async () => undefined,
        onCancelTossAutoRenew: async () => undefined,
      })
    )

    assert.equal(html, '')
  })
})
