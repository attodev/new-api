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
// fork: O13 — after sign-in, an OIDC page target is loaded as a document.
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'

import { replaceLocation } from '@/features/oidc/lib/location'
import { useAuthStore, type AuthBundle } from '@/stores/auth-store'

import { useAuthRedirect } from '../use-auth-redirect'

vi.mock('@/features/oidc/lib/location', () => ({
  replaceLocation: vi.fn(),
  assignLocation: vi.fn(),
  openNewWindow: vi.fn(),
}))

const bundle: AuthBundle = {
  access_token: 'fresh-login',
  token_type: 'Bearer',
  access_expires_at: 9999999999,
  user: { id: 7, username: 'owui-user', role: 1 },
  session: {
    sid: 'fresh-session',
    current: true,
    login_method: 'password',
    ip: '',
    user_agent: '',
    created_at: 1,
    last_active_at: 1,
    expires_at: 9999999999,
  },
}

function Harness(props: { target: string }) {
  const { handleLoginResult } = useAuthRedirect()
  return (
    <button
      type='button'
      onClick={() => void handleLoginResult(bundle, props.target)}
    >
      Signed in
    </button>
  )
}

function renderHarness(target: string) {
  useAuthStore.getState().auth.reset('complete')
  const root = createRootRoute({ component: Outlet })
  const router = createRouter({
    routeTree: root.addChildren([
      createRoute({
        getParentRoute: () => root,
        path: '/sign-in',
        component: () => <Harness target={target} />,
      }),
      createRoute({
        getParentRoute: () => root,
        path: '/pricing',
        component: () => <div>Pricing destination</div>,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ['/sign-in'] }),
  })
  render(<RouterProvider router={router} />)
  return router
}

beforeEach(() => {
  vi.mocked(replaceLocation).mockClear()
})

it('loads the OIDC authorize page with its exact query after sign-in', async () => {
  const target =
    '/oauth2/authorize?response_type=code&client_id=openwebui&state=s1&code_challenge=abc&code_challenge_method=S256'
  const router = renderHarness(target)
  await userEvent.click(
    await screen.findByRole('button', { name: 'Signed in' })
  )

  await waitFor(() =>
    expect(replaceLocation).toHaveBeenCalledWith(
      `${window.location.origin}${target}`
    )
  )
  expect(router.state.location.pathname).toBe('/sign-in')
  expect(useAuthStore.getState().auth.user?.id).toBe(7)
})

it('keeps router navigation for every other target', async () => {
  const router = renderHarness('/pricing?view=grid')
  await userEvent.click(
    await screen.findByRole('button', { name: 'Signed in' })
  )

  await waitFor(() =>
    expect(router.state.location.href).toBe('/pricing?view=grid')
  )
  expect(replaceLocation).not.toHaveBeenCalled()
})
