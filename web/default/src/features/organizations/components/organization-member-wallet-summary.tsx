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
import { useCallback, useEffect, useState } from 'react'
import { Building2, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getSelf } from '@/lib/api'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { TitledCard } from '@/components/ui/titled-card'
import { WalletStatsCard } from '@/features/wallet/components/wallet-stats-card'
import type { UserWalletData } from '@/features/wallet/types'

export function OrganizationMemberWalletSummary() {
  const { t } = useTranslation()
  const [user, setUser] = useState<UserWalletData | null>(null)
  const [loading, setLoading] = useState(true)

  const fetchUser = useCallback(async () => {
    try {
      setLoading(true)
      const response = await getSelf()
      if (response.success && response.data) {
        setUser(response.data as UserWalletData)
      } else {
        toast.error(response.message || t('Failed to load wallet'))
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch organization member wallet:', error)
      toast.error(t('Failed to load wallet'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void fetchUser()
  }, [fetchUser])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Wallet')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
          <WalletStatsCard user={user} loading={loading} />

          <TitledCard
            title={t('Organization allocated quota')}
            description={t(
              'Your available quota is allocated by your organization.'
            )}
            icon={<WalletCards className='h-4 w-4' />}
            action={
              user?.group ? <Badge variant='secondary'>{user.group}</Badge> : null
            }
          >
            <div className='flex items-start gap-3 rounded-md border bg-muted/30 p-3 text-sm'>
              <Building2 className='text-muted-foreground mt-0.5 size-4 shrink-0' />
              <div className='space-y-1'>
                <div className='font-medium'>
                  {t('Topups are managed by your organization')}
                </div>
                <p className='text-muted-foreground'>
                  {t(
                    'Contact your organization owner or admin if you need more quota.'
                  )}
                </p>
              </div>
            </div>
          </TitledCard>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
