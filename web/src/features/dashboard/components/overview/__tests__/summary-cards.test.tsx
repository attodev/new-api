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
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { formatQuota } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { SummaryCards } from '../summary-cards'

const STORE_QUOTA = 1_000_000
const SERVER_QUOTA = 5_000_000
let client: QueryClient
let selfResponse: { success: boolean; data?: Record<string, unknown> }

beforeEach(() => {
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'dashboard-user',
    role: 1,
    quota: STORE_QUOTA,
    used_quota: 1000,
    request_count: 1,
  })
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  selfResponse = {
    success: true,
    data: {
      id: 1,
      username: 'dashboard-user',
      role: 1,
      quota: SERVER_QUOTA,
      used_quota: 1,
      request_count: 2,
    },
  }
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    switch (url) {
      case '/api/user/self':
        return { data: selfResponse }
      case '/api/data/self':
        return { data: { success: true, data: [] } }
      case '/api/status':
        return { data: { data: {} } }
      default:
        throw new Error(`Unexpected request: ${url}`)
    }
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  vi.restoreAllMocks()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
})

async function renderCards() {
  const router = createRouter({
    routeTree: createRootRoute({ component: SummaryCards }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('summary cards balance', () => {
  it('replaces the login-time store snapshot with the balance fetched from the server', async () => {
    await renderCards()

    expect(
      (await screen.findAllByText(formatQuota(SERVER_QUOTA))).length
    ).toBeGreaterThan(0)
    expect(screen.queryByText(formatQuota(STORE_QUOTA))).not.toBeInTheDocument()
  })

  it('keeps showing the store balance when the server fetch is unsuccessful', async () => {
    selfResponse = { success: false }
    await renderCards()

    expect(
      (await screen.findAllByText(formatQuota(STORE_QUOTA))).length
    ).toBeGreaterThan(0)
    await vi.waitFor(() =>
      expect(api.get).toHaveBeenCalledWith('/api/user/self', expect.anything())
    )
    expect(
      screen.queryByText(formatQuota(SERVER_QUOTA))
    ).not.toBeInTheDocument()
  })
})
