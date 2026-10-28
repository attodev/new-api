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
import type { OrganizationDashboardRangePreset } from '@/features/organizations/types'

// The backend groups organization usage into KST days, so range boundaries are
// KST midnights regardless of the browser time zone. KST has no daylight saving
// time, which keeps the offset fixed.
const KST_OFFSET_SECONDS = 9 * 60 * 60
const DAY_SECONDS = 24 * 60 * 60

export function getOrganizationDashboardRange(
  preset: Exclude<OrganizationDashboardRangePreset, 'custom'>
) {
  const now = Math.floor(Date.now() / 1000)
  const todayStart = now - ((now + KST_OFFSET_SECONDS) % DAY_SECONDS)

  let pastDays = 0
  if (preset === '7d') {
    pastDays = 6
  } else if (preset === '30d') {
    pastDays = 29
  }

  return {
    start_timestamp: todayStart - pastDays * DAY_SECONDS,
    end_timestamp: now,
  }
}
