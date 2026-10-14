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
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { deleteOrganization, exportOrgUsers, importOrgUsers } from '../api'

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
}))

const failure = { data: { success: false, message: 'nope' } }

beforeEach(() => {
  vi.mocked(api.get).mockReset()
  vi.mocked(api.post).mockReset()
  vi.mocked(api.delete).mockReset()
})

describe('organization api business failures', () => {
  test('deleteOrganization rejects with the server message', async () => {
    vi.mocked(api.delete).mockResolvedValue(failure)
    await expect(deleteOrganization(1)).rejects.toThrow('nope')
  })

  test('deleteOrganization resolves on success', async () => {
    vi.mocked(api.delete).mockResolvedValue({ data: { success: true } })
    await expect(deleteOrganization(1)).resolves.toBeUndefined()
  })

  test('importOrgUsers rejects with the server message', async () => {
    vi.mocked(api.post).mockResolvedValue(failure)
    await expect(
      importOrgUsers(new File(['x'], 'users.xlsx'), 1)
    ).rejects.toThrow('nope')
  })

  test('importOrgUsers resolves to the result data on success', async () => {
    const result = {
      assigned: 1,
      created: 0,
      updated: 0,
      updated_usernames: [],
      removed: 0,
      removed_usernames: [],
      errors: [],
    }
    vi.mocked(api.post).mockResolvedValue({
      data: { success: true, data: result },
    })
    await expect(
      importOrgUsers(new File(['x'], 'users.xlsx'), 1)
    ).resolves.toEqual(result)
  })

  test('exportOrgUsers throws instead of saving a JSON error body', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: new Blob([JSON.stringify({ success: false, message: 'denied' })], {
        type: 'application/json',
      }),
    })
    await expect(exportOrgUsers(1)).rejects.toThrow('denied')
  })
})
