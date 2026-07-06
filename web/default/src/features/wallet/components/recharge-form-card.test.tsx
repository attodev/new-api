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
import { RechargeFormCard } from './recharge-form-card'
import type { TopupInfo } from '../types'

before(async () => {
  if (!i18next.isInitialized) {
    await i18next.use(initReactI18next).init({
      lng: 'en',
      fallbackLng: 'en',
      resources: { en: { translation: {} } },
      interpolation: { escapeValue: false },
    })
  }
})

function renderWithI18n(element: React.ReactElement) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18next}>{element}</I18nextProvider>
  )
}

const topupInfo: TopupInfo = {
  enable_online_topup: false,
  enable_stripe_topup: false,
  enable_paypal_topup: false,
  enable_toss_topup: true,
  pay_methods: [{ name: 'Toss', type: 'toss', min_topup: 1000 }],
  min_topup: 1,
  stripe_min_topup: 1,
  amount_options: [10, 20, 50, 100, 200, 500],
  discount: {},
  toss_min_topup: 1000,
  toss_unit_price: 1300,
}

describe('RechargeFormCard amount modes', () => {
  test('renders KRW presets and credit preview in KRW mode', () => {
    const html = renderWithI18n(
      <RechargeFormCard
        topupInfo={topupInfo}
        presetAmounts={[]}
        selectedPreset={10000}
        onSelectPreset={() => undefined}
        topupAmount={10000}
        onTopupAmountChange={() => undefined}
        paymentAmount={10000}
        calculating={false}
        onPaymentMethodSelect={() => undefined}
        paymentLoading={null}
        redemptionCode=''
        onRedemptionCodeChange={() => undefined}
        onRedeem={() => undefined}
        redeeming={false}
        amountMode='krw'
        onAmountModeChange={() => undefined}
        tossUnitPrice={1300}
      />
    )

    assert.match(html, /KRW based/)
    assert.match(html, /Quota based/)
    assert.match(html, /10,000원/)
    assert.match(html, /50,000원/)
    assert.match(html, /Credit/)
  })

  test('renders quota presets and KRW payment preview in quota mode', () => {
    const html = renderWithI18n(
      <RechargeFormCard
        topupInfo={topupInfo}
        presetAmounts={[]}
        selectedPreset={10}
        onSelectPreset={() => undefined}
        topupAmount={10}
        onTopupAmountChange={() => undefined}
        paymentAmount={13000}
        calculating={false}
        onPaymentMethodSelect={() => undefined}
        paymentLoading={null}
        redemptionCode=''
        onRedemptionCodeChange={() => undefined}
        onRedeem={() => undefined}
        redeeming={false}
        amountMode='quota'
        onAmountModeChange={() => undefined}
        tossUnitPrice={1300}
      />
    )

    assert.match(html, /Quota based/)
    assert.match(html, />10</)
    assert.match(html, />500</)
    assert.match(html, /Pay/)
    assert.match(html, /13,000원/)
  })

  test('keeps standard presets when Toss is mixed with other providers', () => {
    const mixedTopupInfo: TopupInfo = {
      ...topupInfo,
      enable_stripe_topup: true,
      pay_methods: [
        { name: 'Toss', type: 'toss', min_topup: 1000 },
        { name: 'Stripe', type: 'stripe', min_topup: 10 },
      ],
    }

    const html = renderWithI18n(
      <RechargeFormCard
        topupInfo={mixedTopupInfo}
        presetAmounts={[{ value: 10 }, { value: 20 }]}
        selectedPreset={10}
        onSelectPreset={() => undefined}
        topupAmount={10}
        onTopupAmountChange={() => undefined}
        paymentAmount={10}
        calculating={false}
        onPaymentMethodSelect={() => undefined}
        paymentLoading={null}
        redemptionCode=''
        onRedemptionCodeChange={() => undefined}
        onRedeem={() => undefined}
        redeeming={false}
        amountMode='krw'
        onAmountModeChange={() => undefined}
        tossUnitPrice={1300}
      />
    )

    assert.doesNotMatch(html, /KRW based/)
    assert.doesNotMatch(html, /Quota based/)
    assert.doesNotMatch(html, /10,000원/)
    assert.match(html, />10</)
    assert.match(html, /Stripe/)
  })
})
