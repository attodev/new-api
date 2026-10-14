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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { UsersImportDialog } from '../users-import-dialog'

vi.mock('../../api', () => ({
  importUsers: vi.fn(async () => ({
    created: 2,
    skipped: 1,
    skipped_usernames: ['dup'],
    errors: ['line 4 (bad): initial_password is empty'],
  })),
}))

describe('UsersImportDialog', () => {
  it('uploads the chosen workbook and shows the result summary', async () => {
    const onSuccess = vi.fn()
    render(
      <UsersImportDialog open onOpenChange={() => {}} onSuccess={onSuccess} />
    )
    const input = screen.getByLabelText('Select .xlsx file') as HTMLInputElement
    const file = new File(['x'], 'users.xlsx', {
      type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    })
    await userEvent.upload(input, file)
    await userEvent.click(screen.getByRole('button', { name: 'Upload' }))
    await waitFor(() => expect(onSuccess).toHaveBeenCalled())
    expect(screen.getByText(/2 users created/)).toBeInTheDocument()
    expect(screen.getByText(/1 users skipped/)).toBeInTheDocument()
    expect(screen.getByText(/initial_password is empty/)).toBeInTheDocument()
  })
})
