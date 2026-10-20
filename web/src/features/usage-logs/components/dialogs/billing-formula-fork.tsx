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

import { ChevronDown, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  buildBillingFormula,
  type BillingFormulaLog,
} from '../../lib/billing-formula-fork'
import type { LogOtherData } from '../../types'

// fork: O6 — collapsed "Show formula" toggle under the Billing Details rows.
export function BillingFormula(props: {
  log: BillingFormulaLog
  other: LogOtherData
  fmtAmount: (usd: number) => string
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const lines = buildBillingFormula(props.log, props.other, props.fmtAmount)
  if (!lines) return null

  return (
    <div className='mt-1.5 min-w-0'>
      <button
        type='button'
        className='text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs transition-colors'
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        {open ? (
          <ChevronDown className='size-3' />
        ) : (
          <ChevronRight className='size-3' />
        )}
        {open ? t('Hide formula') : t('Show formula')}
      </button>
      {open && (
        <div className='bg-muted/40 mt-1 min-w-0 rounded border p-2'>
          {lines.map((line) => (
            <p
              key={line}
              className='font-mono text-[11px] leading-relaxed break-all'
            >
              {line}
            </p>
          ))}
        </div>
      )}
    </div>
  )
}
