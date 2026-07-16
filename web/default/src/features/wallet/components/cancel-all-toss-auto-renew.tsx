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
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/confirm-dialog'

interface CancelAllTossAutoRenewProps {
  count: number
  loading: boolean
  onConfirm: () => boolean | Promise<boolean>
}

export function CancelAllTossAutoRenew({
  count,
  loading,
  onConfirm,
}: CancelAllTossAutoRenewProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  if (count <= 0) return null

  const handleConfirm = async () => {
    const succeeded = await onConfirm()
    if (succeeded) setOpen(false)
  }

  return (
    <>
      <div className='border-destructive/30 bg-destructive/5 flex flex-wrap items-center justify-between gap-2 rounded-md border p-3'>
        <span className='text-muted-foreground text-xs'>
          {t('{{count}} configured Toss automatic renewals', { count })}
        </span>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={loading}
          onClick={() => setOpen(true)}
        >
          {t('Cancel all auto-renewals')}
        </Button>
      </div>
      <ConfirmDialog
        destructive
        open={open}
        onOpenChange={setOpen}
        title={t('Cancel all automatic renewals?')}
        desc={t(
          'This will stop all {{count}} configured Toss automatic renewals and revoke their saved automatic-payment authorizations. Any current subscription period remains available until its end date.',
          { count }
        )}
        confirmText={t('Cancel all auto-renewals')}
        handleConfirm={() => void handleConfirm()}
        isLoading={loading}
      />
    </>
  )
}
