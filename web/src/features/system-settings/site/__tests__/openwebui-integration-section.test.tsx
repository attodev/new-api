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
// fork: O13 — built-in OIDC provider settings: write-only client secret,
// enable rules, save order, translated server errors, key rotation and the
// API key token (token-from-key) settings.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import type { SiteSettings } from '../../types'
import {
  OPENWEBUI_SITE_DEFAULTS,
  type OpenWebUIOAuth2Defaults,
} from '../openwebui-fork'
import { OpenWebUIOAuth2Section } from '../openwebui-integration-section'
import { getSiteSectionContent } from '../section-registry'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}))

const CLIENT_SECRET = 'c'.repeat(24)
let putResults: Record<
  string,
  { success: boolean; message?: string; message_key?: string }
>

function Frame({ children }: { children: ReactNode }) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        {children}
      </SettingsPageProvider>
    </>
  )
}

function renderWithClient(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Frame>{node}</Frame>
    </QueryClientProvider>
  )
}

const defaults: OpenWebUIOAuth2Defaults = {
  'oauth2.enabled': false,
  'oauth2.client_id': '',
  'oauth2.redirect_uri': '',
  'oauth2.app_url': '',
  'oauth2.post_logout_redirect_uri': '',
  'oauth2.open_in_new_window': false,
  'oauth2.replace_playground': false,
  'oauth2.token_from_key_enabled': false,
  'oauth2.token_from_key_client_id': 'openwebui-mcp',
  'oauth2.token_from_key_ttl_seconds': 600,
  'oauth2.token_from_key_rate_limit': 30,
}

function renderSection(values: Partial<OpenWebUIOAuth2Defaults> = {}) {
  renderWithClient(
    <OpenWebUIOAuth2Section defaultValues={{ ...defaults, ...values }} />
  )
}

function putKeys() {
  return vi
    .mocked(api.put)
    .mock.calls.map((call) => (call[1] as { key: string }).key)
}

const save = () =>
  userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
const enableSwitch = () =>
  screen.getByRole('switch', { name: 'Enable External App Access' })

beforeEach(() => {
  putResults = {}
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data:
      url === '/api/oauth2/signing-key-status'
        ? {
            success: true,
            data: {
              current: { kid: 'kid-current', created_at: 1_700_000_000 },
              previous: { kid: 'kid-old', created_at: 1_690_000_000 },
            },
          }
        : { success: true, data: [] },
  }))
  vi.spyOn(api, 'put').mockImplementation(async (_url, body) => ({
    data: putResults[(body as { key: string }).key] ?? { success: true },
  }))
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.mocked(toast.error).mockReset()
})

test('never prefills the client secret, even if the options API returned one', () => {
  const leaked = {
    ...OPENWEBUI_SITE_DEFAULTS,
    'oauth2.client_id': 'open-webui',
    'oauth2.client_secret': CLIENT_SECRET,
  } as unknown as SiteSettings
  renderWithClient(getSiteSectionContent('openwebui-integration', leaked))
  expect(screen.getByLabelText('Client ID')).toHaveValue('open-webui')
  expect(screen.getByLabelText('Client Secret')).toHaveValue('')
})

test('enabling needs a client ID and an absolute redirect URI', async () => {
  renderSection()
  await userEvent.click(enableSwitch())
  await save()
  expect(
    await screen.findByText(
      'Set a client ID, a client secret of at least 16 characters and an absolute redirect URI before enabling external app access'
    )
  ).toBeInTheDocument()
  expect(api.put).not.toHaveBeenCalled()
})

test('refuses a short client secret and a relative redirect URI', async () => {
  renderSection()
  await userEvent.type(screen.getByLabelText('Client Secret'), ' short-secret ')
  await userEvent.type(screen.getByLabelText('Redirect URI'), '/callback')
  await save()
  expect(
    await screen.findByText('The client secret must be at least 16 characters')
  ).toBeInTheDocument()
  expect(
    screen.getByText('Must be an absolute http(s) URL')
  ).toBeInTheDocument()
  expect(api.put).not.toHaveBeenCalled()
})

test('saves the client settings before turning access on', async () => {
  renderSection()
  await userEvent.click(enableSwitch())
  await userEvent.type(screen.getByLabelText('Client ID'), 'open-webui')
  await userEvent.type(screen.getByLabelText('Client Secret'), CLIENT_SECRET)
  await userEvent.type(
    screen.getByLabelText('Redirect URI'),
    'https://chat.example.com/oauth/oidc/login/callback'
  )
  await save()
  await waitFor(() =>
    expect(putKeys()).toEqual([
      'oauth2.client_id',
      'oauth2.redirect_uri',
      'oauth2.client_secret',
      'oauth2.enabled',
    ])
  )
  expect(api.put).toHaveBeenCalledWith('/api/option/', {
    key: 'oauth2.client_secret',
    value: CLIENT_SECRET,
  })
  await waitFor(() =>
    expect(screen.getByLabelText('Client Secret')).toHaveValue('')
  )
})

