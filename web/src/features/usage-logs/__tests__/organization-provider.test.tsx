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
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { type AuthUser, useAuthStore } from '@/stores/auth-store'

import { CommonLogsFilterBar } from '../components/common-logs-filter-bar'
import {
  UsageLogsProvider,
  useLogsViewScope,
  useUsageLogsContext,
} from '../components/usage-logs-provider'

function ScopeProbe() {
  const { isAdminView, isRootView, canManageScope, viewAccess } =
    useLogsViewScope()
  const { logsScope, organizationId } = useUsageLogsContext()
  return (
    <output>
      {JSON.stringify({
        isAdminView,
        isRootView,
        canManageScope,
        viewAccess,
        logsScope,
        organizationId: organizationId ?? null,
      })}
    </output>
  )
}

function readProbe() {
  return JSON.parse(screen.getByRole('status').textContent ?? '{}')
}

beforeEach(() => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 2, username: 'member', role: ROLE.USER } as AuthUser)
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.setUser(null)
})

describe('UsageLogsProvider organization scope', () => {
  test('reports the admin view and exposes the organization id', () => {
    render(
      <UsageLogsProvider logsScope='organization' organizationId={7}>
        <ScopeProbe />
      </UsageLogsProvider>
    )

    expect(readProbe()).toEqual({
      isAdminView: true,
      isRootView: false,
      canManageScope: false,
      viewAccess: 'organization',
      logsScope: 'organization',
      organizationId: 7,
    })
  })

  test('keeps the non-admin view for a regular user by default', () => {
    render(
      <UsageLogsProvider>
        <ScopeProbe />
      </UsageLogsProvider>
    )

    expect(readProbe()).toEqual({
      isAdminView: false,
      isRootView: false,
      canManageScope: false,
      viewAccess: 'self',
      logsScope: 'user',
      organizationId: null,
    })
  })
})

function OrganizationFilterFixture() {
  const table = useReactTable({
    data: [],
    columns: [],
    getCoreRowModel: getCoreRowModel(),
  })
  return (
    <UsageLogsProvider logsScope='organization'>
      <CommonLogsFilterBar table={table} />
    </UsageLogsProvider>
  )
}

describe('usage-logs components under the organization route', () => {
  test('render, use member groups and navigate within the organization route', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: true,
        data: url === '/api/user/self/groups' ? { default: {} } : {},
      },
    }))
    const root = createRootRoute()
    const auth = createRoute({
      getParentRoute: () => root,
      id: '_authenticated',
    })
    const logs = createRoute({
      getParentRoute: () => auth,
      path: '/organization/usage-logs/$section',
      component: OrganizationFilterFixture,
      validateSearch: (search: Record<string, unknown>) => search,
    })
    const router = createRouter({
      routeTree: root.addChildren([auth.addChildren([logs])]),
      history: createMemoryHistory({
        initialEntries: ['/organization/usage-logs/common'],
      }),
    })
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <RouterProvider router={router} />
      </QueryClientProvider>
    )

    await userEvent.click(await screen.findByRole('button', { name: 'Search' }))
    await waitFor(() =>
      expect(router.state.location.pathname).toBe(
        '/organization/usage-logs/common'
      )
    )
    expect(router.state.location.search).toMatchObject({ page: 1 })
    expect(api.get).toHaveBeenCalledWith('/api/user/self/groups')
    const urls = vi.mocked(api.get).mock.calls.map(([url]) => String(url))
    expect(urls).not.toContain('/api/group/')
    expect(urls.some((url) => url.startsWith('/api/log/'))).toBe(false)
  })
})
