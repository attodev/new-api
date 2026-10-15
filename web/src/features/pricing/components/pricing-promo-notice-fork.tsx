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
import { Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'

// fork: O11 (team d5e459993, 330d46466) — promotional pricing notice under the
// pricing page title.
export function PricingPromoNotice() {
  const { t } = useTranslation()
  return (
    <Alert variant='notice' className='mx-auto mt-3 max-w-2xl'>
      <Info aria-hidden='true' strokeWidth={2.5} />
      <AlertDescription>
        {t(
          'Prices reflect promotional rates and may vary over time and by model.'
        )}
      </AlertDescription>
    </Alert>
  )
}
