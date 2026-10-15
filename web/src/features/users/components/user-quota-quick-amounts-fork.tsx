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
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'

const QUICK_QUOTA_AMOUNTS = [1, 5, 10, 100]

interface UserQuotaQuickAmountsProps {
  onPick: (amount: number) => void
}

// fork: O11 (team 6fc579349) — fixed-amount shortcuts above the amount input.
export function UserQuotaQuickAmounts(props: UserQuotaQuickAmountsProps) {
  const { t } = useTranslation()
  return (
    <div className='space-y-2'>
      <Label>{t('Quick amount')}</Label>
      <div className='flex flex-wrap gap-1'>
        {QUICK_QUOTA_AMOUNTS.map((amount) => (
          <Button
            key={amount}
            type='button'
            variant='outline'
            size='sm'
            onClick={() => props.onPick(amount)}
          >
            {formatQuota(parseQuotaFromDollars(amount))}
          </Button>
        ))}
      </div>
    </div>
  )
}
