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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import type {
  OrganizationUser,
  OrganizationUserSubscriptionRecord,
} from '../../types'
import { OrganizationSubscriptionsPage } from '../organization-subscriptions-page'

const api = vi.hoisted(() => ({
  assignOrganizationUserSubscription: vi.fn(),
  cancelOrganizationUserSubscription: vi.fn(),
  createOrganizationSubscriptionPlan: vi.fn(),
  disableOrganizationSubscriptionPlan: vi.fn(),
  getOrganizationSubscriptionPlans: vi.fn(),
  getOrganizationUserSubscriptions: vi.fn(),
  getOrganizationUsers: vi.fn(),
  getOrganizations: vi.fn(),
  updateOrganizationSubscriptionPlan: vi.fn(),
}))

vi.mock('../../api', () => api)

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
    quota: 0,
    used_quota: 0,
    group: 'default',
    organization_id: 7,
    organization_role: organizationRole,
  }
}

const OWNER = member(1, 'olivia', 'owner')
const ADMIN = member(2, 'adam', 'admin')
const MEMBER = member(3, 'mia', 'member')
const OTHER = member(4, 'otto', 'member')

function activeRecord(
  user: OrganizationUser,
  id: number
): OrganizationUserSubscriptionRecord {
  return {
    user,
    subscription: {
      id,
      organization_id: 7,
      user_id: user.id,
      plan_id: 1,
      amount_total: 0,
      amount_used: 0,
      start_time: 0,
      end_time: 0,
      status: 'active',
      last_reset_time: 0,
      next_reset_time: 0,
      upgrade_group: '',
      prev_user_group: '',
      assigned_by_user_id: 1,
      created_at: 0,
      updated_at: 0,
    },
  }
}

function signIn(user: OrganizationUser) {
  useAuthStore.getState().auth.setUser({
    id: user.id,
    username: user.username,
    role: 1,
    organization_id: 7,
    organization_role: user.organization_role,
  })
}

function assignOptions() {
  return screen
    .getAllByRole('option')
    .map((option) => option.textContent)
    .filter((text) => text !== 'Select user' && text !== 'Select plan')
}

function cancelButtonsIn(username: string) {
  const row = screen.getByRole('cell', { name: username }).closest('tr')
  return within(row as HTMLElement).queryAllByRole('button')
}

beforeEach(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
  for (const fn of Object.values(api)) fn.mockReset()
  api.getOrganizationSubscriptionPlans.mockResolvedValue({
    success: true,
    data: [],
  })
  api.getOrganizationUsers.mockResolvedValue({
    success: true,
    data: {
      page: 1,
      page_size: 500,
      total: 4,
      items: [OWNER, ADMIN, MEMBER, OTHER],
    },
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  useAuthStore.getState().auth.reset()
})

describe('OrganizationSubscriptionsPage owner targets', () => {
  it('does not offer the owner to a non-owner admin', async () => {
    api.getOrganizationUserSubscriptions.mockResolvedValue({
      success: true,
      data: [],
    })
    signIn(ADMIN)
    render(<OrganizationSubscriptionsPage />)

    await waitFor(() => expect(assignOptions()).toContain('mia'))
    expect(assignOptions()).not.toContain('olivia')
  })

  it('offers the owner to the owner', async () => {
    api.getOrganizationUserSubscriptions.mockResolvedValue({
      success: true,
      data: [],
    })
    signIn(OWNER)
    render(<OrganizationSubscriptionsPage />)

    await waitFor(() => expect(assignOptions()).toContain('olivia'))
  })

  it("hides the cancel action on the owner's plan from a non-owner admin", async () => {
    api.getOrganizationUserSubscriptions.mockResolvedValue({
      success: true,
      data: [activeRecord(OWNER, 11), activeRecord(MEMBER, 12)],
    })
    signIn(ADMIN)
    render(<OrganizationSubscriptionsPage />)

    await waitFor(() => expect(cancelButtonsIn('mia')).toHaveLength(1))
    expect(cancelButtonsIn('olivia')).toHaveLength(0)
  })
})
