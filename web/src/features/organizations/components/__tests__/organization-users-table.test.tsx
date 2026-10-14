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
  getOrganizationProfile: vi.fn(),
  getOrganizationUsers: vi.fn(),
  getOrganizationUserSubscriptions: vi.fn(),
  getAllOrganizations: vi.fn(),
  importOrgUsers: vi.fn(),
  lookupOrganizationMember: vi.fn(),
  removeOrganizationUserMembership: vi.fn(),
  updateOrganization: vi.fn(),
  updateOrganizationUser: vi.fn(),
}))

vi.mock('../../api', () => api)

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))

vi.mock('sonner', () => ({ toast }))

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
    member_status: 1,
    quota: 500000,
    used_quota: 0,
    group: 'default',
    organization_id: 7,
    organization_role: organizationRole,
    organization_remark: '',
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
  toast.success.mockReset()
  toast.error.mockReset()
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

  it('adds a member by exact lookup with the chosen role', async () => {
    signIn(1, 'owner')
    api.lookupOrganizationMember.mockResolvedValue({
      id: 42,
      username: 'newbie',
      display_name: 'New Bie',
      organization_id: 0,
    })
    api.assignOrganizationUser.mockResolvedValue({ success: true })
    render(<OrganizationUsersTable />)
    await waitFor(() => expect(screen.getByText('mia')).toBeInTheDocument())
    // Nothing is listed before a lookup.
    expect(screen.queryByRole('button', { name: 'Add' })).toBeNull()

    await userEvent.type(
      screen.getByPlaceholderText('Username or email'),
      ' newbie '
    )
    await userEvent.click(screen.getByRole('button', { name: 'Look up user' }))

    expect(api.lookupOrganizationMember).toHaveBeenCalledWith({
      username: 'newbie',
    })
    expect(await screen.findByText('newbie')).toBeInTheDocument()
    expect(screen.getByText('New Bie')).toBeInTheDocument()
    await userEvent.selectOptions(
      screen.getByRole('combobox', { name: 'New member role' }),
      'admin'
    )
    await userEvent.click(screen.getByRole('button', { name: 'Add' }))

    expect(api.assignOrganizationUser).toHaveBeenCalledWith(42, {
      organization_role: 'admin',
    })
    expect(toast.success).toHaveBeenCalledWith('Organization member assigned')
  })

  it('looks up by email and shows when no user matches', async () => {
    signIn(2, 'admin')
    api.lookupOrganizationMember.mockResolvedValue(null)
    render(<OrganizationUsersTable />)
    await waitFor(() => expect(screen.getByText('mia')).toBeInTheDocument())

    await userEvent.type(
      screen.getByPlaceholderText('Username or email'),
      'ghost@example.com{Enter}'
    )

    expect(await screen.findByText('User not found')).toBeInTheDocument()
    // Username first; an input with "@" falls back to an email lookup.
    expect(api.lookupOrganizationMember.mock.calls).toEqual([
      [{ username: 'ghost@example.com' }],
      [{ email: 'ghost@example.com' }],
    ])
    expect(screen.queryByRole('button', { name: 'Add' })).toBeNull()
  })

  it('explains the personal-balance refusal and adds non-owner picks as members', async () => {
    signIn(2, 'admin')
    api.lookupOrganizationMember.mockResolvedValue({
      id: 43,
      username: 'rich',
      display_name: '',
      organization_id: 0,
    })
    api.assignOrganizationUser.mockResolvedValue({
      success: false,
      message:
        'user still has a personal balance, affiliate balance or subscription; it must be cleared before joining an organization',
      message_key:
        'user still has a personal balance, affiliate balance or subscription; it must be cleared before joining an organization',
    })
    render(<OrganizationUsersTable />)
    await waitFor(() => expect(screen.getByText('mia')).toBeInTheDocument())

    await userEvent.type(
      screen.getByPlaceholderText('Username or email'),
      'rich'
    )
    await userEvent.click(screen.getByRole('button', { name: 'Look up user' }))
    await screen.findByText('rich')
    // Only the owner picks a role; other admins add plain members.
    expect(
      screen.queryByRole('combobox', { name: 'New member role' })
    ).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: 'Add' }))

    expect(api.assignOrganizationUser).toHaveBeenCalledWith(43, {
      organization_role: 'member',
    })
    expect(toast.error).toHaveBeenCalledWith(
      'This user still has a personal balance or subscription'
    )
  })

  it('finds a username that contains "@" without an email lookup', async () => {
    signIn(2, 'admin')
    api.lookupOrganizationMember.mockResolvedValue({
      id: 44,
      username: 'odd@name',
      display_name: '',
      organization_id: 0,
    })
    api.assignOrganizationUser.mockResolvedValue({ success: true })
    render(<OrganizationUsersTable />)
    await waitFor(() => expect(screen.getByText('mia')).toBeInTheDocument())

    await userEvent.type(
      screen.getByPlaceholderText('Username or email'),
      'odd@name{Enter}'
    )

    expect(await screen.findByText('odd@name')).toBeInTheDocument()
    expect(api.lookupOrganizationMember.mock.calls).toEqual([
      [{ username: 'odd@name' }],
    ])
    await userEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(api.assignOrganizationUser).toHaveBeenCalledWith(44, {
      organization_role: 'member',
    })
  })

  it('marks an existing member as already a member and blocks Add', async () => {
    signIn(1, 'owner')
    api.lookupOrganizationMember.mockResolvedValue({
      id: 2,
      username: 'adam',
      display_name: '',
      organization_id: 7,
    })
    render(<OrganizationUsersTable />)
    await waitFor(() => expect(screen.getByText('mia')).toBeInTheDocument())

    await userEvent.type(
      screen.getByPlaceholderText('Username or email'),
      'adam'
    )
    await userEvent.click(screen.getByRole('button', { name: 'Look up user' }))

    expect(await screen.findByText('Already a member')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add' })).toBeDisabled()
    expect(
      screen.queryByRole('combobox', { name: 'New member role' })
    ).toBeNull()
    expect(api.assignOrganizationUser).not.toHaveBeenCalled()
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
    api.getAllOrganizations.mockResolvedValue({
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

  it('suspends selected members through the organization member status', async () => {
    signIn(1, 'owner')
    api.updateOrganizationUser.mockResolvedValue({ success: true })
    render(<OrganizationUsersTable />)

    await waitFor(() => expect(screen.getByText('mia')).toBeInTheDocument())
    expect(within(rowOf('mia')).getByText('Active')).toBeInTheDocument()
    await userEvent.click(within(rowOf('mia')).getByRole('checkbox'))
    await userEvent.click(screen.getByRole('button', { name: 'Suspend' }))
    expect(
      within(rowOf('mia')).getByText('Pending suspension')
    ).toBeInTheDocument()
    expect(within(rowOf('mia')).getByText('Suspended')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(api.updateOrganizationUser).toHaveBeenCalledWith(3, {
        member_status: 2,
      })
    )
  })

  it('resumes a suspended member', async () => {
    signIn(1, 'owner')
    api.getOrganizationUsers.mockResolvedValue({
      success: true,
      data: {
        page: 1,
        page_size: 20,
        total: 3,
        items: [MEMBERS[0], MEMBERS[1], { ...MEMBERS[2], member_status: 2 }],
      },
    })
    api.updateOrganizationUser.mockResolvedValue({ success: true })
    render(<OrganizationUsersTable />)

    await waitFor(() => expect(screen.getByText('mia')).toBeInTheDocument())
    expect(within(rowOf('mia')).getByText('Suspended')).toBeInTheDocument()
    await userEvent.click(within(rowOf('mia')).getByRole('checkbox'))
    await userEvent.click(screen.getByRole('button', { name: 'Resume' }))
    expect(within(rowOf('mia')).getByText('Pending resume')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(api.updateOrganizationUser).toHaveBeenCalledWith(3, {
        member_status: 1,
      })
    )
  })

  it('edits the organization note, not the platform remark', async () => {
    signIn(1, 'owner')
    api.updateOrganizationUser.mockResolvedValue({ success: true })
    render(<OrganizationUsersTable />)

    await waitFor(() => expect(screen.getByText('mia')).toBeInTheDocument())
    expect(
      screen.getByRole('columnheader', { name: 'Organization note' })
    ).toBeInTheDocument()
    expect(
      within(rowOf('olivia')).getByRole('textbox', {
        name: 'Organization note',
      })
    ).toBeDisabled()
    const note = within(rowOf('mia')).getByRole('textbox', {
      name: 'Organization note',
    })
    await userEvent.type(note, 'night shift')
    await userEvent.tab()

    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(api.updateOrganizationUser).toHaveBeenCalledWith(3, {
        organization_remark: 'night shift',
      })
    )
  })

  it('hides import/export from root until an organization is selected', async () => {
    useAuthStore.getState().auth.setUser({
      id: 99,
      username: 'root',
      role: 100,
    })
    api.getAllOrganizations.mockResolvedValue({
      success: true,
      data: { page: 1, page_size: 50, total: 0, items: [] },
    })
    render(<OrganizationUsersTable />)

    await waitFor(() => expect(api.getAllOrganizations).toHaveBeenCalled())
    expect(screen.queryByRole('button', { name: 'Export' })).toBeNull()
  })
})
