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
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { SideDrawerSection } from '@/components/drawer-layout'
import { getOpenWebUIRelayStatus } from '@/features/system-settings/api'
import {
  createOpenWebUILink,
  deleteOpenWebUILink,
  getOpenWebUILink,
} from '../api'
import type { OpenWebUITokenStatus } from '../types'

const TOKEN_STATUS_LABEL: Record<number, string> = {
  1: 'Enabled',
  2: 'Disabled',
  3: 'Expired',
  4: 'Exhausted',
}

function tokenStatusText(
  token: OpenWebUITokenStatus,
  t: (k: string) => string
) {
  if (!token.exists) return t('Not created yet')
  return t(TOKEN_STATUS_LABEL[token.status] ?? 'Unknown')
}

/**
 * Admin view of the Open WebUI ↔ new-api user link. Links are created
 * automatically by email on the first relay request; this block exists for
 * the cases that cannot auto-link (no matching email, duplicate emails, or
 * a recreated Open WebUI account whose new sub must replace the old one).
 * Hidden entirely while the relay is disabled.
 */
export function UserOpenWebUILinkSection({ userId }: { userId: number }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [sub, setSub] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)

  const statusQuery = useQuery({
    queryKey: ['openwebui-relay-status'],
    queryFn: getOpenWebUIRelayStatus,
    staleTime: 60 * 1000,
  })
  const relayEnabled = statusQuery.data?.data?.relay_enabled === true

  const linkQuery = useQuery({
    queryKey: ['openwebui-link', userId],
    queryFn: () => getOpenWebUILink(userId),
    enabled: relayEnabled && userId > 0,
  })
  const link = linkQuery.data?.data ?? null

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['openwebui-link', userId] })

  const linkMutation = useMutation({
    mutationFn: () => createOpenWebUILink(userId, sub.trim()),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(t('Open WebUI account linked'))
        setSub('')
        refresh()
      } else {
        toast.error(res.message || t('Failed to link Open WebUI account'))
      }
    },
    onError: () => toast.error(t('Failed to link Open WebUI account')),
  })

  const unlinkMutation = useMutation({
    mutationFn: (id: number) => deleteOpenWebUILink(id),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(t('Open WebUI account unlinked'))
        refresh()
      } else {
        toast.error(res.message || t('Failed to unlink Open WebUI account'))
      }
      setConfirmOpen(false)
    },
    onError: () => {
      toast.error(t('Failed to unlink Open WebUI account'))
      setConfirmOpen(false)
    },
  })

  if (!relayEnabled) return null

  return (
    <SideDrawerSection>
      <h3 className='text-sm font-medium'>{t('Open WebUI Link')}</h3>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Requests from Open WebUI are billed to this user once their Open WebUI account (sub) is linked. Linking happens automatically by email on the first request; use this when it did not.'
        )}
      </p>

      {linkQuery.isLoading ? (
        <p className='text-muted-foreground text-xs'>{t('Loading...')}</p>
      ) : link ? (
        <div className='flex flex-col gap-3'>
          <div>
            <Label className='text-muted-foreground text-xs'>
              {t('Open WebUI user id (sub)')}
            </Label>
            <Input value={link.sub} disabled className='mt-1 font-mono' />
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
                {token.name}: {tokenStatusText(token, t)}
              </span>
            ))}
          </div>
          <div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setConfirmOpen(true)}
              disabled={unlinkMutation.isPending}
            >
              {t('Unlink')}
            </Button>
          </div>
          <ConfirmDialog
            open={confirmOpen}
            onOpenChange={setConfirmOpen}
            title={t('Unlink Open WebUI account?')}
            desc={t(
              'The next request from this Open WebUI account will try to auto-link by email again. The openwebui tokens are kept so usage history stays intact.'
            )}
            destructive
            confirmText={t('Unlink')}
            isLoading={unlinkMutation.isPending}
            handleConfirm={() => unlinkMutation.mutate(link.id)}
          />
        </div>
      ) : (
        <div className='flex flex-col gap-2'>
          <Label className='text-muted-foreground text-xs'>
            {t('Open WebUI user id (sub)')}
          </Label>
          <div className='flex gap-2'>
            <Input
              value={sub}
              onChange={(e) => setSub(e.target.value)}
              placeholder={t(
                'Paste the sub from the server log or Open WebUI admin page'
              )}
              className='flex-1 font-mono'
            />
            <Button
              type='button'
              size='sm'
              onClick={() => linkMutation.mutate()}
              disabled={sub.trim() === '' || linkMutation.isPending}
            >
              {t('Link')}
            </Button>
          </div>
        </div>
      )}
    </SideDrawerSection>
  )
}
