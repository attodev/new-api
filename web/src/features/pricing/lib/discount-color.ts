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
// fork: O6 — ported from team web/default/src/features/pricing/lib/discount-color.ts
// Badge colour classes for a discount percent (heatmap: hotter = deeper discount).
const DISCOUNT_BADGE_TIERS: Array<[min: number, classes: string]> = [
  [50, 'bg-red-500/15 text-red-600 dark:text-red-400'],
  [30, 'bg-orange-500/15 text-orange-600 dark:text-orange-400'],
  [15, 'bg-amber-500/15 text-amber-600 dark:text-amber-400'],
]
const DISCOUNT_BADGE_DEFAULT =
  'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400'

export function discountBadgeClasses(percent: number): string {
  const tier = DISCOUNT_BADGE_TIERS.find(([min]) => percent >= min)
  return tier ? tier[1] : DISCOUNT_BADGE_DEFAULT
}

// withDiscount formats a price twice when a discount applies: `value` is the
// discounted price and `original` the list price to strike through.
export function withDiscount(
  percent: number | undefined,
  format: (discountPercent: number) => string
): { value: string; original?: string } {
  if (!percent || percent <= 0) return { value: format(0) }
  return { value: format(percent), original: format(0) }
}
