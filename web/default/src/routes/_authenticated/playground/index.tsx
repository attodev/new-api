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
import { createFileRoute, redirect } from '@tanstack/react-router'
import { getCachedStatus, isSidebarModuleEnabled } from '@/lib/nav-modules'
import { Main } from '@/components/layout'
import { Playground } from '@/features/playground'
import { getOpenInChatAppUrl } from '@/features/playground/api'

export const Route = createFileRoute('/_authenticated/playground/')({
  beforeLoad: async ({ preload }) => {
    if (!isSidebarModuleEnabled('chat', 'playground')) {
      throw redirect({ to: '/dashboard' })
    }

    // The router preloads on hover (defaultPreload: 'intent' in main.tsx),
    // which runs beforeLoad too — without this check, just hovering the
    // Playground nav item would open a new window / navigate away.
    // Only run the redirect below on an actual navigation.
    if (preload) return

    // "Replace Playground": clicking the Playground nav item is a genuine
    // user gesture, so handling the redirect right here (rather than after
    // the route finishes loading and the component mounts) means a
    // new-window open still counts as gesture-triggered and isn't blocked
    // by the browser — no intermediate button needed for the normal
    // click-to-navigate path. Direct URL loads/refreshes have no such
    // gesture; Playground's own component still falls back to a
    // click-to-open button for that case.
    const status = getCachedStatus()
    const oauth2Enabled = Boolean(status?.oauth2_enabled)
    const replacePlayground = Boolean(status?.oauth2_replace_playground)
    if (!oauth2Enabled || !replacePlayground) return

    let redirectUrl: string
    try {
      redirectUrl = await getOpenInChatAppUrl()
    } catch {
      return
    }

    const openInNewWindow = Boolean(status?.oauth2_open_in_new_window)
    if (openInNewWindow) {
      window.open(redirectUrl, '_blank', 'noopener,noreferrer')
      throw redirect({ to: '/dashboard' })
    }
    window.location.href = redirectUrl
    // Block rendering while the browser navigates away, so the old
    // playground UI never flashes on screen in the meantime.
    return new Promise<never>(() => {})
  },
  component: PlaygroundPage,
})

function PlaygroundPage() {
  return (
    <Main className='p-0'>
      <Playground />
    </Main>
  )
}
