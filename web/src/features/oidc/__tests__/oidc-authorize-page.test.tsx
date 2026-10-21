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
// fork: O13 — /oauth2/authorize page: sign-in hand-off, code redirect, errors.
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, expect, it, vi } from 'vitest'

import {
  clearAuthentication,
  refreshAuthentication,
  resolveAuthentication,
} from '@/lib/auth-session'

import { authorizeOidc } from '../api'
import { OidcAuthorizePage } from '../components/oidc-authorize-page'
import { replaceLocation } from '../lib/location'

const navigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  useNavigate: () => navigate,
}))
vi.mock('@/features/auth/auth-layout', () => ({
  AuthLayout: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}))
vi.mock('@/lib/auth-session', () => ({
  resolveAuthentication: vi.fn(),
  refreshAuthentication: vi.fn(),
  clearAuthentication: vi.fn(),
}))
vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  authorizeOidc: vi.fn(),
}))
vi.mock('../lib/location', () => ({
  replaceLocation: vi.fn(),
  assignLocation: vi.fn(),
  openNewWindow: vi.fn(),
}))

const QUERY =
  '?response_type=code&client_id=openwebui&redirect_uri=https%3A%2F%2Fchat.example.com%2Foauth%2Foidc%2Fcallback&scope=openid+email+profile&state=12345678901234567890123&nonce=n-1&code_challenge=c_h-a&code_challenge_method=S256'

beforeEach(() => {
  vi.clearAllMocks()
  window.history.replaceState({}, '', `/oauth2/authorize${QUERY}`)
  vi.mocked(resolveAuthentication).mockResolvedValue({
    kind: 'authenticated',
  } as Awaited<ReturnType<typeof resolveAuthentication>>)
})

it('sends a visitor without a session to sign-in and back to this exact URL', async () => {
  vi.mocked(resolveAuthentication).mockResolvedValue({ kind: 'anonymous' })
  render(<OidcAuthorizePage />)

  await waitFor(() =>
    expect(navigate).toHaveBeenCalledWith({
      to: '/sign-in',
      search: { redirect: `/oauth2/authorize${QUERY}` },
      replace: true,
    })
  )
  expect(authorizeOidc).not.toHaveBeenCalled()
})

it('posts the raw query as strings and follows the code redirect', async () => {
  const redirect = 'https://chat.example.com/oauth/oidc/callback?code=c&state=s'
  vi.mocked(authorizeOidc).mockResolvedValue({
    status: 200,
    body: { success: true, data: { redirect_url: redirect } },
  })
  render(<OidcAuthorizePage />)

  await waitFor(() => expect(replaceLocation).toHaveBeenCalledWith(redirect))
  expect(authorizeOidc).toHaveBeenCalledTimes(1)
  expect(authorizeOidc).toHaveBeenCalledWith({
    response_type: 'code',
    client_id: 'openwebui',
    redirect_uri: 'https://chat.example.com/oauth/oidc/callback',
    scope: 'openid email profile',
    state: '12345678901234567890123',
    nonce: 'n-1',
    code_challenge: 'c_h-a',
    code_challenge_method: 'S256',
  })
  expect(navigate).not.toHaveBeenCalled()
})

it('follows a redirectable OAuth error back to the client', async () => {
  const redirect =
    'https://chat.example.com/oauth/oidc/callback?error=access_denied&state=s'
  vi.mocked(authorizeOidc).mockResolvedValue({
    status: 200,
    body: {
      success: false,
      error: 'access_denied',
      data: { redirect_url: redirect },
    },
  })
  render(<OidcAuthorizePage />)

  await waitFor(() => expect(replaceLocation).toHaveBeenCalledWith(redirect))
})

it('refreshes an access token that expired before the POST and retries once', async () => {
  const redirect = 'https://chat.example.com/oauth/oidc/callback?code=c&state=s'
  vi.mocked(authorizeOidc)
    .mockResolvedValueOnce({ status: 401, body: null })
    .mockResolvedValueOnce({
      status: 200,
      body: { success: true, data: { redirect_url: redirect } },
    })
  vi.mocked(refreshAuthentication).mockResolvedValue({
    kind: 'authenticated',
  } as Awaited<ReturnType<typeof refreshAuthentication>>)
  render(<OidcAuthorizePage />)

  await waitFor(() => expect(replaceLocation).toHaveBeenCalledWith(redirect))
  expect(refreshAuthentication).toHaveBeenCalledTimes(1)
  expect(authorizeOidc).toHaveBeenCalledTimes(2)
  expect(vi.mocked(authorizeOidc).mock.calls[1]).toEqual(
    vi.mocked(authorizeOidc).mock.calls[0]
  )
  expect(clearAuthentication).not.toHaveBeenCalled()
  expect(navigate).not.toHaveBeenCalled()
})

