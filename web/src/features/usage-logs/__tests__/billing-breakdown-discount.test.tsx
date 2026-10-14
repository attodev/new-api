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

import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { BillingBreakdown } from '../components/dialogs/details-dialog'
import type { UsageLog } from '../data/schema'
import type { LogOtherData } from '../types'

afterEach(cleanup)

function renderBreakdown(other: LogOtherData) {
  render(
    <BillingBreakdown
      log={{ quota: 5000 } as unknown as UsageLog}
      other={other}
      isAdmin={false}
    />
  )
}

describe('BillingBreakdown discount row', () => {
  it('shows the model discount multiplier after the group ratio row', () => {
    renderBreakdown({
      group_ratio: 0.9,
      discount_multiplier: 0.7,
      discount_source: 'model',
    })
    expect(screen.getByText('Group Ratio')).toBeInTheDocument()
    expect(screen.getByText('0.9000x')).toBeInTheDocument()
    expect(screen.getByText('Model discount')).toBeInTheDocument()
    expect(screen.getByText('×0.7000')).toBeInTheDocument()
    expect(screen.queryByText('Vendor discount')).toBeNull()
  })

  it('labels a vendor-sourced discount as the vendor discount', () => {
    renderBreakdown({
      group_ratio: 1,
      discount_multiplier: 0.8,
      discount_source: 'vendor',
    })
    expect(screen.getByText('Vendor discount')).toBeInTheDocument()
    expect(screen.getByText('×0.8000')).toBeInTheDocument()
    expect(screen.queryByText('Model discount')).toBeNull()
  })

  it('omits the row without a discount', () => {
    renderBreakdown({ group_ratio: 0.9 })
    expect(screen.queryByText('Model discount')).toBeNull()
    expect(screen.queryByText('Vendor discount')).toBeNull()
  })

  it('omits the row when the multiplier is 1', () => {
    renderBreakdown({
      group_ratio: 0.9,
      discount_multiplier: 1,
      discount_source: 'model',
    })
    expect(screen.queryByText('Model discount')).toBeNull()
  })
})