test('turning access off is saved before other changes', async () => {
  renderSection({
    'oauth2.enabled': true,
    'oauth2.client_id': 'open-webui',
    'oauth2.redirect_uri': 'https://chat.example.com/cb',
  })
  await userEvent.click(enableSwitch())
  await userEvent.clear(screen.getByLabelText('Client ID'))
  await userEvent.type(screen.getByLabelText('Client ID'), 'other')
  await save()
  await waitFor(() =>
    expect(putKeys()).toEqual(['oauth2.enabled', 'oauth2.client_id'])
  )
})

test('a successful save refreshes the cached status for the chat app entry points', async () => {
  const invalidate = vi.spyOn(QueryClient.prototype, 'invalidateQueries')
  window.localStorage.setItem(
    'status',
    JSON.stringify({ oauth2_enabled: true })
  )
  renderSection({
    'oauth2.enabled': true,
    'oauth2.client_id': 'open-webui',
    'oauth2.redirect_uri': 'https://chat.example.com/cb',
  })
  await userEvent.type(
    screen.getByLabelText('App URL'),
    'https://chat.example.com'
  )
  await save()
  await waitFor(() => expect(putKeys()).toEqual(['oauth2.app_url']))
  await waitFor(() =>
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['status'] })
  )
  expect(window.localStorage.getItem('status')).toBeNull()
})

test('a refused save keeps the cached status', async () => {
  const invalidate = vi.spyOn(QueryClient.prototype, 'invalidateQueries')
  window.localStorage.setItem(
    'status',
    JSON.stringify({ oauth2_enabled: false })
  )
  putResults['oauth2.enabled'] = { success: false, message: 'refused' }
  renderSection({
    'oauth2.client_id': 'open-webui',
    'oauth2.redirect_uri': 'https://chat.example.com/cb',
  })
  await userEvent.click(enableSwitch())
  await save()
  await waitFor(() => expect(toast.error).toHaveBeenCalled())
  expect(invalidate).not.toHaveBeenCalledWith({ queryKey: ['status'] })
  expect(window.localStorage.getItem('status')).not.toBeNull()
  window.localStorage.removeItem('status')
})

test('shows the translated server error when the stored secret is missing', async () => {
  const sentence =
    'Set a client ID, a client secret of at least 16 characters and an absolute redirect URI before enabling external app access'
  i18next.addResource('en', 'translation', sentence, 'translated: not ready')
  putResults['oauth2.enabled'] = {
    success: false,
    message: sentence,
    message_key: sentence,
  }
  renderSection({
    'oauth2.client_id': 'open-webui',
    'oauth2.redirect_uri': 'https://chat.example.com/cb',
  })
  await userEvent.click(enableSwitch())
  await save()
  await waitFor(() =>
    expect(toast.error).toHaveBeenCalledWith('translated: not ready')
  )
  expect(putKeys()).toEqual(['oauth2.enabled'])
  i18next.addResource('en', 'translation', sentence, sentence)
})

test('rotates the signing key only after confirmation', async () => {
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: {
        current: { kid: 'kid-new', created_at: 1_710_000_000 },
        previous: { kid: 'kid-current', created_at: 1_700_000_000 },
      },
    },
  })
  renderSection()
  expect(await screen.findByText(/kid-current/)).toBeInTheDocument()
  expect(screen.getByText(/kid-old/)).toBeInTheDocument()

  await userEvent.click(
    screen.getByRole('button', { name: 'Rotate signing key' })
  )
  const dialog = await screen.findByRole('alertdialog')
  expect(post).not.toHaveBeenCalled()
  await userEvent.click(within(dialog).getByRole('button', { name: 'Rotate' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/oauth2/rotate-signing-key')
  )
})

test('cancelling the rotation dialog does not rotate', async () => {
  const post = vi.spyOn(api, 'post')
  renderSection()
  await screen.findByText(/kid-current/)
  await userEvent.click(
    screen.getByRole('button', { name: 'Rotate signing key' })
  )
  const dialog = await screen.findByRole('alertdialog')
  await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  expect(post).not.toHaveBeenCalled()
})

const tokenSwitch = () =>
  screen.getByRole('switch', { name: 'Issue Tokens from API Keys' })

test('shows the API key token settings from the server defaults', () => {
  renderWithClient(
    getSiteSectionContent('openwebui-integration', {
      ...OPENWEBUI_SITE_DEFAULTS,
      'oauth2.token_from_key_client_id': 'mcp-from-server',
      'oauth2.token_from_key_ttl_seconds': 300,
      'oauth2.token_from_key_rate_limit': 12,
    } as SiteSettings)
  )
  expect(tokenSwitch()).not.toBeChecked()
  expect(screen.getByLabelText('API Key Token Client ID')).toHaveValue(
    'mcp-from-server'
  )
  expect(screen.getByLabelText('API Key Token Lifetime (seconds)')).toHaveValue(
    300
  )
  expect(
    screen.getByLabelText('API Key Token Requests per Minute')
  ).toHaveValue(12)
})

