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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type {
  WalletAutoRechargePolicy,
  WalletAutoRechargePreset,
  WalletAutoRechargeRequest,
  WalletAutoRechargeType,
} from '../types'

interface AutoRechargeCardProps {
  mode: 'scheduled' | 'threshold'
  policies: WalletAutoRechargePolicy[]
  presets: WalletAutoRechargePreset[]
  loading: boolean
  processing: boolean
  canManage: boolean
  permissionMessageKey?: string
  onCreateScheduled: (payload: WalletAutoRechargeRequest) => Promise<boolean>
  onCreateThreshold: (payload: WalletAutoRechargeRequest) => Promise<boolean>
  onCancel: (id: number) => Promise<boolean>
}

function formatTimestamp(timestamp?: number) {
  if (!timestamp) return '-'
  const value = timestamp > 1e12 ? timestamp : timestamp * 1000
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value))
}

export function getAutoRechargeModeTitleKey(mode: WalletAutoRechargeType) {
  return mode === 'scheduled' ? 'Scheduled recharge' : 'Auto recharge'
}

function getModeTitle(
  mode: WalletAutoRechargeType,
  t: (key: string) => string
) {
  return t(getAutoRechargeModeTitleKey(mode))
}

export function parseMoneyInput(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return null

  const normalized = Math.floor(Number(trimmed))
  return Number.isFinite(normalized) ? normalized : null
}

interface AutoRechargeFormState {
  amount: string
  thresholdAmount: string
  intervalUnit: 'month' | 'day'
  intervalValue: string
  chargeImmediately: boolean
}

export function getInitialAutoRechargeFormState(
  activePolicy?: WalletAutoRechargePolicy
): AutoRechargeFormState {
  return {
    amount:
      activePolicy?.amount !== null && activePolicy?.amount !== undefined
        ? String(activePolicy.amount)
        : '',
    thresholdAmount:
      activePolicy?.threshold_amount !== null &&
      activePolicy?.threshold_amount !== undefined
        ? String(activePolicy.threshold_amount)
        : '',
    intervalUnit: activePolicy?.interval_unit === 'day' ? 'day' : 'month',
    intervalValue:
      activePolicy?.interval_value !== null &&
      activePolicy?.interval_value !== undefined
        ? String(activePolicy.interval_value)
        : '1',
    chargeImmediately: activePolicy?.charge_immediately !== false,
  }
}

export function buildPresetCreatePayload(
  presetId: number
): WalletAutoRechargeRequest {
  return { preset_id: presetId }
}

export function getVisibleAutoRechargeModes(
  presets: Array<Pick<WalletAutoRechargePreset, 'type'>>,
  policies: Array<Pick<WalletAutoRechargePolicy, 'type' | 'status'>>
): WalletAutoRechargeType[] {
  return (['scheduled', 'threshold'] as WalletAutoRechargeType[]).filter(
    (mode) =>
      presets.some((preset) => preset.type === mode) ||
      policies.some(
        (policy) =>
          policy.type === mode &&
          (policy.status === 'active' || policy.status === 'pending')
      )
  )
}

