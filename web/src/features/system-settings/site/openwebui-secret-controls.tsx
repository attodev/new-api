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
// fork: O13 — write-only secret helpers for the Open WebUI settings sections:
// a set / not set badge and a confirmed Clear button.
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

export function ConfiguredBadge(props: { configured: boolean | undefined }) {
  const { t } = useTranslation()
  if (props.configured === undefined) return null
  return (
    <Badge variant={props.configured ? 'default' : 'outline'} className='ml-2'>
      {props.configured ? t('Configured') : t('Not configured')}
    </Badge>
  )
}

type ClearSecretButtonProps = {
  disabled?: boolean
  isPending?: boolean
  onConfirm: () => Promise<unknown>
}

/** Clearing is explicit: an empty field always means "keep the current value". */
export function ClearSecretButton(props: ClearSecretButtonProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button
        type='button'
        variant='outline'
        onClick={() => setOpen(true)}
        disabled={props.disabled || props.isPending}
      >
        {t('Clear')}
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={t('Clear the stored secret?')}
        desc={t(
          'The stored value is removed. Enter a new value and save to set it again.'
        )}
        destructive
        confirmText={t('Clear')}
        isLoading={props.isPending}
        handleConfirm={() => {
          void props.onConfirm().finally(() => setOpen(false))
        }}
      />
    </>
  )
}
