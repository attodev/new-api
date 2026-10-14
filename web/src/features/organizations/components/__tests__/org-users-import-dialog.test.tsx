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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { OrgUsersImportDialog } from '../org-users-import-dialog'

const importOrgUsersMock = vi.hoisted(() => vi.fn())

vi.mock('../../api', () => ({ importOrgUsers: importOrgUsersMock }))

describe('OrgUsersImportDialog', () => {
  it('imports into the given organization', async () => {
    importOrgUsersMock.mockResolvedValue({
      assigned: 1,
      created: 0,
      updated: 0,
      updated_usernames: [],
      removed: 0,
      removed_usernames: [],
      errors: [],
    })
    const file = new File(['x'], 'members.xlsx')
    render(
      <OrgUsersImportDialog
        open
        onOpenChange={() => {}}
        onSuccess={() => {}}
        organizationId={7}
      />
    )
    await userEvent.upload(screen.getByLabelText('Select .xlsx file'), file)
    await userEvent.click(screen.getByRole('button', { name: 'Upload' }))
    expect(importOrgUsersMock).toHaveBeenCalledWith(file, 7, false)
    expect(await screen.findByText(/1 members assigned/)).toBeInTheDocument()
  })
})