export function AutoRechargeCard({
  mode,
  policies,
  presets,
  loading,
  processing,
  canManage,
  permissionMessageKey = 'You do not have permission to manage this.',
  onCreateScheduled,
  onCreateThreshold,
  onCancel,
}: AutoRechargeCardProps) {
  const { t } = useTranslation()

  const activePolicy = useMemo(
    () =>
      policies.find(
        (policy) =>
          policy.type === mode &&
          (policy.status === 'active' || policy.status === 'pending')
      ),
    [mode, policies]
  )

  const availablePresets = useMemo(
    () =>
      presets
        .filter((preset) => preset.enabled && preset.type === mode)
        .sort((left, right) => {
          const sortOrder = (left.sort_order ?? 0) - (right.sort_order ?? 0)
          if (sortOrder !== 0) return sortOrder
          return left.id - right.id
        }),
    [mode, presets]
  )

  const disabled = loading || processing || !canManage

  const handleSelectPreset = async (presetId: number) => {
    const payload = buildPresetCreatePayload(presetId)
    if (mode === 'scheduled') {
      await onCreateScheduled(payload)
      return
    }
    await onCreateThreshold(payload)
  }

  const handleCancel = async () => {
    if (!activePolicy) return
    await onCancel(activePolicy.id)
  }

  return (
    <Card className='gap-0 overflow-hidden py-0'>
      <CardHeader className='border-b p-4'>
        <CardTitle className='text-base'>{getModeTitle(mode, t)}</CardTitle>
      </CardHeader>
      <CardContent className='space-y-4 p-4'>
        <div className='rounded-md border p-3'>
          <div className='flex items-start justify-between gap-3'>
            <div className='space-y-1'>
              <div className='text-sm font-medium'>{t('Current policy')}</div>
              {activePolicy ? (
                <>
                  <div className='text-muted-foreground text-sm'>
                    {t('Recharge amount')}: {activePolicy.amount}
                  </div>
                  {mode === 'scheduled' ? (
                    <div className='text-muted-foreground text-sm'>
                      {t('Charge interval')}: {activePolicy.interval_value || 1}
                      {activePolicy.interval_unit === 'day'
                        ? t('day')
                        : t('month')}
                    </div>
                  ) : (
                    <div className='text-muted-foreground text-sm'>
                      {t('Threshold balance')}:{' '}
                      {activePolicy.threshold_amount ?? 0}
                    </div>
                  )}
                  <div className='text-muted-foreground text-sm'>
                    {t('Next charge')}:{' '}
                    {formatTimestamp(activePolicy.next_charge_time)}
                  </div>
                  <div className='text-muted-foreground text-sm'>
                    {t('Card')}:{' '}
                    {[
                      activePolicy.card_company,
                      activePolicy.card_number_masked,
                    ]
                      .filter(Boolean)
                      .join(' ') || '-'}
                  </div>
                </>
              ) : (
                <div className='text-muted-foreground text-sm'>
                  {t('No policy configured')}
                </div>
              )}
            </div>
            {activePolicy ? (
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={disabled}
                onClick={handleCancel}
              >
                {t('Cancel')}
              </Button>
            ) : null}
          </div>
        </div>

        {!canManage ? (
          <div className='text-muted-foreground text-sm'>
            {t(permissionMessageKey)}
          </div>
        ) : null}

        {availablePresets.length > 0 ? (
          <div className={cn('space-y-3', !canManage && 'opacity-60')}>
            <div className='text-sm font-medium'>{t('Available presets')}</div>
            <div className='space-y-3'>
              {availablePresets.map((preset) => (
                <button
                  key={preset.id}
                  type='button'
                  disabled={disabled}
                  onClick={() => void handleSelectPreset(preset.id)}
                  className='hover:bg-muted w-full rounded-md border p-3 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-60'
                >
                  <div className='font-medium'>{preset.name}</div>
                  {preset.description ? (
                    <div className='text-muted-foreground text-sm'>
                      {preset.description}
                    </div>
                  ) : null}
                  <div className='text-muted-foreground text-sm'>
                    {t('Recharge amount')}: {preset.amount}
                  </div>
                  {mode === 'threshold' &&
                  preset.threshold_amount !== null &&
                  preset.threshold_amount !== undefined ? (
                    <div className='text-muted-foreground text-sm'>
                      {t('Threshold balance')}: {preset.threshold_amount}
                    </div>
                  ) : null}
                  {mode === 'scheduled' &&
                  preset.interval_value !== null &&
                  preset.interval_value !== undefined ? (
                    <div className='text-muted-foreground text-sm'>
                      {t('Charge interval')}: {preset.interval_value}
                      {preset.interval_unit === 'day' ? t('day') : t('month')}
                    </div>
                  ) : null}
                </button>
              ))}
            </div>
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}
