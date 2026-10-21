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
// fork: O13 — keep the pricing forms stable across re-renders.
import { useState } from 'react'

function sameValue(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true
  if (
    typeof a !== 'object' ||
    typeof b !== 'object' ||
    a === null ||
    b === null
  ) {
    return false
  }
  const aKeys = Object.keys(a)
  const bKeys = Object.keys(b)
  return (
    aKeys.length === bKeys.length &&
    aKeys.every(
      (key) =>
        Object.hasOwn(b, key) &&
        sameValue(
          (a as Record<string, unknown>)[key],
          (b as Record<string, unknown>)[key]
        )
    )
  )
}

/**
 * Returns the previous reference while `value` is deeply equal to it.
 *
 * RatioSettingsCard resets its forms whenever the defaults object changes by
 * reference, but the billing section registry builds a fresh object on every
 * parent render. Any unrelated re-render (tab switch, focus refetch) therefore
 * wiped unsaved edits. Keeping the reference stable until the values really
 * change makes the reset effects fire only for real settings changes.
 */
export function useStableValue<T>(value: T): T {
  const [stable, setStable] = useState(value)
  if (stable !== value && !sameValue(stable, value)) {
    setStable(value)
    return value
  }
  return stable
}
