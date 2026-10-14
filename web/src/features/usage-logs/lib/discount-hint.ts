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
// fork: O6 — the usage-log list shows the pre-discount group ratio
// (group_ratio is logged as the base); a model/vendor discount is hinted
// next to it as a plain number so the row reconciles with the Cost column.

/**
 * Returns `−{percent}%` for a logged discount multiplier (0.7 → `−30%`,
 * 0 → `−100%`), or null when there is no discount.
 */
export function formatDiscountHint(
  multiplier: number | null | undefined
): string | null {
  if (
    multiplier == null ||
    !Number.isFinite(multiplier) ||
    multiplier < 0 ||
    multiplier >= 1
  ) {
    return null
  }
  const percent = Math.round((1 - multiplier) * 10000) / 100
  return `−${percent}%`
}
