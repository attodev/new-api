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

import {
  calculateOrganizationWaffoAmount,
  deleteOrganization,
  exportOrgUsers,
  getAllOrganizations,
  importOrgUsers,
  lookupOrganizationMember,
} from '../api'

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

describe('organization list and quotes', () => {
  function organization(id: number) {
    return {
      id,
      name: `org-${id}`,
      description: '',
      status: 1,
      owner_user_id: id,
      quota: 0,
      used_quota: 0,
    }
  }

  test('getAllOrganizations pages past the 100-item API maximum', async () => {
    const firstPage = Array.from({ length: 100 }, (_, i) => organization(i + 1))
    const secondPage = Array.from({ length: 50 }, (_, i) =>
      organization(i + 101)
    )
    vi.mocked(api.get)
      .mockResolvedValueOnce({
        data: {
          success: true,
          data: { page: 1, page_size: 100, total: 150, items: firstPage },
        },
      })
      .mockResolvedValueOnce({
        data: {
          success: true,
          data: { page: 2, page_size: 100, total: 150, items: secondPage },
        },
      })

    const res = await getAllOrganizations()

    expect(vi.mocked(api.get).mock.calls.map((call) => call[0])).toEqual([
      '/api/organizations/?p=1&page_size=100',
      '/api/organizations/?p=2&page_size=100',
    ])
    expect(res.success).toBe(true)
    expect(res.data?.items).toHaveLength(150)
    expect(res.data?.items.at(-1)?.id).toBe(150)
  })

  test('calculateOrganizationWaffoAmount quotes through the organization Waffo endpoint', async () => {
    vi.mocked(api.post).mockResolvedValue({
      data: { message: 'success', data: '9.50' },
    })
    await expect(
      calculateOrganizationWaffoAmount({ amount: 10 })
    ).resolves.toEqual({ message: 'success', data: '9.50' })
    expect(vi.mocked(api.post).mock.calls[0][0]).toBe(
      '/api/organization/waffo/amount'
    )
    expect(vi.mocked(api.post).mock.calls[0][1]).toEqual({ amount: 10 })
  })
})

describe('organization member lookup', () => {
  test('sends the exact username and returns the single match', async () => {
    const user = {
      id: 5,
      username: 'a b',
      display_name: 'A',
      organization_id: 0,
    }
    vi.mocked(api.get).mockResolvedValue({
      status: 200,
      data: { success: true, data: user },
    })
    await expect(
      lookupOrganizationMember({ username: 'a b' })
    ).resolves.toEqual(user)
    expect(vi.mocked(api.get).mock.calls[0][0]).toBe(
      '/api/organization/users/lookup?username=a+b'
    )
  })

  test('resolves to null when no user matches (404)', async () => {
    vi.mocked(api.get).mockResolvedValue({
      status: 404,
      data: { success: false, message: 'user not found' },
    })
    await expect(
      lookupOrganizationMember({ email: 'x@example.com' })
    ).resolves.toBeNull()
    expect(vi.mocked(api.get).mock.calls[0][0]).toBe(
      '/api/organization/users/lookup?email=x%40example.com'
    )
  })

  test('rejects with the server message on other failures', async () => {
    vi.mocked(api.get).mockResolvedValue({ status: 200, ...failure })
    await expect(lookupOrganizationMember({ username: 'a' })).rejects.toThrow(
      'nope'
    )
  })
})
