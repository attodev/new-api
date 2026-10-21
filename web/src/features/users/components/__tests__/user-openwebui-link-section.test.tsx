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
// fork: O13 — Open WebUI link block in the user drawer: list, link with an
// admin.user.update proof, unlink with an admin.user.binding.clear proof.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { toast } from 'sonner'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { OpenWebUILink } from '../../openwebui-link-api-fork'
import { UserOpenWebUILinkSection } from '../user-openwebui-link-section'
import { UsersProvider } from '../users-provider'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}))

const USER_ID = 2
let relayEnabled: boolean
let link: OpenWebUILink | null
let verifyScope: string

const existingLink: OpenWebUILink = {
  id: 7,
  sub: 'owui-sub-123',
  user_id: USER_ID,
  email: 'user@example.com',
  linked_by: 'auto',
  created_at: 1_700_000_000,
  tokens: [
    { name: 'openwebui', exists: true, status: 1 },
    { name: 'openwebui-task', exists: false, status: 0 },
  ],
}

function proofResponse(token: string) {
  return {
    data: {
      success: true,
      data: {
        proof_token: token,
        method: '2fa',
        scope: verifyScope,
        expires_at: Math.floor(Date.now() / 1000) + 60,
      },
    },
  }
}

function renderSection() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <UsersProvider>
        <UserOpenWebUILinkSection userId={USER_ID} />
      </UsersProvider>
    </QueryClientProvider>
  )
}

async function completeTwoFactorVerification() {
  await userEvent.type(
    await screen.findByLabelText('Authenticator code or backup code'),
    '123456'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Verify' }))
}

beforeEach(() => {
  relayEnabled = true
  link = null
  verifyScope = ''
  vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    if (url === '/api/openwebui/relay-status') {
      return { data: { success: true, data: { relay_enabled: relayEnabled } } }
    }
    if (url === '/api/openwebui/links') {
      expect(config?.params).toEqual({ user_id: USER_ID })
      return { data: { success: true, data: link } }
    }
    if (url === '/api/verify/methods') {
      return {
        data: {
          success: true,
          data: {
            scope: verifyScope,
            methods: [{ method: '2fa', available: true }],
            oauth_providers: [],
            password_encryption_enabled: false,
          },
        },
      }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.mocked(toast.error).mockReset()
})

test('renders nothing while the relay is disabled', async () => {
  relayEnabled = false
  renderSection()
  await waitFor(() => expect(api.get).toHaveBeenCalled())
  expect(screen.queryByText('Open WebUI Link')).not.toBeInTheDocument()
  expect(api.get).not.toHaveBeenCalledWith(
    '/api/openwebui/links',
    expect.anything()
  )
})

test('shows the linked account and its tokens', async () => {
  link = existingLink
  renderSection()
  expect(await screen.findByDisplayValue('owui-sub-123')).toBeDisabled()
  expect(screen.getByText(/Automatic \(email\)/)).toBeInTheDocument()
  expect(screen.getByText('openwebui: Enabled')).toBeInTheDocument()
  expect(
    screen.getByText('openwebui-task: Not created yet')
  ).toBeInTheDocument()
})

test('linking sends the sub only with an admin.user.update proof for this user', async () => {
  verifyScope = 'admin.user.update'
  const post = vi.spyOn(api, 'post').mockImplementation(async (url) => {
    if (url === '/api/verify') return proofResponse('link-proof')
    if (url === '/api/openwebui/links') {
      return {
        data: { success: true, data: { ...existingLink, linked_by: 'admin' } },
      }
    }
    throw new Error(`Unexpected POST ${url}`)
  })
  renderSection()
  await userEvent.type(
    await screen.findByLabelText('Open WebUI user id (sub)'),
    '  owui-sub-123  '
  )
  await userEvent.click(screen.getByRole('button', { name: 'Link' }))
  expect(post).not.toHaveBeenCalledWith(
    '/api/openwebui/links',
    expect.anything(),
    expect.anything()
  )
  await completeTwoFactorVerification()
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/openwebui/links',
      { user_id: USER_ID, sub: 'owui-sub-123' },
      expect.objectContaining({
        headers: { 'X-Security-Proof': 'link-proof' },
        singleUseAuthorization: true,
      })
    )
  )
  expect(post).toHaveBeenCalledWith(
    '/api/verify',
    expect.objectContaining({
      scope: 'admin.user.update',
      context: { user_id: USER_ID },
    }),
    expect.anything()
  )
  await waitFor(() =>
    expect(toast.success).toHaveBeenCalledWith('Open WebUI account linked')
  )
})

test('a link conflict shows the translated server message', async () => {
  const sentence = 'This Open WebUI account or new-api user is already linked'
  i18next.addResource('en', 'translation', sentence, 'translated: conflict')
  verifyScope = 'admin.user.update'
  vi.spyOn(api, 'post').mockImplementation(async (url) => {
    if (url === '/api/verify') return proofResponse('link-proof')
    return {
      data: { success: false, message: sentence, message_key: sentence },
    }
  })
  renderSection()
  await userEvent.type(
    await screen.findByLabelText('Open WebUI user id (sub)'),
    'owui-sub-123'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Link' }))
  await completeTwoFactorVerification()
  await waitFor(() =>
    expect(toast.error).toHaveBeenCalledWith('translated: conflict')
  )
  // The typed sub stays so the admin can correct it.
  expect(screen.getByLabelText('Open WebUI user id (sub)')).toHaveValue(
    'owui-sub-123'
  )
  i18next.addResource('en', 'translation', sentence, sentence)
})

test('unlinking needs confirmation and an admin.user.binding.clear proof', async () => {
  link = existingLink
  verifyScope = 'admin.user.binding.clear'
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue(proofResponse('unlink-proof'))
  const del = vi
    .spyOn(api, 'delete')
    .mockResolvedValue({ data: { success: true, data: null } })
  renderSection()
  await screen.findByDisplayValue('owui-sub-123')
  await userEvent.click(screen.getByRole('button', { name: 'Unlink' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(post).not.toHaveBeenCalled()
  await userEvent.click(within(dialog).getByRole('button', { name: 'Unlink' }))
  await completeTwoFactorVerification()
  await waitFor(() =>
    expect(del).toHaveBeenCalledWith(
      '/api/openwebui/links/7',
      expect.objectContaining({
        headers: { 'X-Security-Proof': 'unlink-proof' },
        singleUseAuthorization: true,
      })
    )
  )
  expect(post).toHaveBeenCalledWith(
    '/api/verify',
    expect.objectContaining({
      scope: 'admin.user.binding.clear',
      context: { user_id: USER_ID, binding_type: 'openwebui' },
    }),
    expect.anything()
  )
})

test('cancelling verification does not unlink', async () => {
  link = existingLink
  verifyScope = 'admin.user.binding.clear'
  vi.spyOn(api, 'post').mockResolvedValue(proofResponse('unlink-proof'))
  const del = vi.spyOn(api, 'delete')
  renderSection()
  await screen.findByDisplayValue('owui-sub-123')
  await userEvent.click(screen.getByRole('button', { name: 'Unlink' }))
  await userEvent.click(
    within(await screen.findByRole('alertdialog')).getByRole('button', {
      name: 'Unlink',
    })
  )
  await screen.findByLabelText('Authenticator code or backup code')
  await userEvent.keyboard('{Escape}')
  await waitFor(() =>
    expect(
      screen.queryByLabelText('Authenticator code or backup code')
    ).not.toBeInTheDocument()
  )
  expect(del).not.toHaveBeenCalled()
})
