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
import { afterEach, beforeEach, describe, expect, test } from 'vitest'

import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { formatCurrencyFromUSD } from '../currency'
import { formatTokens } from '../format'

describe('formatTokens', () => {
  test('keeps small and mid-range rendering unchanged', () => {
    expect(formatTokens(0)).toBe('-')
    expect(formatTokens(999)).toBe('999')
    expect(formatTokens(1500)).toBe('1.5K')
    expect(formatTokens(1e6)).toBe('1.00M')
  })
  test('uses B and T tiers for large token counts', () => {
    expect(formatTokens(1e9)).toBe('1.00B')
    expect(formatTokens(2e9)).toBe('2.00B')
    expect(formatTokens(1e12)).toBe('1.00T')
    expect(formatTokens(9e15)).toBe('9000.00T') // ~MaxWalletQuota
  })
})

describe('formatCurrencyFromUSD abbreviation tiers', () => {
  // Abbreviation applies in TOKENS display mode; quotaPerUnit 1 keeps the
  // input amount equal to the rendered token count.
  beforeEach(() => {
    useSystemConfigStore.getState().setConfig({
      currency: {
        ...DEFAULT_CURRENCY_CONFIG,
        quotaDisplayType: 'TOKENS',
        quotaPerUnit: 1,
      },
    })
  })
  afterEach(() => {
    useSystemConfigStore
      .getState()
      .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  })

  test('abbreviates thousands, millions and billions', () => {
    expect(
      formatCurrencyFromUSD(1500, { abbreviate: true, showSymbol: false })
    ).toBe('1.5k')
    expect(
      formatCurrencyFromUSD(2_000_000, { abbreviate: true, showSymbol: false })
    ).toBe('2M')
    expect(
      formatCurrencyFromUSD(5_000_000_000, {
        abbreviate: true,
        showSymbol: false,
      })
    ).toBe('5B')
  })
})
