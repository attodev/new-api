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
// fork: O13 — a signed-in visit to /sign-in?redirect=/oauth2/... loads the OIDC page.
import { beforeEach, expect, it, vi } from 'vitest'

import { replaceLocation } from '@/features/oidc/lib/location'
import { Route } from '@/routes/(auth)/sign-in'
import { useAuthStore } from '@/stores/auth-store'

vi.mock('@/features/oidc/lib/location', () => ({
  replaceLocation: vi.fn(),
  assignLocation: vi.fn(),
  openNewWindow: vi.fn(),
}))
vi.mock('@/features/auth/sign-in', () => ({ SignIn: () => null }))
vi.mock('@/lib/auth-session', () => ({
  resolveAuthentication: vi.fn(async () => ({ kind: 'authenticated' })),
}))

type BeforeLoad = (ctx: { search: { redirect?: string } }) => unknown

function runBeforeLoad(redirect: string): unknown {
  const beforeLoad = Route.options.beforeLoad as unknown as BeforeLoad
  return beforeLoad({ search: { redirect } })
}

beforeEach(() => {
  vi.mocked(replaceLocation).mockClear()
  useAuthStore.getState().auth.reset('complete')
  useAuthStore.getState().auth.setUser({ id: 3, username: 'u', role: 1 })
})

it('loads a query-bearing OIDC page as a document and never renders sign-in', async () => {
  const target = '/oauth2/authorize?client_id=openwebui&state=s'
  const pending = runBeforeLoad(target) as Promise<unknown>
  let settled = false
  void pending.then(
    () => (settled = true),
    () => (settled = true)
  )
  await vi.waitFor(() =>
    expect(replaceLocation).toHaveBeenCalledWith(
      `${window.location.origin}${target}`
    )
  )
  await new Promise((resolve) => setTimeout(resolve, 20))
  expect(settled).toBe(false)
})

it('keeps the router redirect for other targets', async () => {
  await expect(runBeforeLoad('/pricing?view=grid')).rejects.toMatchObject({
    options: { href: '/pricing?view=grid' },
  })
  expect(replaceLocation).not.toHaveBeenCalled()
})
