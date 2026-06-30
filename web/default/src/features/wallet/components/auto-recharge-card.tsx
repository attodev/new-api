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
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import type {
  WalletAutoRechargePolicy,
  WalletAutoRechargeRequest,
  WalletAutoRechargeType,
} from '../types'

interface AutoRechargeCardProps {
  mode: 'scheduled' | 'threshold'
  policies: WalletAutoRechargePolicy[]
  loading: boolean
  processing: boolean
  canManage: boolean
  minTopup: number
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

export function AutoRechargeCard({
  mode,
  policies,
  loading,
  processing,
  canManage,
  minTopup,
  permissionMessageKey = 'You do not have permission to manage this.',
  onCreateScheduled,
  onCreateThreshold,
  onCancel,
}: AutoRechargeCardProps) {
  const { t } = useTranslation()
  const [amount, setAmount] = useState('')
  const [thresholdAmount, setThresholdAmount] = useState('')
  const [intervalUnit, setIntervalUnit] = useState<'month' | 'day'>('month')
  const [intervalValue, setIntervalValue] = useState('1')
  const [chargeImmediately, setChargeImmediately] = useState(true)

  const activePolicy = useMemo(
    () =>
      policies.find(
        (policy) =>
          policy.type === mode &&
          (policy.status === 'active' || policy.status === 'pending')
      ),
    [mode, policies]
  )

  useEffect(() => {
    if (!activePolicy) return

    const nextState = getInitialAutoRechargeFormState(activePolicy)
    setAmount(nextState.amount)
    setThresholdAmount(nextState.thresholdAmount)
    setIntervalUnit(nextState.intervalUnit)
    setIntervalValue(nextState.intervalValue)
    setChargeImmediately(nextState.chargeImmediately)
  }, [activePolicy])

  const disabled = loading || processing || !canManage

  const handleSubmit = async () => {
    const normalizedAmount = parseMoneyInput(amount)
    if (normalizedAmount === null || normalizedAmount < minTopup) {
      toast.error(
        t('Minimum recharge amount is {{amount}}.', {
          amount: String(minTopup),
        })
      )
      return
    }

    const payload: WalletAutoRechargeRequest = { amount: normalizedAmount }

    if (mode === 'scheduled') {
      const normalizedInterval = parseMoneyInput(intervalValue)
      if (normalizedInterval === null || normalizedInterval <= 0) {
        toast.error(t('Please enter a valid interval value.'))
        return
      }
      payload.interval_unit = intervalUnit
      payload.interval_value = normalizedInterval
      payload.charge_immediately = chargeImmediately
      await onCreateScheduled(payload)
      return
    }

    const normalizedThreshold = parseMoneyInput(thresholdAmount)
    if (normalizedThreshold === null || normalizedThreshold < 0) {
      toast.error(t('Please enter a valid threshold balance.'))
      return
    }
    payload.threshold_amount = normalizedThreshold
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

        <div className={cn('space-y-4', !canManage && 'opacity-60')}>
          <div className='space-y-2'>
            <Label htmlFor={`${mode}-amount`}>{t('Recharge amount')}</Label>
            <Input
              id={`${mode}-amount`}
              inputMode='numeric'
              value={amount}
              disabled={disabled}
              onChange={(event) => setAmount(event.target.value)}
            />
          </div>

          {mode === 'scheduled' ? (
            <>
              <div className='grid gap-4 sm:grid-cols-2'>
                <div className='space-y-2'>
                  <Label htmlFor={`${mode}-interval-unit`}>
                    {t('Charge interval')}
                  </Label>
                  <Select
                    value={intervalUnit}
                    disabled={disabled}
                    onValueChange={(value) =>
                      setIntervalUnit(value as 'month' | 'day')
                    }
                  >
                    <SelectTrigger id={`${mode}-interval-unit`}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value='month'>{t('month')}</SelectItem>
                      <SelectItem value='day'>{t('day')}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className='space-y-2'>
                  <Label htmlFor={`${mode}-interval-value`}>
                    {t('Interval value')}
                  </Label>
                  <Input
                    id={`${mode}-interval-value`}
                    inputMode='numeric'
                    value={intervalValue}
                    disabled={disabled}
                    onChange={(event) => setIntervalValue(event.target.value)}
                  />
                </div>
              </div>

              <div className='flex items-center justify-between rounded-md border px-3 py-2'>
                <Label
                  htmlFor={`${mode}-charge-immediately`}
                  className='cursor-pointer'
                >
                  {t('Charge immediately')}
                </Label>
                <Switch
                  id={`${mode}-charge-immediately`}
                  checked={chargeImmediately}
                  disabled={disabled}
                  onCheckedChange={setChargeImmediately}
                />
              </div>
            </>
          ) : (
            <div className='space-y-2'>
              <Label htmlFor={`${mode}-threshold`}>
                {t('Threshold balance')}
              </Label>
              <Input
                id={`${mode}-threshold`}
                inputMode='numeric'
                value={thresholdAmount}
                disabled={disabled}
                onChange={(event) => setThresholdAmount(event.target.value)}
              />
            </div>
          )}

          <Button
            type='button'
            className='w-full'
            disabled={disabled}
            onClick={handleSubmit}
          >
            {getModeTitle(mode, t)}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
