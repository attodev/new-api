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
// fork: O13 — Open WebUI relay settings: write-only secrets, client-side
// length rules, save order and translated server errors.
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
  type OpenWebUIRelayStatus,
} from '../openwebui-fork'
import { OpenWebUIRelaySection } from '../openwebui-relay-section'
import { getSiteSectionContent } from '../section-registry'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}))

const SECRET = 'x'.repeat(40)
let status: OpenWebUIRelayStatus
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

function renderSection(relayEnabled = false) {
  renderWithClient(
    <OpenWebUIRelaySection
      defaultValues={{
        'openwebui.relay_enabled': relayEnabled,
        'openwebui.jwt_issuer': 'open-webui',
        'openwebui.jwt_leeway_seconds': 60,
        'openwebui.allow_unsigned_requests': true,
      }}
    />
  )
}

function putKeys() {
  return vi
    .mocked(api.put)
    .mock.calls.map((call) => (call[1] as { key: string }).key)
}

const save = () =>
  userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))

beforeEach(() => {
  status = {
    relay_enabled: false,
    service_key_configured: false,
    jwt_secret_configured: false,
    jwt_previous_secret_configured: false,
    jwt_issuer: 'open-webui',
    jwt_leeway_seconds: 60,
    allow_unsigned_requests: true,
  }
  putResults = {}
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data:
      url === '/api/openwebui/relay-status'
        ? { success: true, data: status }
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

test('never prefills a secret, even if the options API returned one', async () => {
  status.service_key_configured = true
  status.jwt_secret_configured = true
  const leaked = {
    ...OPENWEBUI_SITE_DEFAULTS,
    'openwebui.service_api_key': 'leaked-service-key',
    'openwebui.jwt_secret': SECRET,
    'openwebui.jwt_previous_secret': SECRET,
  } as unknown as SiteSettings
  renderWithClient(getSiteSectionContent('openwebui-relay', leaked))

  for (const label of [/^Service key/, /^JWT secret/, /^Previous JWT secret/]) {
    expect(screen.getByLabelText(label)).toHaveValue('')
  }
  // Set / not set comes from relay-status, not from a value.
  expect(await screen.findAllByText('Configured')).toHaveLength(2)
  expect(screen.getByText('Not configured')).toBeInTheDocument()
})

test('secret inputs ask for new-password so the browser never autofills the login password', () => {
  renderSection()
  for (const label of [/^Service key/, /^JWT secret/, /^Previous JWT secret/]) {
    expect(screen.getByLabelText(label)).toHaveAttribute(
      'autocomplete',
      'new-password'
    )
  }
})

test('refuses a JWT secret shorter than 32 characters after trimming', async () => {
  renderSection()
  await userEvent.type(
    screen.getByLabelText(/^JWT secret/),
    `  ${'s'.repeat(31)}  `
  )
  await save()
  expect(
    await screen.findByText(
      'The Open WebUI JWT secret must be at least 32 characters'
    )
  ).toBeInTheDocument()
  expect(api.put).not.toHaveBeenCalled()
})

test('enabling needs a saved or entered service key and JWT secret', async () => {
  renderSection()
  await screen.findAllByText('Not configured')
  await userEvent.click(
    screen.getByRole('switch', { name: 'Enable Open WebUI relay' })
  )
  await save()
  expect(
    await screen.findByText(
      'Set the service key and a JWT secret of at least 32 characters before enabling the Open WebUI relay'
    )
  ).toBeInTheDocument()
  expect(api.put).not.toHaveBeenCalled()
})

test('saves the key and secret before turning the relay on, with trimmed values', async () => {
  renderSection()
  await screen.findAllByText('Not configured')
  await userEvent.click(
    screen.getByRole('switch', { name: 'Enable Open WebUI relay' })
  )
  await userEvent.type(screen.getByLabelText(/^Service key/), ' sk-abc ')
  await userEvent.type(screen.getByLabelText(/^JWT secret/), ` ${SECRET} `)
  await save()
  await waitFor(() =>
    expect(putKeys()).toEqual([
      'openwebui.service_api_key',
      'openwebui.jwt_secret',
      'openwebui.relay_enabled',
    ])
  )
  expect(api.put).toHaveBeenCalledWith('/api/option/', {
    key: 'openwebui.service_api_key',
    value: 'sk-abc',
  })
  expect(api.put).toHaveBeenCalledWith('/api/option/', {
    key: 'openwebui.jwt_secret',
    value: SECRET,
  })
  expect(api.put).toHaveBeenLastCalledWith('/api/option/', {
    key: 'openwebui.relay_enabled',
    value: true,
  })
  // Accepted secrets go back to "keep the current value".
  await waitFor(() =>
    expect(screen.getByLabelText(/^JWT secret/)).toHaveValue('')
  )
})

test('shows the translated server error and stops before enabling', async () => {
  const sentence = 'The Open WebUI service key does not match an enabled token'
  i18next.addResource('en', 'translation', sentence, 'translated: bad key')
  putResults['openwebui.service_api_key'] = {
    success: false,
    message: sentence,
    message_key: sentence,
  }
  renderSection()
  await screen.findAllByText('Not configured')
  await userEvent.click(
    screen.getByRole('switch', { name: 'Enable Open WebUI relay' })
  )
  await userEvent.type(screen.getByLabelText(/^Service key/), 'sk-dead')
  await userEvent.type(screen.getByLabelText(/^JWT secret/), SECRET)
  await save()
  await waitFor(() =>
    expect(toast.error).toHaveBeenCalledWith('translated: bad key')
  )
  expect(putKeys()).toEqual(['openwebui.service_api_key'])
  // The typed secret is kept so the admin can fix the key and retry.
  expect(screen.getByLabelText(/^JWT secret/)).toHaveValue(SECRET)
  i18next.addResource('en', 'translation', sentence, sentence)
})

test('an empty secret field is not a change', async () => {
  status.service_key_configured = true
  status.jwt_secret_configured = true
  renderSection(true)
  await userEvent.clear(screen.getByLabelText('JWT issuer'))
  await userEvent.type(screen.getByLabelText('JWT issuer'), 'my-webui')
  await save()
  await waitFor(() => expect(putKeys()).toEqual(['openwebui.jwt_issuer']))
})

test('clears the previous JWT secret after confirmation', async () => {
  status.service_key_configured = true
  status.jwt_secret_configured = true
  status.jwt_previous_secret_configured = true
  status.relay_enabled = true
  renderSection(true)
  await screen.findAllByText('Configured')

  // The relay is on, so the service key and current secret cannot be cleared.
  const clears = screen.getAllByRole('button', { name: 'Clear' })
  expect(clears).toHaveLength(3)
  expect(clears[0]).toBeDisabled()
  expect(clears[1]).toBeDisabled()
  expect(clears[2]).toBeEnabled()

  await userEvent.click(clears[2])
  expect(api.put).not.toHaveBeenCalled()
  await userEvent.click(
    within(await screen.findByRole('alertdialog')).getByRole('button', {
      name: 'Clear',
    })
  )
  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'openwebui.jwt_previous_secret',
      value: '',
    })
  )
})
