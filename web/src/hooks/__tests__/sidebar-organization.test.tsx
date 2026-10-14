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
import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { useSidebarView } from '../use-sidebar-view'

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  useLocation: (opts?: { select?: (l: { pathname: string }) => unknown }) => {
    const location = { pathname: '/dashboard/overview' }
    return opts?.select ? opts.select(location) : location
  },
}))

beforeEach(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  useAuthStore.getState().auth.reset()
})

function groupIdsFor(role: number, organizationRole?: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], { SidebarModulesAdmin: '' })
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'alice',
    role,
    organization_id: organizationRole ? 7 : 0,
    organization_role: organizationRole ?? '',
  })
  function Wrapper(props: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        {props.children}
      </QueryClientProvider>
    )
  }
  const { result } = renderHook(() => useSidebarView(), { wrapper: Wrapper })
  return result.current.navGroups.map((group) => group.id)
}

describe('organization sidebar group', () => {
  it.each([
    ['root', 100, undefined],
    ['an organization owner', 1, 'owner'],
    ['an organization admin', 1, 'admin'],
  ])('is shown to %s, after Personal', (_label, role, organizationRole) => {
    const ids = groupIdsFor(role, organizationRole)
    expect(ids).toContain('organization')
    expect(ids.indexOf('organization')).toBe(ids.indexOf('personal') + 1)
  })

  it.each([
    ['a plain member', 1, 'member'],
    ['a user outside any organization', 1, undefined],
    ['a site admin outside any organization', 10, undefined],
  ])('is hidden from %s', (_label, role, organizationRole) => {
    expect(groupIdsFor(role, organizationRole)).not.toContain('organization')
  })

  it('lists the five organization entries', () => {
    const client = new QueryClient()
    client.setQueryData(['status'], { SidebarModulesAdmin: '' })
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'alice',
      role: 1,
      organization_role: 'owner',
    })
    const { result } = renderHook(() => useSidebarView(), {
      wrapper: (props: { children: ReactNode }) => (
        <QueryClientProvider client={client}>
          {props.children}
        </QueryClientProvider>
      ),
    })
    const group = result.current.navGroups.find((g) => g.id === 'organization')
    expect(
      group?.items.map((item) => ('url' in item ? item.url : undefined))
    ).toEqual([
      '/organization',
      '/organization/dashboard',
      '/organization/usage-logs/common',
      '/organization/usage-logs/task',
      '/organization/subscriptions',
    ])
  })
})
