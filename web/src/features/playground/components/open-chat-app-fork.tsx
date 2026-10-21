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
// fork: O13 — playground "Open in Chat App" entry and "Replace Playground" (oauth2_* status flags).
import { ExternalLinkIcon } from 'lucide-react'
import { useEffect, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { assignLocation, openNewWindow } from '@/features/oidc/lib/location'
import { useStatus } from '@/hooks/use-status'

import { openChatApp, readChatAppFlags } from '../lib/chat-app-fork'

function GateMessage(props: { children: ReactNode }) {
  return (
    <div className='flex size-full flex-col items-center justify-center gap-4'>
      {props.children}
    </div>
  )
}

/**
 * Shown instead of the built-in playground when "Replace Playground" is on.
 * Same-window mode leaves on mount (a plain navigation is never blocked).
 * New-window mode cannot open a popup without a click, so it shows a button.
 */
export function ChatAppPlaygroundGate(props: { children: ReactNode }) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const flags = readChatAppFlags(status)
  const replaced = flags.enabled && flags.replacePlayground
  const leaveNow = replaced && !flags.openInNewWindow && flags.appUrl

  useEffect(() => {
    if (leaveNow) assignLocation(leaveNow)
  }, [leaveNow])

  if (!replaced) return props.children

  if (!flags.appUrl) {
    return (
      <GateMessage>
        <p className='text-muted-foreground text-sm'>
          {t('External chat app is not configured')}
        </p>
      </GateMessage>
    )
  }

  if (flags.openInNewWindow) {
    return (
      <GateMessage>
        <p className='text-muted-foreground text-sm'>
          {t('Playground has been replaced by the external chat app')}
        </p>
        <Button onClick={() => openNewWindow(flags.appUrl)}>
          <ExternalLinkIcon className='mr-1 size-4' />
          {t('Open Chat App')}
        </Button>
      </GateMessage>
    )
  }

  return (
    <GateMessage>
      <p className='text-muted-foreground text-sm'>
        {t('Redirecting to the external chat app...')}
      </p>
    </GateMessage>
  )
}

/** "Open in Chat App" row above the playground chat, when the app is set up. */
export function OpenInChatAppButton() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const flags = readChatAppFlags(status)
  if (!flags.enabled || !flags.appUrl) return null

  return (
    <div className='flex items-center justify-end px-4 pt-2'>
      <Button
        variant='ghost'
        size='sm'
        onClick={() => openChatApp(flags.appUrl, flags.openInNewWindow)}
      >
        <ExternalLinkIcon className='mr-1 size-4' />
        {t('Open in Chat App')}
      </Button>
    </div>
  )
}