test('the site defaults match the server defaults', () => {
  expect(OPENWEBUI_SITE_DEFAULTS).toMatchObject({
    'oauth2.token_from_key_enabled': false,
    'oauth2.token_from_key_client_id': 'openwebui-mcp',
    'oauth2.token_from_key_ttl_seconds': 600,
    'oauth2.token_from_key_rate_limit': 30,
  })
})

test('refuses out-of-range API key token lifetime and rate', async () => {
  renderSection()
  const ttl = screen.getByLabelText('API Key Token Lifetime (seconds)')
  await userEvent.clear(ttl)
  await userEvent.type(ttl, '30')
  const rate = screen.getByLabelText('API Key Token Requests per Minute')
  await userEvent.clear(rate)
  await userEvent.type(rate, '601')
  await save()
  expect(
    await screen.findByText(
      'The API key token lifetime must be between 60 and 900 seconds'
    )
  ).toBeInTheDocument()
  expect(
    screen.getByText(
      'The API key token rate limit must be between 1 and 600 per minute'
    )
  ).toBeInTheDocument()
  expect(api.put).not.toHaveBeenCalled()
})

test('the API key token client ID must be set and differ from the client ID', async () => {
  const sentence =
    'The API key token client ID must not be empty and must differ from the Open WebUI client ID'
  renderSection({ 'oauth2.client_id': 'open-webui' })
  const mcpClientId = screen.getByLabelText('API Key Token Client ID')
  await userEvent.clear(mcpClientId)
  await save()
  expect(await screen.findByText(sentence)).toBeInTheDocument()
  await userEvent.type(mcpClientId, ' open-webui ')
  await save()
  expect(await screen.findByText(sentence)).toBeInTheDocument()
  expect(api.put).not.toHaveBeenCalled()
})

test('turning the API key token feature on saves its settings first', async () => {
  renderSection({
    'oauth2.enabled': true,
    'oauth2.client_id': 'open-webui',
    'oauth2.redirect_uri': 'https://chat.example.com/cb',
  })
  await userEvent.click(tokenSwitch())
  const mcpClientId = screen.getByLabelText('API Key Token Client ID')
  await userEvent.clear(mcpClientId)
  await userEvent.type(mcpClientId, ' mcp ')
  const ttl = screen.getByLabelText('API Key Token Lifetime (seconds)')
  await userEvent.clear(ttl)
  await userEvent.type(ttl, '120')
  await save()
  await waitFor(() =>
    expect(putKeys()).toEqual([
      'oauth2.token_from_key_client_id',
      'oauth2.token_from_key_ttl_seconds',
      'oauth2.token_from_key_enabled',
    ])
  )
  expect(api.put).toHaveBeenCalledWith('/api/option/', {
    key: 'oauth2.token_from_key_client_id',
    value: 'mcp',
  })
  expect(api.put).toHaveBeenCalledWith('/api/option/', {
    key: 'oauth2.token_from_key_ttl_seconds',
    value: 120,
  })
})

test('turning the API key token feature off is saved first', async () => {
  renderSection({
    'oauth2.enabled': true,
    'oauth2.client_id': 'open-webui',
    'oauth2.redirect_uri': 'https://chat.example.com/cb',
    'oauth2.token_from_key_enabled': true,
  })
  await userEvent.click(tokenSwitch())
  const rate = screen.getByLabelText('API Key Token Requests per Minute')
  await userEvent.clear(rate)
  await userEvent.type(rate, '5')
  await save()
  await waitFor(() =>
    expect(putKeys()).toEqual([
      'oauth2.token_from_key_enabled',
      'oauth2.token_from_key_rate_limit',
    ])
  )
})

test('when both client IDs change, the API key token client ID is saved first', async () => {
  // Server rule: each save is checked against the other, stored client ID.
  // Moving client_id onto the old API key token client ID only works after
  // the latter has moved away.
  renderSection({
    'oauth2.client_id': 'open-webui',
    'oauth2.token_from_key_client_id': 'mcp',
  })
  const clientId = screen.getByLabelText('Client ID')
  await userEvent.clear(clientId)
  await userEvent.type(clientId, 'mcp')
  const mcpClientId = screen.getByLabelText('API Key Token Client ID')
  await userEvent.clear(mcpClientId)
  await userEvent.type(mcpClientId, 'mcp-2')
  await save()
  await waitFor(() =>
    expect(putKeys()).toEqual([
      'oauth2.token_from_key_client_id',
      'oauth2.client_id',
    ])
  )
})

test('warns that any API key becomes an Open WebUI sign-in', () => {
  renderSection()
  expect(
    screen.getByText(
      /any API key works as an Open WebUI sign-in for its owner, admin keys included/
    )
  ).toBeInTheDocument()
})
