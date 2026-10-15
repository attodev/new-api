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
import { act, cleanup, render, screen } from '@testing-library/react'
import i18n from 'i18next'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'

import ko from '@/i18n/locales/ko.json'

import { PricingPromoNotice } from '../components/pricing-promo-notice-fork'

beforeAll(() => {
  i18n.addResourceBundle('ko', 'translation', ko.translation, true, true)
})

afterEach(async () => {
  cleanup()
  await act(async () => {
    await i18n.changeLanguage('en')
  })
})

describe('pricing promotional notice', () => {
  it('renders the notice with the emphasized notice variant', () => {
    render(<PricingPromoNotice />)
    const text = screen.getByText(
      'Prices reflect promotional rates and may vary over time and by model.'
    )
    expect(text).toBeVisible()
    const alert = screen.getByRole('alert')
    expect(alert).toHaveClass('w-fit')
    expect(alert).toHaveClass(
      '*:data-[slot=alert-description]:font-bold',
      '*:data-[slot=alert-description]:text-warning',
      '*:[svg]:text-warning'
    )
  })

  it('renders the Korean notice', async () => {
    await act(async () => {
      await i18n.changeLanguage('ko')
    })
    render(<PricingPromoNotice />)
    expect(
      screen.getByText(
        '표시된 가격은 프로모션 요율이며 시기와 모델에 따라 달라질 수 있습니다.'
      )
    ).toBeVisible()
  })
})
