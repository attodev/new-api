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

function getModeTitle(mode: WalletAutoRechargeType, t: (key: string) => string) {
  return mode === 'scheduled' ? t('정기결제') : t('자동결제')
}

export function AutoRechargeCard({
  mode,
  policies,
  loading,
  processing,
  canManage,
  minTopup,
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

    setAmount(activePolicy.amount ? String(activePolicy.amount) : '')
    setThresholdAmount(
      activePolicy.threshold_amount
        ? String(activePolicy.threshold_amount)
        : ''
    )
    setIntervalUnit(activePolicy.interval_unit === 'day' ? 'day' : 'month')
    setIntervalValue(
      activePolicy.interval_value ? String(activePolicy.interval_value) : '1'
    )
    setChargeImmediately(activePolicy.charge_immediately !== false)
  }, [activePolicy])

  const disabled = loading || processing || !canManage

  const handleSubmit = async () => {
    const normalizedAmount = Math.floor(Number(amount))
    if (!Number.isFinite(normalizedAmount) || normalizedAmount < minTopup) {
      toast.error(
        t('최소 충전 금액은 {{amount}}입니다.', {
          amount: String(minTopup),
        })
      )
      return
    }

    const payload: WalletAutoRechargeRequest = { amount: normalizedAmount }

    if (mode === 'scheduled') {
      const normalizedInterval = Math.floor(Number(intervalValue))
      if (!Number.isFinite(normalizedInterval) || normalizedInterval <= 0) {
        toast.error(t('올바른 주기 값을 입력해주세요.'))
        return
      }
      payload.interval_unit = intervalUnit
      payload.interval_value = normalizedInterval
      payload.charge_immediately = chargeImmediately
      await onCreateScheduled(payload)
      return
    }

    const normalizedThreshold = Math.floor(Number(thresholdAmount))
    if (!Number.isFinite(normalizedThreshold) || normalizedThreshold < 0) {
      toast.error(t('올바른 기준 잔액을 입력해주세요.'))
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
              <div className='text-sm font-medium'>{t('현재 설정')}</div>
              {activePolicy ? (
                <>
                  <div className='text-muted-foreground text-sm'>
                    {t('충전 금액')}: {activePolicy.amount}
                  </div>
                  {mode === 'scheduled' ? (
                    <div className='text-muted-foreground text-sm'>
                      {t('결제 주기')}:{' '}
                      {activePolicy.interval_value || 1}
                      {activePolicy.interval_unit === 'day'
                        ? t('일')
                        : t('개월')}
                    </div>
                  ) : (
                    <div className='text-muted-foreground text-sm'>
                      {t('기준 잔액')}: {activePolicy.threshold_amount ?? 0}
                    </div>
                  )}
                  <div className='text-muted-foreground text-sm'>
                    {t('다음 결제')}: {formatTimestamp(activePolicy.next_charge_time)}
                  </div>
                  <div className='text-muted-foreground text-sm'>
                    {t('카드')}:{' '}
                    {[activePolicy.card_company, activePolicy.card_number_masked]
                      .filter(Boolean)
                      .join(' ') || '-'}
                  </div>
                </>
              ) : (
                <div className='text-muted-foreground text-sm'>
                  {t('설정 없음')}
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
                {t('해지')}
              </Button>
            ) : null}
          </div>
        </div>

        {!canManage ? (
          <div className='text-muted-foreground text-sm'>
            {t('관리 권한이 없습니다.')}
          </div>
        ) : null}

        <div className={cn('space-y-4', !canManage && 'opacity-60')}>
          <div className='space-y-2'>
            <Label htmlFor={`${mode}-amount`}>{t('충전 금액')}</Label>
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
                    {t('결제 주기')}
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
                      <SelectItem value='month'>{t('월')}</SelectItem>
                      <SelectItem value='day'>{t('일')}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className='space-y-2'>
                  <Label htmlFor={`${mode}-interval-value`}>
                    {t('주기 값')}
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
                  {t('즉시 첫 충전')}
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
              <Label htmlFor={`${mode}-threshold`}>{t('기준 잔액')}</Label>
              <Input
                id={`${mode}-threshold`}
                inputMode='numeric'
                value={thresholdAmount}
                disabled={disabled}
                onChange={(event) => setThresholdAmount(event.target.value)}
              />
            </div>
          )}

          <Button type='button' className='w-full' disabled={disabled} onClick={handleSubmit}>
            {getModeTitle(mode, t)}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
