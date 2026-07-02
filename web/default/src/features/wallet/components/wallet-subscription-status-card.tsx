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
import { Crown, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { TitledCard } from '@/components/ui/titled-card'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  StatusBadge,
  dotColorMap,
  textColorMap,
} from '@/components/status-badge'
import type {
  SubscriptionPlan,
  UserSubscriptionRecord,
} from '@/features/subscriptions/types'

interface WalletSubscriptionStatusCardProps {
  activeSubscriptions: UserSubscriptionRecord[]
  allSubscriptions: UserSubscriptionRecord[]
  billingPreference: string
  refreshing: boolean
  cancellingAutoRenew: boolean
  onRefresh: () => void | Promise<void>
  onBillingPreferenceChange: (preference: string) => void | Promise<void>
  onCancelTossAutoRenew: () => void | Promise<void>
}

type UserSubscriptionWithPlan = UserSubscriptionRecord & {
  plan?: Pick<SubscriptionPlan, 'id' | 'title'>
}

export function getBillingPreferenceLabel(
  preference: string,
  t: (key: string) => string
): string {
  switch (preference) {
    case 'subscription_first':
      return t('Subscription First')
    case 'wallet_first':
      return t('Wallet First')
    case 'subscription_only':
      return t('Subscription Only')
    case 'wallet_only':
      return t('Wallet Only')
    default:
      return preference
  }
}

export function getRemainingDays(sub: UserSubscriptionRecord): number {
  const endTime = sub?.subscription?.end_time || 0
  if (!endTime) return 0
  const now = Date.now() / 1000
  return Math.max(0, Math.ceil((endTime - now) / 86400))
}

export function getUsagePercent(sub: UserSubscriptionRecord): number {
  const total = Number(sub?.subscription?.amount_total || 0)
  const used = Number(sub?.subscription?.amount_used || 0)
  if (total <= 0) return 0
  return Math.round((used / total) * 100)
}

