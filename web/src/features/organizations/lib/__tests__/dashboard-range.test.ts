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
import { afterEach, describe, expect, test, vi } from 'vitest'

import { getOrganizationDashboardRange } from '../dashboard-range'

function toSeconds(isoTime: string) {
  return Date.parse(isoTime) / 1000
}

afterEach(() => {
  vi.useRealTimers()
})

describe('getOrganizationDashboardRange', () => {
  // 2026-10-28 01:30 KST is still 2026-10-27 in UTC.
  const now = '2026-10-27T16:30:00Z'

  test.each([
    ['today', '2026-10-28T00:00:00+09:00'],
    ['7d', '2026-10-22T00:00:00+09:00'],
    ['30d', '2026-09-29T00:00:00+09:00'],
  ] as const)(
    'when the KST date differs from UTC, %s starts at KST midnight',
    (preset, expectedStart) => {
      vi.useFakeTimers()
      vi.setSystemTime(new Date(now))

      expect(getOrganizationDashboardRange(preset)).toEqual({
        start_timestamp: toSeconds(expectedStart),
        end_timestamp: toSeconds(now),
      })
    }
  )
})
