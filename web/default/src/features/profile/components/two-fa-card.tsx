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
import { Shield } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useDialogs } from '@/hooks/use-dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { useTwoFA } from '../hooks'
import { TwoFADisableDialog } from './dialogs/two-fa-disable-dialog'
import { TwoFASetupDialog } from './dialogs/two-fa-setup-dialog'

// ============================================================================
// Two-Factor Authentication Card Component
// ============================================================================

interface TwoFACardProps {
  loading: boolean
}

type DialogKey = 'setup' | 'disable'

export function TwoFACard({ loading: pageLoading }: TwoFACardProps) {
  const { t } = useTranslation()
  const { status, loading, refetch } = useTwoFA(!pageLoading)
  const dialogs = useDialogs<DialogKey>()

  if (pageLoading || loading) {
    return <Skeleton className='h-24 w-full rounded-lg' />
  }

  return (
    <>
      <button
        type='button'
        onClick={() => dialogs.open(status.enabled ? 'disable' : 'setup')}
        className='hover:bg-muted/50 flex items-center gap-3 rounded-lg border p-3 text-left transition-colors md:flex-col md:gap-2 md:p-4 md:text-center'
      >
        <div className='bg-muted rounded-md p-2'>
          <Shield className='h-5 w-5' />
        </div>
        <div className='min-w-0 md:contents'>
          <p className='text-sm font-medium'>
            {t('Two-Factor Authentication')}
          </p>
          <p className='text-muted-foreground line-clamp-1 text-xs md:line-clamp-none'>
            {t('Add an extra layer of security to your account')}
          </p>
        </div>
      </button>

      {/* Dialogs */}
      <TwoFASetupDialog
        open={dialogs.isOpen('setup')}
        onOpenChange={(open) =>
          open ? dialogs.open('setup') : dialogs.close('setup')
        }
        onSuccess={refetch}
      />

      <TwoFADisableDialog
        open={dialogs.isOpen('disable')}
        onOpenChange={(open) =>
          open ? dialogs.open('disable') : dialogs.close('disable')
        }
        onSuccess={refetch}
      />
    </>
  )
}
