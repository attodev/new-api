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
// fork: O13 — /oauth2/logout page: session restore, confirmation, full sign-out.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { toast } from 'sonner'
import { beforeEach, expect, it, vi } from 'vitest'

import { logout } from '@/features/auth/api'
import {
  clearAuthenticatedClientState,
  resolveAuthentication,
} from '@/lib/auth-session'

import { logoutOidc } from '../api'
import { OidcLogoutPage } from '../components/oidc-logout-page'
import { replaceLocation } from '../lib/location'

const navigate = vi.fn()
const calls: string[] = []

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  useNavigate: () => navigate,
}))
vi.mock('@/features/auth/auth-layout', () => ({
  AuthLayout: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}))
vi.mock('@/features/auth/api', () => ({ logout: vi.fn() }))
vi.mock('@/hooks/use-system-config', () => ({
  useSystemConfig: () => ({ systemName: 'Acme Gateway' }),
}))
vi.mock('@/lib/auth-session', () => ({
  resolveAuthentication: vi.fn(),
  clearAuthenticatedClientState: vi.fn(),
}))
vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  logoutOidc: vi.fn(),
}))
vi.mock('../lib/location', () => ({
  replaceLocation: vi.fn(),
  assignLocation: vi.fn(),
  openNewWindow: vi.fn(),
}))
vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

const QUERY =
  '?id_token_hint=h.e.s&post_logout_redirect_uri=https%3A%2F%2Fchat.example.com%2Fauth&state=123'
const PARAMS = {
  id_token_hint: 'h.e.s',
  post_logout_redirect_uri: 'https://chat.example.com/auth',
  state: '123',
}
const LOGGED_OUT = {
  success: true,
  data: { redirect_url: 'https://chat.example.com/auth?state=123' },
}
const NEEDS_CONFIRMATION = {
  success: true,
  data: { redirect_url: null, needs_confirmation: true },
}

function renderPage() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <OidcLogoutPage />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  calls.length = 0
  window.history.replaceState({}, '', `/oauth2/logout${QUERY}`)
  vi.mocked(resolveAuthentication).mockImplementation(async () => {
    calls.push('restore')
    return { kind: 'anonymous' }
  })
  vi.mocked(logout).mockImplementation(async () => {
    calls.push('logout')
    return { success: true, message: '' }
  })
  vi.mocked(clearAuthenticatedClientState).mockImplementation(() => {
    calls.push('clear')
  })
  vi.mocked(replaceLocation).mockImplementation((url) => {
    calls.push(`replace ${url}`)
  })
})

it('restores the session, ends it, clears the refresh cookie, then follows the redirect', async () => {
  vi.mocked(logoutOidc).mockImplementation(async (params) => {
    calls.push(`oidc ${JSON.stringify(params)}`)
    return LOGGED_OUT
  })
  renderPage()

  await waitFor(() =>
    expect(calls).toEqual([
      'restore',
      `oidc ${JSON.stringify(PARAMS)}`,
      'logout',
      'clear',
      `replace ${LOGGED_OUT.data.redirect_url}`,
    ])
  )
  expect(logoutOidc).toHaveBeenCalledTimes(1)
})

it('still leaves when the normal sign-out request fails', async () => {
  vi.mocked(logoutOidc).mockResolvedValue(LOGGED_OUT)
  vi.mocked(logout).mockRejectedValue(new Error('409'))
  renderPage()

  await waitFor(() =>
    expect(replaceLocation).toHaveBeenCalledWith(LOGGED_OUT.data.redirect_url)
  )
  expect(clearAuthenticatedClientState).toHaveBeenCalled()
})

it('asks before signing out when the server needs confirmation', async () => {
  vi.mocked(logoutOidc)
    .mockResolvedValueOnce(NEEDS_CONFIRMATION)
    .mockResolvedValueOnce(LOGGED_OUT)
  renderPage()

  const signOut = await screen.findByRole('button', { name: 'Sign out' })
  expect(screen.getByText('Sign out of Acme Gateway?')).toBeInTheDocument()
  expect(logout).not.toHaveBeenCalled()
  expect(replaceLocation).not.toHaveBeenCalled()

  await userEvent.click(signOut)
  await waitFor(() =>
    expect(replaceLocation).toHaveBeenCalledWith(LOGGED_OUT.data.redirect_url)
  )
  expect(logoutOidc).toHaveBeenLastCalledWith({ ...PARAMS, confirmed: true })
  expect(logout).toHaveBeenCalledTimes(1)
})

it('stays signed in on request', async () => {
  vi.mocked(logoutOidc).mockResolvedValue(NEEDS_CONFIRMATION)
  renderPage()

  await userEvent.click(
    await screen.findByRole('button', { name: 'Stay signed in' })
  )
  expect(navigate).toHaveBeenCalledWith({ to: '/' })
  expect(logoutOidc).toHaveBeenCalledTimes(1)
  expect(logout).not.toHaveBeenCalled()
})

it('falls back to the confirm screen when the first request fails', async () => {
  vi.mocked(logoutOidc).mockRejectedValueOnce(new Error('network'))
  renderPage()

  expect(
    await screen.findByRole('button', { name: 'Sign out' })
  ).toBeInTheDocument()
  expect(logout).not.toHaveBeenCalled()
})

it('reports a failed confirmation and keeps the page', async () => {
  vi.mocked(logoutOidc)
    .mockResolvedValueOnce(NEEDS_CONFIRMATION)
    .mockRejectedValueOnce({
      response: {
        status: 403,
        data: { success: false, code: 'AUTH_ORIGIN_FORBIDDEN', message: 'x' },
      },
    })
  renderPage()

  await userEvent.click(await screen.findByRole('button', { name: 'Sign out' }))
  await waitFor(() => expect(toast.error).toHaveBeenCalled())
  expect(replaceLocation).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: 'Sign out' })).toBeEnabled()
})
