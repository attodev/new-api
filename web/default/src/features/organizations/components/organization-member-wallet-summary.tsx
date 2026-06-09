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
import { formatQuota, formatTimestamp } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { TitledCard } from '@/components/ui/titled-card'
import { SectionPageLayout } from '@/components/layout'
import { WalletStatsCard } from '@/features/wallet/components/wallet-stats-card'
import type { UserWalletData } from '@/features/wallet/types'
import { getMyOrganizationSubscription } from '../api'
import type { OrganizationUserSubscriptionRecord } from '../types'

function getSubscriptionStatusKey(status: string): string {
  switch (status) {
    case 'active':
      return 'Subscription status active'
    case 'cancelled':
      return 'Subscription status cancelled'
    case 'expired':
      return 'Subscription status expired'
    default:
      return status
  }
}

export function OrganizationMemberWalletSummary() {
  const { t } = useTranslation()
  const [user, setUser] = useState<UserWalletData | null>(null)
  const [subscription, setSubscription] =
    useState<OrganizationUserSubscriptionRecord | null>(null)
  const [loading, setLoading] = useState(true)

  const fetchUser = useCallback(async () => {
    try {
      setLoading(true)
      const [response, subscriptionResponse] = await Promise.all([
        getSelf(),
        getMyOrganizationSubscription(),
      ])
      if (response.success && response.data) {
        setUser(response.data as UserWalletData)
      } else {
        toast.error(response.message || t('Failed to load wallet'))
      }
      if (subscriptionResponse.success) {
        setSubscription(subscriptionResponse.data || null)
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

  const hasActivePlan = subscription?.subscription?.status === 'active'

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Wallet')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
          <WalletStatsCard
            user={user}
            loading={loading}
            balanceDisplay={
              hasActivePlan
                ? {
                    value: t('Organization plan in use'),
                    description: t(
                      'Balance is managed by the assigned organization plan'
                    ),
                  }
                : undefined
            }
          />

          <TitledCard
            title={t('Organization allocated quota')}
            description={t(
              'Your available quota is allocated by your organization.'
            )}
            icon={<WalletCards className='h-4 w-4' />}
            action={
              user?.group ? (
                <Badge variant='secondary'>{t(user.group)}</Badge>
              ) : null
            }
          >
            <div className='bg-muted/30 flex items-start gap-3 rounded-md border p-3 text-sm'>
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

          <TitledCard
            title={t('Assigned organization plan')}
            description={t(
              'If a plan is assigned, requests use this plan limit and the organization wallet.'
            )}
            icon={<WalletCards className='h-4 w-4' />}
            action={
              subscription?.subscription?.status ? (
                <Badge variant='secondary'>
                  {t(
                    getSubscriptionStatusKey(subscription.subscription.status)
                  )}
                </Badge>
              ) : null
            }
          >
            {subscription?.subscription ? (
              <div className='grid gap-2 text-sm sm:grid-cols-3'>
                <div>
                  <div className='text-muted-foreground'>
                    {t('Organization plan')}
                  </div>
                  <div className='font-medium'>
                    {subscription.plan?.title ||
                      `#${subscription.subscription.plan_id}`}
                  </div>
                </div>
                <div>
                  <div className='text-muted-foreground'>{t('Used')}</div>
                  <div className='font-medium'>
                    {subscription.subscription.amount_total > 0
                      ? `${formatQuota(
                          subscription.subscription.amount_used
                        )} / ${formatQuota(
                          subscription.subscription.amount_total
                        )}`
                      : formatQuota(subscription.subscription.amount_used)}
                  </div>
                </div>
                <div>
                  <div className='text-muted-foreground'>{t('Expires')}</div>
                  <div className='font-medium'>
                    {formatTimestamp(subscription.subscription.end_time)}
                  </div>
                </div>
              </div>
            ) : (
              <div className='text-muted-foreground text-sm'>
                {t('No organization plan is assigned.')}
              </div>
            )}
          </TitledCard>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