it('signs in again when the refresh after a 401 fails', async () => {
  vi.mocked(authorizeOidc).mockResolvedValue({ status: 401, body: null })
  vi.mocked(refreshAuthentication).mockResolvedValue({ kind: 'anonymous' })
  render(<OidcAuthorizePage />)

  await waitFor(() =>
    expect(navigate).toHaveBeenCalledWith({
      to: '/sign-in',
      search: { redirect: `/oauth2/authorize${QUERY}` },
      replace: true,
    })
  )
  expect(authorizeOidc).toHaveBeenCalledTimes(1)
  expect(clearAuthentication).toHaveBeenCalledWith(false)
  expect(replaceLocation).not.toHaveBeenCalled()
})

it('keeps the session when the refresh after a 401 fails transiently', async () => {
  vi.mocked(authorizeOidc).mockResolvedValue({ status: 401, body: null })
  vi.mocked(refreshAuthentication).mockResolvedValue({
    kind: 'transient_error',
    error: new Error('offline'),
  })
  render(<OidcAuthorizePage />)

  expect(await screen.findByText('Request failed')).toBeInTheDocument()
  expect(clearAuthentication).not.toHaveBeenCalled()
  expect(navigate).not.toHaveBeenCalled()
})

it('drops a session the server still refuses after a refresh', async () => {
  vi.mocked(authorizeOidc).mockResolvedValue({ status: 401, body: null })
  vi.mocked(refreshAuthentication).mockResolvedValue({
    kind: 'authenticated',
  } as Awaited<ReturnType<typeof refreshAuthentication>>)
  render(<OidcAuthorizePage />)

  await waitFor(() =>
    expect(navigate).toHaveBeenCalledWith({
      to: '/sign-in',
      search: { redirect: `/oauth2/authorize${QUERY}` },
      replace: true,
    })
  )
  expect(authorizeOidc).toHaveBeenCalledTimes(2)
  expect(refreshAuthentication).toHaveBeenCalledTimes(1)
  expect(clearAuthentication).toHaveBeenCalledWith(false)
  expect(replaceLocation).not.toHaveBeenCalled()
})

it('explains email_required with the server sentence and links to the profile', async () => {
  const sentence =
    'Register an email address in your profile before signing in to the external app'
  vi.mocked(authorizeOidc).mockResolvedValue({
    status: 400,
    body: {
      success: false,
      error: 'email_required',
      message: sentence,
      message_key: sentence,
    },
  })
  render(<OidcAuthorizePage />)

  expect(await screen.findByText('Email address required')).toBeInTheDocument()
  expect(screen.getByText(sentence)).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Go to profile' }))
  expect(navigate).toHaveBeenCalledWith({ to: '/profile' })
  expect(replaceLocation).not.toHaveBeenCalled()
})

it('shows a non-redirectable error and never trusts redirect_uri', async () => {
  const sentence =
    'The sign-in request is not valid, please contact your administrator'
  vi.mocked(authorizeOidc).mockResolvedValue({
    status: 400,
    body: {
      success: false,
      error: 'invalid_client',
      message: sentence,
      message_key: sentence,
    },
  })
  render(<OidcAuthorizePage />)

  expect(
    await screen.findByText('Sign-in request rejected')
  ).toBeInTheDocument()
  expect(screen.getByText(sentence)).toBeInTheDocument()
  expect(replaceLocation).not.toHaveBeenCalled()
})

it('reports a failed request rather than an invalid sign-in request', async () => {
  vi.mocked(authorizeOidc).mockResolvedValue({ status: 0, body: null })
  render(<OidcAuthorizePage />)

  expect(await screen.findByText('Request failed')).toBeInTheDocument()
  expect(replaceLocation).not.toHaveBeenCalled()
  expect(navigate).not.toHaveBeenCalled()
})

it('reports a failed session check instead of asking for a password', async () => {
  vi.mocked(resolveAuthentication).mockResolvedValue({
    kind: 'transient_error',
    error: new Error('offline'),
  })
  render(<OidcAuthorizePage />)

  expect(await screen.findByText('Request failed')).toBeInTheDocument()
  expect(navigate).not.toHaveBeenCalled()
  expect(authorizeOidc).not.toHaveBeenCalled()
})
