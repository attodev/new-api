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
import assert from 'node:assert/strict'
import { before, describe, test } from 'node:test'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import i18next from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { WalletStatsCard } from './wallet-stats-card'

before(async () => {
  if (!i18next.isInitialized) {
    await i18next.use(initReactI18next).init({
      lng: 'en',
      fallbackLng: 'en',
      resources: {
        en: {
          translation: {},
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
    <I18nextProvider i18n={i18next}>{element}</I18nextProvider>
  )
}

describe('WalletStatsCard', () => {
  test('renders organization plan balance message while keeping usage and request stats', () => {
    const html = renderWithI18n(
      <WalletStatsCard
        user={{
          id: 2,
          username: 'member',
          quota: 100,
          used_quota: 20,
          request_count: 7,
          aff_quota: 0,
          aff_history_quota: 0,
          aff_count: 0,
          group: 'default',
        }}
        balanceDisplay={{
          value: 'Organization plan in use',
          description: 'Balance is managed by the assigned organization plan',
        }}
      />
    )

    assert.match(html, /Organization plan in use/)
    assert.match(html, /Balance is managed by the assigned organization plan/)
    assert.match(html, /Total Usage/)
    assert.match(html, /API Requests/)
    assert.match(html, />7</)
  })
})