export function WalletSubscriptionStatusCard({
  activeSubscriptions,
  allSubscriptions,
  billingPreference,
  refreshing,
  cancellingAutoRenew,
  onRefresh,
  onBillingPreferenceChange,
  onCancelTossAutoRenew,
}: WalletSubscriptionStatusCardProps) {
  const { t } = useTranslation()

  if (activeSubscriptions.length === 0) {
    return null
  }

  const expiredCount = Math.max(
    0,
    allSubscriptions.length - activeSubscriptions.length
  )

  return (
    <TitledCard
      title={t('Subscription')}
      icon={<Crown className='h-4 w-4' />}
      contentClassName='space-y-4 sm:space-y-5'
    >
      <div className='rounded-xl border p-3 sm:p-4'>
        <div className='flex flex-wrap items-center justify-between gap-2.5 sm:gap-3'>
          <div className='flex min-w-0 flex-wrap items-center gap-2'>
            <span className='text-sm font-medium'>{t('My Subscriptions')}</span>
            <span className='flex items-center gap-1.5 text-xs font-medium'>
              <span
                className={cn(
                  'size-1.5 shrink-0 rounded-full',
                  dotColorMap.success
                )}
                aria-hidden='true'
              />
              <span className={cn(textColorMap.success)}>
                {activeSubscriptions.length} {t('active')}
              </span>
              {expiredCount > 0 && (
                <>
                  <span className='text-muted-foreground/30'>·</span>
                  <span className='text-muted-foreground'>
                    {expiredCount} {t('expired')}
                  </span>
                </>
              )}
            </span>
          </div>
          <div className='flex w-full items-center gap-2 sm:w-auto'>
            <Select
              items={[
                {
                  value: 'subscription_first',
                  label: getBillingPreferenceLabel('subscription_first', t),
                },
                {
                  value: 'wallet_first',
                  label: getBillingPreferenceLabel('wallet_first', t),
                },
                {
                  value: 'subscription_only',
                  label: getBillingPreferenceLabel('subscription_only', t),
                },
                {
                  value: 'wallet_only',
                  label: getBillingPreferenceLabel('wallet_only', t),
                },
              ]}
              value={billingPreference}
              onValueChange={(value) => {
                if (value !== null) {
                  void onBillingPreferenceChange(value)
                }
              }}
            >
              <SelectTrigger className='h-8 flex-1 text-xs sm:w-[140px] sm:flex-none'>
                <SelectValue>
                  {getBillingPreferenceLabel(billingPreference, t)}
                </SelectValue>
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value='subscription_first'>
                    {getBillingPreferenceLabel('subscription_first', t)}
                  </SelectItem>
                  <SelectItem value='wallet_first'>
                    {getBillingPreferenceLabel('wallet_first', t)}
                  </SelectItem>
                  <SelectItem value='subscription_only'>
                    {getBillingPreferenceLabel('subscription_only', t)}
                  </SelectItem>
                  <SelectItem value='wallet_only'>
                    {getBillingPreferenceLabel('wallet_only', t)}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
            <Button
              variant='ghost'
              size='icon'
              className='h-8 w-8'
              onClick={() => void onRefresh()}
              disabled={refreshing}
            >
              <RefreshCw
                className={`h-3.5 w-3.5 ${refreshing ? 'animate-spin' : ''}`}
              />
            </Button>
          </div>
        </div>

        <Separator className='my-3' />
        <div className='max-h-64 space-y-3 overflow-y-auto pr-1'>
          {activeSubscriptions.map((sub) => {
            const record = sub as UserSubscriptionWithPlan
            const subscription = record.subscription
            const totalAmount = Number(subscription?.amount_total || 0)
            const usedAmount = Number(subscription?.amount_used || 0)
            const remainAmount =
              totalAmount > 0 ? Math.max(0, totalAmount - usedAmount) : 0
            const planTitle = record.plan?.title || ''
            const remainDays = getRemainingDays(record)
            const usagePercent = getUsagePercent(record)

            return (
              <div
                key={subscription?.id}
                className='bg-background rounded-md border p-3 text-xs'
              >
                <div className='flex items-center justify-between'>
                  <div className='flex items-center gap-2'>
                    <span className='font-medium'>
                      {planTitle
                        ? `${planTitle} · ${t('Subscription')} #${subscription?.id}`
                        : `${t('Subscription')} #${subscription?.id}`}
                    </span>
                    <StatusBadge
                      label={t('Active')}
                      variant='success'
                      copyable={false}
                    />
                  </div>
                  <span className='text-muted-foreground'>
                    {t('{{count}} days remaining', {
                      count: remainDays,
                    })}
                  </span>
                </div>
                <div className='text-muted-foreground mt-1.5'>
                  {t('Until')}{' '}
                  {new Date(
                    (subscription?.end_time || 0) * 1000
                  ).toLocaleString()}
                </div>
                {(subscription?.next_reset_time ?? 0) > 0 && (
                  <div className='text-muted-foreground mt-1'>
                    {t('Next reset')}:{' '}
                    {new Date(
                      subscription!.next_reset_time! * 1000
                    ).toLocaleString()}
                  </div>
                )}
                <div className='text-muted-foreground mt-1'>
                  {t('Total Quota')}:{' '}
                  {totalAmount > 0 ? (
                    <Tooltip>
                      <TooltipTrigger render={<span className='cursor-help' />}>
                        {formatQuota(usedAmount)}/{formatQuota(totalAmount)} ·{' '}
                        {t('Remaining')} {formatQuota(remainAmount)}
                      </TooltipTrigger>
                      <TooltipContent>
                        {t('Raw Quota')}: {usedAmount}/{totalAmount} ·{' '}
                        {t('Remaining')} {remainAmount}
                      </TooltipContent>
                    </Tooltip>
                  ) : (
                    t('Unlimited')
                  )}
                  {totalAmount > 0 && (
                    <span className='ml-2'>
                      {t('Used')} {usagePercent}%
                    </span>
                  )}
                </div>
                {totalAmount > 0 && (
                  <Progress value={usagePercent} className='mt-2 h-1.5' />
                )}
                {subscription?.auto_renew && (
                  <div className='mt-2 flex items-center justify-between'>
                    <span className='text-muted-foreground text-xs'>
                      {t('Auto-renew active')}
                    </span>
                    <Button
                      variant='outline'
                      size='sm'
                      className='h-6 px-2 text-xs'
                      onClick={() => void onCancelTossAutoRenew()}
                      disabled={cancellingAutoRenew}
                    >
                      {t('Cancel Auto-renew')}
                    </Button>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </TitledCard>
  )
}
