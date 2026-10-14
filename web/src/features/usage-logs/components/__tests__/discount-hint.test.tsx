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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import { usageLogSchema } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { useCommonLogsColumns } from '../columns/common-logs-columns'
import { UsageLogsMobileList } from '../usage-logs-mobile-card'
import { UsageLogsProvider } from '../usage-logs-provider'

// fork: O6 (review M3) — the list row's group ratio is the pre-discount base,
// so a logged discount_multiplier is hinted next to it.

vi.mock('@lobehub/icons', () => ({}))

function makeLog(other: LogOtherData) {
  return usageLogSchema.parse({
    id: 1,
    user_id: 2,
    created_at: 1788840000,
    type: 2,
    content: '',
    model_name: 'gpt-4',
    username: 'user',
    channel: 3,
    channel_name: 'channel',
    token_name: 'token',
    group: 'default',
    quota: 67500,
    use_time: 1,
    prompt_tokens: 100,
    completion_tokens: 10,
    other: JSON.stringify(other),
  })
}

function TokenCell(props: { other: LogOtherData }) {
  const table = useReactTable({
    data: [makeLog(props.other)],
    columns: useCommonLogsColumns(true, false),
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0].getAllCells()
    .find((item) => item.column.id === 'token_name')
  if (!cell) throw new Error('The log must have a token column')
  return (
    <div data-testid='token-cell'>
      {flexRender(cell.column.columnDef.cell, cell.getContext())}
    </div>
  )
}

function MobileList(props: { other: LogOtherData }) {
  const table = useReactTable({
    data: [makeLog(props.other)],
    columns: useCommonLogsColumns(true, false),
    getCoreRowModel: getCoreRowModel(),
  })
  return <UsageLogsMobileList table={table} logCategory='common' />
}

function renderWithProviders(node: React.ReactNode) {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <UsageLogsProvider>{node}</UsageLogsProvider>
    </QueryClientProvider>
  )
}

test('desktop token cell hints the discount next to the base group ratio', () => {
  renderWithProviders(
    <TokenCell
      other={{
        group_ratio: 1.5,
        discount_multiplier: 0.75,
        discount_source: 'model',
      }}
    />
  )
  const cell = screen.getByTestId('token-cell')
  expect(cell.textContent).toContain('1.5x')
  expect(cell.textContent).toContain('−25%')
})

test('desktop token cell hints a discount even at the default 1x ratio', () => {
  renderWithProviders(
    <TokenCell
      other={{
        group_ratio: 1,
        discount_multiplier: 0,
        discount_source: 'vendor',
      }}
    />
  )
  expect(screen.getByTestId('token-cell').textContent).toContain('−100%')
})

test('desktop token cell shows no hint without a discount', () => {
  renderWithProviders(<TokenCell other={{ group_ratio: 1.5 }} />)
  const cell = screen.getByTestId('token-cell')
  expect(cell.textContent).toContain('1.5x')
  expect(cell.textContent).not.toContain('%')
})

test('mobile card hints the discount next to the group ratio', () => {
  renderWithProviders(
    <MobileList
      other={{
        group_ratio: 1,
        discount_multiplier: 0.75,
        discount_source: 'model',
      }}
    />
  )
  expect(screen.getByText(/Group Ratio: 1×/).textContent).toContain('−25%')
})
