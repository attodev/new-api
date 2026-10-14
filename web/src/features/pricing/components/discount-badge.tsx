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
// fork: O6 — model/vendor discount badge and struck-through list price.
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import { discountBadgeClasses, withDiscount } from '../lib/discount-color'

export function DiscountBadge(props: { percent?: number; className?: string }) {
  const { t } = useTranslation()
  if (!props.percent || props.percent <= 0) return null
  return (
    <Badge
      variant='outline'
      className={cn(
        'border-transparent',
        discountBadgeClasses(props.percent),
        props.className
      )}
    >
      {t('{{percent}}% off', { percent: props.percent })}
    </Badge>
  )
}

// DiscountedPrice renders the struck list price on its own small line above the
// discounted price when a discount applies, otherwise just the list price.
// `format(0)` must return the list price.
export function DiscountedPrice(props: {
  percent?: number
  format: (discountPercent: number) => string
}) {
  const price = withDiscount(props.percent, props.format)
  return (
    <>
      {price.original && (
        <s className='text-muted-foreground/60 block text-xs font-normal'>
          {price.original}
        </s>
      )}
      {price.value}
    </>
  )
}
