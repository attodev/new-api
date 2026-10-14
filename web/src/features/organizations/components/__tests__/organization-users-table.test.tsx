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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import type { OrganizationUser } from '../../types'
import { OrganizationUsersTable } from '../organization-users-table'

const api = vi.hoisted(() => ({
  assignOrganizationUser: vi.fn(),
  createOrganization: vi.fn(),
  deleteOrganization: vi.fn(),
  exportOrgUsers: vi.fn(),
  getAssignableOrganizationUsers: vi.fn(),
  getOrganizationProfile: vi.fn(),
  getOrganizationUsers: vi.fn(),
  getOrganizationUserSubscriptions: vi.fn(),
  getOrganizations: vi.fn(),
  importOrgUsers: vi.fn(),
  removeOrganizationUserMembership: vi.fn(),
  updateOrganization: vi.fn(),
  updateOrganizationUser: vi.fn(),
}))

vi.mock('../../api', () => api)

vi.mock('@/features/users/api', () => ({
  getUsers: vi.fn(async () => ({ success: true, data: { items: [] } })),
}))

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  Link: (props: { to: string; children?: ReactNode }) => (
    <a href={props.to}>{props.children}</a>
  ),
}))

function member(
  id: number,
  username: string,
  organizationRole: OrganizationUser['organization_role']
): OrganizationUser {
  return {
    id,
    username,
    display_name: '',
    role: 1,
    status: 1,
    quota: 500000,
    used_quota: 0,
    group: 'default',
    organization_id: 7,
    organization_role: organizationRole,
  }
}

const MEMBERS = [
  member(1, 'olivia', 'owner'),
  member(2, 'adam', 'admin'),
  member(3, 'mia', 'member'),
]

function signIn(id: number, organizationRole: string) {
  useAuthStore.getState().auth.setUser({
    id,
    username: MEMBERS.find((m) => m.id === id)?.username ?? 'someone',
    role: 1,
    organization_id: 7,
    organization_role: organizationRole,
  })
}

function rowOf(username: string) {
  return screen.getByText(username).closest('tr') as HTMLElement
}

beforeEach(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
  for (const fn of Object.values(api)) fn.mockReset()
  api.getOrganizationUsers.mockResolvedValue({
    success: true,
    data: { page: 1, page_size: 20, total: 3, items: MEMBERS },
  })
  api.getOrganizationUserSubscriptions.mockResolvedValue({
    success: true,
    data: [],
  })
  api.getOrganizationProfile.mockResolvedValue({
    success: true,
    data: {
      id: 7,
      name: 'Acme',
      description: '',
      status: 1,
      owner_user_id: 1,
      quota: 1000000,
      used_quota: 0,
    },
  })
  api.getAssignableOrganizationUsers.mockResolvedValue({
    success: true,
    data: { page: 1, page_size: 50, total: 0, items: [] },
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  useAuthStore.getState().auth.reset()
})

describe('OrganizationUsersTable', () => {
  it('shows the owner a Role column with member/admin selectors on other rows', async () => {
    signIn(1, 'owner')
    render(<OrganizationUsersTable />)

    expect(
      await screen.findByRole('columnheader', { name: /Role/ })
    ).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText('mia')).toBeInTheDocument())
    expect(screen.getByText('Assign Organization Member')).toBeInTheDocument()

    expect(within(rowOf('adam')).getByRole('combobox')).toBeInTheDocument()
    expect(within(rowOf('mia')).getByRole('combobox')).toBeInTheDocument()
    // The owner's own row is never editable here.
    expect(within(rowOf('olivia')).queryByRole('combobox')).toBeNull()
    expect(within(rowOf('olivia')).getByRole('spinbutton')).toBeDisabled()
    expect(within(rowOf('olivia')).getByRole('checkbox')).toBeDisabled()
  })

  it('does not let a non-owner admin edit the owner or their own row', async () => {
    signIn(2, 'admin')
    render(<OrganizationUsersTable />)

    await waitFor(() => expect(screen.getByText('mia')).toBeInTheDocument())
    expect(screen.queryAllByRole('combobox')).toHaveLength(0)
    expect(within(rowOf('olivia')).getByRole('spinbutton')).toBeDisabled()
    expect(within(rowOf('adam')).getByRole('spinbutton')).toBeDisabled()
    expect(within(rowOf('adam')).getByRole('checkbox')).toBeDisabled()
    expect(within(rowOf('mia')).getByRole('spinbutton')).toBeEnabled()
    expect(within(rowOf('mia')).getByRole('checkbox')).toBeEnabled()
  })

  it('shows a plain member no add-member section', async () => {
    signIn(3, 'member')
    render(<OrganizationUsersTable />)

    await waitFor(() => expect(api.getOrganizationProfile).toHaveBeenCalled())
    expect(screen.queryByText('Assign Organization Member')).toBeNull()
    expect(api.getOrganizationUsers).not.toHaveBeenCalled()
  })

  it('lets root export the selected organization', async () => {
    useAuthStore.getState().auth.setUser({
      id: 99,
      username: 'root',
      role: 100,
    })
    api.getOrganizations.mockResolvedValue({
      success: true,
      data: {
        page: 1,
        page_size: 50,
        total: 1,
        items: [
          {
            id: 7,
            name: 'Acme',
            description: '',
            status: 1,
            owner_user_id: 1,
            quota: 0,
            used_quota: 0,
          },
        ],
      },
    })
    api.exportOrgUsers.mockResolvedValue(undefined)
    render(<OrganizationUsersTable />)

    await userEvent.click(await screen.findByRole('button', { name: 'Export' }))
    expect(api.exportOrgUsers).toHaveBeenCalledWith(7)
    expect(screen.getByRole('button', { name: 'Import' })).toBeInTheDocument()
  })

  it('hides import/export from root until an organization is selected', async () => {
    useAuthStore.getState().auth.setUser({
      id: 99,
      username: 'root',
      role: 100,
    })
    api.getOrganizations.mockResolvedValue({
      success: true,
      data: { page: 1, page_size: 50, total: 0, items: [] },
    })
    render(<OrganizationUsersTable />)

    await waitFor(() => expect(api.getOrganizations).toHaveBeenCalled())
    expect(screen.queryByRole('button', { name: 'Export' })).toBeNull()
  })
})
