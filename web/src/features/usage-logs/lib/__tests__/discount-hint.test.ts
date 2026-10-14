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
import { describe, expect, test } from 'vitest'

import { formatDiscountHint } from '../discount-hint'

describe('formatDiscountHint', () => {
  test.each([
    [0.7, '−30%'],
    [0.875, '−12.5%'],
    [0, '−100%'],
    [0.9999, '−0.01%'],
  ])('%s → %s', (multiplier, expected) => {
    expect(formatDiscountHint(multiplier)).toBe(expected)
  })

  test.each([undefined, null, 1, 1.2, -0.1, Number.NaN])(
    'no hint for %s',
    (multiplier) => {
      expect(formatDiscountHint(multiplier)).toBeNull()
    }
  )
})
