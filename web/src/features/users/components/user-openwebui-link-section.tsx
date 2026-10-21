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
// fork: O13 — Open WebUI link block in the user edit drawer: show the linked
// Open WebUI account (sub) and its tokens, link one by hand, or unlink it.
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type KeyboardEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { SideDrawerSection } from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  getOpenWebUIRelayStatus,
  OPENWEBUI_RELAY_STATUS_QUERY_KEY,
} from '@/features/system-settings/site/openwebui-fork'
import { handleServerError } from '@/lib/handle-server-error'
import { AuthOperationError } from '@/lib/secure-verification'

import {
  createOpenWebUILink,
  deleteOpenWebUILink,
  getOpenWebUILink,
  OPENWEBUI_BINDING_TYPE,
  type OpenWebUILink,
  type OpenWebUITokenStatus,
} from '../openwebui-link-api-fork'
import { useUsers } from './users-provider'

const TOKEN_STATUS_LABEL: Record<number, string> = {
  1: 'Enabled',
  2: 'Disabled',
  3: 'Expired',
  4: 'Exhausted',
}

function tokenStatusKey(token: OpenWebUITokenStatus): string {
  if (!token.exists) return 'Not created yet'
  return TOKEN_STATUS_LABEL[token.status] ?? 'Unknown'
}

/**
 * Links are created automatically by email on the first relay request; this
 * block covers what cannot auto-link (no matching email, duplicate emails, or
 * a recreated Open WebUI account whose new sub must replace the old one).
 * Hidden while the relay is disabled.
 */
export function UserOpenWebUILinkSection({ userId }: { userId: number }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { requestVerification, verificationActive } = useUsers()
  const [sub, setSub] = useState('')
  const [linking, setLinking] = useState(false)
  const [unlinking, setUnlinking] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)

  const statusQuery = useQuery({
    queryKey: OPENWEBUI_RELAY_STATUS_QUERY_KEY,
    queryFn: getOpenWebUIRelayStatus,
    staleTime: 60 * 1000,
  })
  const relayEnabled = statusQuery.data?.data?.relay_enabled === true

  const linkQueryKey = ['openwebui-link', userId]
  const linkQuery = useQuery({
    queryKey: linkQueryKey,
    queryFn: () => getOpenWebUILink(userId),
    enabled: relayEnabled && userId > 0,
  })
  const link = linkQuery.data?.data ?? null

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: linkQueryKey })

  const handleLink = async () => {
    const trimmed = sub.trim()
    if (trimmed === '' || linking) return
    setLinking(true)
    try {
      const proof = await requestVerification({
        scope: 'admin.user.update',
        context: { user_id: userId },
        title: t('Verify to link Open WebUI account'),
        description: t(
          'Confirm your identity before linking an Open WebUI account to this user.'
        ),
      })
      if (!proof) return
      const res = await createOpenWebUILink(userId, trimmed, proof.proof_token)
      if (res.success) {
        toast.success(t('Open WebUI account linked'))
        setSub('')
        await refresh()
      } else {
        handleServerError(res, t('Failed to link Open WebUI account'))
      }
    } catch (error) {
      handleServerError(
        AuthOperationError.from(error),
        t('Failed to link Open WebUI account')
      )
    } finally {
      setLinking(false)
    }
  }

  const handleUnlink = async (target: OpenWebUILink) => {
    setUnlinking(true)
    try {
      const proof = await requestVerification({
        scope: 'admin.user.binding.clear',
        context: {
          user_id: target.user_id,
          binding_type: OPENWEBUI_BINDING_TYPE,
        },
        title: t('Verify to unlink Open WebUI account'),
        description: t(
          'Confirm your identity before unlinking the Open WebUI account from this user.'
        ),
      })
      // Cancelled: the confirmation shows again.
      if (!proof) return
      const res = await deleteOpenWebUILink(target.id, proof.proof_token)
      if (res.success) {
        toast.success(t('Open WebUI account unlinked'))
        await refresh()
      } else {
        handleServerError(res, t('Failed to unlink Open WebUI account'))
      }
      setConfirmOpen(false)
    } catch (error) {
      handleServerError(
        AuthOperationError.from(error),
        t('Failed to unlink Open WebUI account')
      )
      setConfirmOpen(false)
    } finally {
      setUnlinking(false)
    }
  }

  // The drawer wraps this block in its user form; Enter must not submit it.
  const onSubKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== 'Enter') return
    event.preventDefault()
    void handleLink()
  }

  if (!relayEnabled) return null

  let content: ReactNode
  if (linkQuery.isLoading) {
    content = <p className='text-muted-foreground text-xs'>{t('Loading...')}</p>
  } else if (link) {
    content = (
      <div className='flex flex-col gap-3'>
        <div>
          <Label
            htmlFor='owui-linked-sub'
            className='text-muted-foreground text-xs'
          >
            {t('Open WebUI user id (sub)')}
          </Label>
          <Input
            id='owui-linked-sub'
            value={link.sub}
            disabled
            className='mt-1 font-mono'
          />
        </div>
        <div className='text-muted-foreground grid grid-cols-2 gap-2 text-xs'>
          <span>
            {t('Linked by')}:{' '}
            {link.linked_by === 'auto' ? t('Automatic (email)') : t('Admin')}
          </span>
          <span>
            {t('Linked at')}:{' '}
            {new Date(link.created_at * 1000).toLocaleString()}
          </span>
          {link.tokens.map((token) => (
            <span key={token.name}>
              {token.name}: {t(tokenStatusKey(token))}
            </span>
          ))}
        </div>
        <div>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => setConfirmOpen(true)}
            disabled={unlinking}
          >
            {t('Unlink')}
          </Button>
        </div>
        <ConfirmDialog
          open={confirmOpen && !verificationActive}
          onOpenChange={setConfirmOpen}
          title={t('Unlink Open WebUI account?')}
          desc={t(
            'The next request from this Open WebUI account will try to auto-link by email again. The openwebui tokens are kept so usage history stays intact.'
          )}
          destructive
          confirmText={t('Unlink')}
          isLoading={unlinking}
          handleConfirm={() => void handleUnlink(link)}
        />
      </div>
    )
  } else {
    content = (
      <div className='flex flex-col gap-2'>
        <Label
          htmlFor='owui-sub-input'
          className='text-muted-foreground text-xs'
        >
          {t('Open WebUI user id (sub)')}
        </Label>
        <div className='flex gap-2'>
          <Input
            id='owui-sub-input'
            value={sub}
            onChange={(e) => setSub(e.target.value)}
            onKeyDown={onSubKeyDown}
            autoComplete='off'
            placeholder={t(
              'Paste the sub from the server log or Open WebUI admin page'
            )}
            className='flex-1 font-mono'
          />
          <Button
            type='button'
            size='sm'
            onClick={() => void handleLink()}
            disabled={sub.trim() === '' || linking}
          >
            {t('Link')}
          </Button>
        </div>
      </div>
    )
  }

  return (
    <SideDrawerSection>
      <h3 className='text-sm font-medium'>{t('Open WebUI Link')}</h3>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Requests from Open WebUI are billed to this user once their Open WebUI account (sub) is linked. Linking happens automatically by email on the first request; use this when it did not.'
        )}
      </p>

      {content}
    </SideDrawerSection>
  )
}
