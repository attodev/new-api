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
// fork: O13 — chat app status flags and the playground route redirect (oauth2_* status fields).
import { redirect } from '@tanstack/react-router'

import { assignLocation, openNewWindow } from '@/features/oidc/lib/location'
import { readCachedStatus } from '@/lib/status-query'

export type ChatAppFlags = {
  /** The built-in OIDC provider for the external chat app is on. */
  enabled: boolean
  /** http(s) URL of the external chat app, or '' when unusable. */
  appUrl: string
  openInNewWindow: boolean
  replacePlayground: boolean
}

function httpUrl(value: unknown): string {
  if (typeof value !== 'string' || !value.trim()) return ''
  try {
    const url = new URL(value.trim())
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : ''
  } catch {
    return ''
  }
}

/** Read the chat app flags that /api/status carries (Task 3 status fields). */
export function readChatAppFlags(
  status: Record<string, unknown> | null | undefined
): ChatAppFlags {
  return {
    enabled: status?.oauth2_enabled === true,
    appUrl: httpUrl(status?.oauth2_app_url),
    openInNewWindow: status?.oauth2_open_in_new_window === true,
    replacePlayground: status?.oauth2_replace_playground === true,
  }
}

/**
 * Open the external chat app per the admin's preference: a new tab (needs a
 * direct user gesture, browsers block other popups) or this tab.
 */
export function openChatApp(appUrl: string, openInNewWindow: boolean): void {
  if (openInNewWindow) openNewWindow(appUrl)
  else assignLocation(appUrl)
}

/**
 * Playground route `beforeLoad` part of "Replace Playground". Clicking the
 * Playground nav item is a user gesture, so opening the app here (before the
 * route renders) is not blocked as a popup. Hover preloads run beforeLoad
 * too and must not open anything. Direct loads and refreshes have no gesture;
 * `ChatAppPlaygroundGate` covers them when the page renders.
 */
export async function replacePlaygroundBeforeLoad(
  preload: boolean
): Promise<void> {
  if (preload) return
  const flags = readChatAppFlags(readCachedStatus())
  if (!flags.enabled || !flags.replacePlayground || !flags.appUrl) return

  if (flags.openInNewWindow) {
    openNewWindow(flags.appUrl)
    throw redirect({ to: '/dashboard' })
  }
  assignLocation(flags.appUrl)
  // Hold the route while the browser leaves, so the playground never flashes.
  await new Promise<never>(() => {})
}
