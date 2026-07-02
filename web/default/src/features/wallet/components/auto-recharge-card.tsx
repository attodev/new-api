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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  getScheduledPeriodLabelKey,
  groupScheduledPresetOptions,
  groupThresholdPresetOptions,
} from '../lib/auto-recharge-options'
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
  creationDisabled?: boolean
  creationDisabledMessageKey?: string
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
  policies: Array<Pick<WalletAutoRechargePolicy, 'type' | 'status'>>,
  presetsLoaded = true,
  policiesLoaded = true
): WalletAutoRechargeType[] {
  if (!presetsLoaded || !policiesLoaded) {
    return ['scheduled', 'threshold']
  }

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
  creationDisabled = false,
  creationDisabledMessageKey = 'Cancel the current payment setting before choosing another one.',
  onCreateScheduled,
  onCreateThreshold,
  onCancel,
}: AutoRechargeCardProps) {
  const { t } = useTranslation()
  const [selectedScheduledPeriodKey, setSelectedScheduledPeriodKey] =
    useState<string | null>(null)
  const [selectedThresholdAmount, setSelectedThresholdAmount] = useState<
    number | null
  >(null)

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
  const creationLocked = creationDisabled && !activePolicy
  const creationButtonDisabled = disabled || creationLocked
  const scheduledGroups = useMemo(
    () => groupScheduledPresetOptions(availablePresets),
    [availablePresets]
  )
  const thresholdGroups = useMemo(
    () => groupThresholdPresetOptions(availablePresets),
    [availablePresets]
  )
  const selectedScheduledGroup =
    scheduledGroups.find((group) => group.period.key === selectedScheduledPeriodKey) ??
    scheduledGroups[0]
  const selectedThresholdGroup =
    thresholdGroups.find((group) => group.amount === selectedThresholdAmount) ??
    null

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

  const handleThresholdAmountClick = async (
    group: (typeof thresholdGroups)[number]
  ) => {
    if (group.thresholds.length === 1) {
      await handleSelectPreset(group.thresholds[0].preset.id)
      return
    }
    setSelectedThresholdAmount(group.amount)
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

        {creationLocked ? (
          <div className='text-muted-foreground text-sm'>
            {t(creationDisabledMessageKey)}
          </div>
        ) : null}

        {!activePolicy && availablePresets.length > 0 ? (
          <div className={cn('space-y-3', !canManage && 'opacity-60')}>
            {mode === 'scheduled' ? (
              <>
                {scheduledGroups.length > 1 ? (
                  <div className='space-y-2'>
                    <div className='text-sm font-medium'>
                      {t('Choose recharge period')}
                    </div>
                    <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
                      {scheduledGroups.map((group) => (
                        <Button
                          key={group.period.key}
                          type='button'
                          variant={
                            selectedScheduledGroup?.period.key ===
                            group.period.key
                              ? 'default'
                              : 'outline'
                          }
                          disabled={creationButtonDisabled}
                          onClick={() =>
                            setSelectedScheduledPeriodKey(group.period.key)
                          }
                          className='h-10'
                        >
                          {t(getScheduledPeriodLabelKey(group.period))}
                        </Button>
                      ))}
                    </div>
                  </div>
                ) : null}
                {selectedScheduledGroup ? (
                  <div className='space-y-2'>
                    <div className='text-sm font-medium'>
                      {t('Choose recharge amount')}
                    </div>
                    <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
                      {selectedScheduledGroup.amounts.map((option) => (
                        <Button
                          key={`${selectedScheduledGroup.period.key}:${option.amount}`}
                          type='button'
                          variant='outline'
                          disabled={creationButtonDisabled}
                          onClick={() =>
                            void handleSelectPreset(option.preset.id)
                          }
                          className='h-10'
                        >
                          {option.amount}
                        </Button>
                      ))}
                    </div>
                  </div>
                ) : null}
              </>
            ) : (
              <>
                {thresholdGroups.length > 0 ? (
                  <div className='space-y-2'>
                    <div className='text-sm font-medium'>
                      {t('Choose recharge amount')}
                    </div>
                    <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
                      {thresholdGroups.map((group) => (
                        <Button
                          key={group.amount}
                          type='button'
                          variant={
                            selectedThresholdGroup?.amount === group.amount
                              ? 'default'
                              : 'outline'
                          }
                          disabled={creationButtonDisabled}
                          onClick={() => void handleThresholdAmountClick(group)}
                          className='h-10'
                        >
                          {group.amount}
                        </Button>
                      ))}
                    </div>
                  </div>
                ) : null}
                {selectedThresholdGroup &&
                selectedThresholdGroup.thresholds.length > 1 ? (
                  <div className='space-y-2'>
                    <div className='text-sm font-medium'>
                      {t('Choose threshold balance')}
                    </div>
                    <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
                      {selectedThresholdGroup.thresholds.map((option) => (
                        <Button
                          key={`${selectedThresholdGroup.amount}:${option.thresholdAmount}`}
                          type='button'
                          variant='outline'
                          disabled={creationButtonDisabled}
                          onClick={() =>
                            void handleSelectPreset(option.preset.id)
                          }
                          className='h-10'
                        >
                          {option.thresholdAmount}
                        </Button>
                      ))}
                    </div>
                  </div>
                ) : null}
              </>
            )}
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}
