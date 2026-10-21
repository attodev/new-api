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
// fork: O13 — covers the pricing form default-value stabiliser.
import { renderHook } from '@testing-library/react'
import { expect, test } from 'vitest'

import { useStableValue } from '../pricing-forms-fork'

test('useStableValue keeps the reference while a fresh object holds equal values', () => {
  const { result, rerender } = renderHook(
    ({ value }) => useStableValue(value),
    { initialProps: { value: { ModelPrice: '{}', Expose: true } } }
  )
  const first = result.current

  // The billing section registry builds a new object on every parent render.
  rerender({ value: { ModelPrice: '{}', Expose: true } })
  expect(result.current).toBe(first)

  rerender({ value: { ModelPrice: '{"gpt-4":1}', Expose: true } })
  expect(result.current).not.toBe(first)
  expect(result.current.ModelPrice).toBe('{"gpt-4":1}')
})

test('useStableValue picks up a value that changes back after a real change', () => {
  const { result, rerender } = renderHook(
    ({ value }) => useStableValue(value),
    { initialProps: { value: { a: 1 } } }
  )
  rerender({ value: { a: 2 } })
  const changed = result.current
  rerender({ value: { a: 2 } })
  expect(result.current).toBe(changed)
  rerender({ value: { a: 1 } })
  expect(result.current).toEqual({ a: 1 })
})
