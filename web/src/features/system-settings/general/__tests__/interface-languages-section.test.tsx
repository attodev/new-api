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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { InterfaceLanguagesSection } from '../interface-languages-section'

function Fixture({ value }: { value: string }) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <InterfaceLanguagesSection
          defaultValues={{ InterfaceLanguages: value }}
        />
      </SettingsPageProvider>
    </>
  )
}

function renderSection(value = 'en,ko') {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Fixture value={value} />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true } })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

test('offers English and Korean and saves the checked ones as a comma-separated option', async () => {
  const user = userEvent.setup()
  renderSection('en')

  expect(screen.getAllByRole('checkbox')).toHaveLength(2)
  expect(screen.getByRole('checkbox', { name: 'English' })).toBeChecked()
  expect(screen.getByRole('checkbox', { name: '한국어' })).not.toBeChecked()

  await user.click(screen.getByRole('checkbox', { name: '한국어' }))
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))

  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'InterfaceLanguages',
      value: 'en,ko',
    })
  )
})

test('refuses to save an empty selection', async () => {
  const user = userEvent.setup()
  renderSection()

  await user.click(screen.getByRole('checkbox', { name: 'English' }))
  await user.click(screen.getByRole('checkbox', { name: '한국어' }))
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))

  expect(await screen.findByText('Select at least one language.')).toBeVisible()
  expect(api.put).not.toHaveBeenCalled()
})
