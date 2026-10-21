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
// fork: O13 — playground "Open in Chat App" entry driven by the oauth2_* status flags.
import { isRedirect } from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  assignLocation,
  openNewWindow,
  replaceLocation,
} from '@/features/oidc/lib/location'
import { useStatus } from '@/hooks/use-status'
import { readCachedStatus } from '@/lib/status-query'

import {
  readChatAppFlags,
  replacePlaygroundBeforeLoad,
} from '../../lib/chat-app-fork'
import {
  ChatAppPlaygroundGate,
  OpenInChatAppButton,
} from '../open-chat-app-fork'

vi.mock('@/features/oidc/lib/location', () => ({
  replaceLocation: vi.fn(),
  assignLocation: vi.fn(),
  openNewWindow: vi.fn(),
}))
vi.mock('@/hooks/use-status', () => ({ useStatus: vi.fn() }))
vi.mock('@/lib/status-query', () => ({ readCachedStatus: vi.fn() }))

const APP = 'https://chat.example.com/'

function withStatus(status: Record<string, unknown> | null) {
  vi.mocked(useStatus).mockReturnValue({
    status,
    loading: false,
    error: null,
  } as ReturnType<typeof useStatus>)
  vi.mocked(readCachedStatus).mockReturnValue(status)
}

function flags(extra: Record<string, unknown> = {}) {
  return {
    oauth2_enabled: true,
    oauth2_app_url: APP,
    oauth2_open_in_new_window: false,
    oauth2_replace_playground: false,
    ...extra,
  }
}

const playground = <div>Built-in playground</div>

beforeEach(() => {
  vi.clearAllMocks()
})

describe('readChatAppFlags', () => {
  it('reads the status flags and accepts only http(s) app URLs', () => {
    expect(
      readChatAppFlags(flags({ oauth2_replace_playground: true }))
    ).toEqual({
      enabled: true,
      appUrl: APP,
      openInNewWindow: false,
      replacePlayground: true,
    })
    expect(
      readChatAppFlags(flags({ oauth2_app_url: 'javascript:alert(1)' }))
    ).toMatchObject({ appUrl: '' })
    expect(readChatAppFlags(null)).toEqual({
      enabled: false,
      appUrl: '',
      openInNewWindow: false,
      replacePlayground: false,
    })
  })
})

describe('ChatAppPlaygroundGate', () => {
  it('renders the playground when replacement is off', () => {
    withStatus(flags())
    render(<ChatAppPlaygroundGate>{playground}</ChatAppPlaygroundGate>)
    expect(screen.getByText('Built-in playground')).toBeInTheDocument()
    expect(assignLocation).not.toHaveBeenCalled()
  })

  it('renders the playground when the provider is disabled', () => {
    withStatus(
      flags({ oauth2_enabled: false, oauth2_replace_playground: true })
    )
    render(<ChatAppPlaygroundGate>{playground}</ChatAppPlaygroundGate>)
    expect(screen.getByText('Built-in playground')).toBeInTheDocument()
  })

  it('replace + same window: goes to the app without rendering the playground', () => {
    withStatus(flags({ oauth2_replace_playground: true }))
    render(<ChatAppPlaygroundGate>{playground}</ChatAppPlaygroundGate>)
    expect(assignLocation).toHaveBeenCalledWith(APP)
    expect(screen.queryByText('Built-in playground')).not.toBeInTheDocument()
    expect(
      screen.getByText('Redirecting to the external chat app...')
    ).toBeInTheDocument()
  })

  it('replace + new window: offers a button that opens the app URL', async () => {
    withStatus(
      flags({
        oauth2_replace_playground: true,
        oauth2_open_in_new_window: true,
      })
    )
    render(<ChatAppPlaygroundGate>{playground}</ChatAppPlaygroundGate>)
    expect(screen.queryByText('Built-in playground')).not.toBeInTheDocument()
    expect(openNewWindow).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Open Chat App' }))
    expect(openNewWindow).toHaveBeenCalledWith(APP)
    expect(assignLocation).not.toHaveBeenCalled()
  })

  it('replace without an app URL: says so instead of the playground', () => {
    withStatus(flags({ oauth2_replace_playground: true, oauth2_app_url: '' }))
    render(<ChatAppPlaygroundGate>{playground}</ChatAppPlaygroundGate>)
    expect(
      screen.getByText('External chat app is not configured')
    ).toBeInTheDocument()
    expect(screen.queryByText('Built-in playground')).not.toBeInTheDocument()
    expect(assignLocation).not.toHaveBeenCalled()
  })
})

describe('OpenInChatAppButton', () => {
  it('opens the app in this tab by default', async () => {
    withStatus(flags())
    render(<OpenInChatAppButton />)
    await userEvent.click(
      screen.getByRole('button', { name: 'Open in Chat App' })
    )
    expect(assignLocation).toHaveBeenCalledWith(APP)
    expect(openNewWindow).not.toHaveBeenCalled()
  })

  it('opens the app in a new window when configured', async () => {
    withStatus(flags({ oauth2_open_in_new_window: true }))
    render(<OpenInChatAppButton />)
    await userEvent.click(
      screen.getByRole('button', { name: 'Open in Chat App' })
    )
    expect(openNewWindow).toHaveBeenCalledWith(APP)
    expect(assignLocation).not.toHaveBeenCalled()
  })

  it('is hidden when the provider is off or has no app URL', () => {
    withStatus(flags({ oauth2_enabled: false }))
    const { container, rerender } = render(<OpenInChatAppButton />)
    expect(container).toBeEmptyDOMElement()

    withStatus(flags({ oauth2_app_url: '' }))
    rerender(<OpenInChatAppButton />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe('replacePlaygroundBeforeLoad', () => {
  it('does nothing on hover preload or when replacement is off', async () => {
    withStatus(flags({ oauth2_replace_playground: true }))
    await expect(replacePlaygroundBeforeLoad(true)).resolves.toBeUndefined()

    withStatus(flags())
    await expect(replacePlaygroundBeforeLoad(false)).resolves.toBeUndefined()
    withStatus(null)
    await expect(replacePlaygroundBeforeLoad(false)).resolves.toBeUndefined()
    expect(assignLocation).not.toHaveBeenCalled()
    expect(openNewWindow).not.toHaveBeenCalled()
  })

  it('new window: opens the app URL during the click and goes to the dashboard', async () => {
    withStatus(
      flags({
        oauth2_replace_playground: true,
        oauth2_open_in_new_window: true,
      })
    )
    const error = await replacePlaygroundBeforeLoad(false).catch(
      (thrown: unknown) => thrown
    )
    expect(openNewWindow).toHaveBeenCalledWith(APP)
    expect(isRedirect(error)).toBe(true)
    expect(error).toMatchObject({ options: { to: '/dashboard' } })
  })

  it('same window: navigates to the app and never renders the playground', async () => {
    withStatus(flags({ oauth2_replace_playground: true }))
    let settled = false
    void replacePlaygroundBeforeLoad(false).then(
      () => (settled = true),
      () => (settled = true)
    )
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(assignLocation).toHaveBeenCalledWith(APP)
    expect(settled).toBe(false)
    expect(replaceLocation).not.toHaveBeenCalled()
  })
})
