import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  formatQuota,
  parseQuotaFromDollars,
  quotaUnitsToDollars,
} from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import {
  createAdminWalletAutoRechargePreset,
  deleteAdminWalletAutoRechargePreset,
  isApiSuccess,
  listAdminWalletAutoRechargePresets,
  updateAdminWalletAutoRechargePreset,
} from '@/features/wallet/api'
import {
  buildAdminBalanceOptionState,
  buildPresetSavePlanFromBalanceThresholds,
  formatScheduledPeriodSummary,
  getScheduledPeriodLabelKey,
  type AdminAutoRechargeOptionState,
  type AdminScheduledPeriodOption,
  type ScheduledPeriodOption,
} from '@/features/wallet/lib/auto-recharge-options'
import type {
  WalletAutoRechargeIntervalUnit,
  WalletAutoRechargePreset,
  WalletAutoRechargePresetRequest,
  WalletAutoRechargeTargetScope,
  WalletAutoRechargeType,
} from '@/features/wallet/types'
import { SettingsSection } from '../components/settings-section'

const PRESET_QUERY_KEY = ['admin-wallet-auto-recharge-presets'] as const
type SummaryTranslator = (
  key: string,
  options?: Record<string, unknown>
) => string

const TARGET_SCOPE_OPTIONS: Array<{
  value: WalletAutoRechargeTargetScope
  label: string
}> = [
  { value: 'all', label: 'All targets' },
  { value: 'user', label: 'Users only' },
  { value: 'organization', label: 'Organizations only' },
]

export interface WalletAutoRechargePresetFormState {
  type: WalletAutoRechargeType
  target_scope: WalletAutoRechargeTargetScope
  name: string
  description: string
  amount: string
  threshold_amount: string
  threshold_quota: string
  interval_unit: WalletAutoRechargeIntervalUnit
  interval_value: string
  custom_seconds: string
  charge_immediately: boolean
  sort_order: string
  enabled: boolean
}

function createEmptyPresetFormState(): WalletAutoRechargePresetFormState {
  return {
    type: 'scheduled',
    target_scope: 'all',
    name: '',
    description: '',
    amount: '',
    threshold_amount: '',
    threshold_quota: '',
    interval_unit: 'month',
    interval_value: '1',
    custom_seconds: '',
    charge_immediately: true,
    sort_order: '0',
    enabled: true,
  }
}

function formatBalanceInput(value: number): string {
  if (!Number.isFinite(value)) return ''
  return String(Number(value.toFixed(4)))
}

export function toPresetFormState(
  preset?: WalletAutoRechargePreset | null
): WalletAutoRechargePresetFormState {
  if (!preset) return createEmptyPresetFormState()

  return {
    type: preset.type,
    target_scope: preset.target_scope,
    name: preset.name,
    description: preset.description ?? '',
    amount: String(preset.amount ?? ''),
    threshold_amount:
      preset.threshold_amount !== null && preset.threshold_amount !== undefined
        ? String(preset.threshold_amount)
        : '',
    threshold_quota:
      preset.threshold_quota !== null && preset.threshold_quota !== undefined
        ? formatBalanceInput(quotaUnitsToDollars(preset.threshold_quota))
        : '',
    interval_unit: preset.interval_unit ?? 'month',
    interval_value:
      preset.interval_value !== null && preset.interval_value !== undefined
        ? String(preset.interval_value)
        : '1',
    custom_seconds:
      preset.custom_seconds !== null && preset.custom_seconds !== undefined
        ? String(preset.custom_seconds)
        : '',
    charge_immediately: preset.charge_immediately !== false,
    sort_order:
      preset.sort_order !== null && preset.sort_order !== undefined
        ? String(preset.sort_order)
        : '0',
    enabled: preset.enabled,
  }
}

export function normalizePresetForm(
  form: WalletAutoRechargePresetFormState
): WalletAutoRechargePresetRequest {
  const amount = Number(form.amount || 0)
  const sortOrder = Number(form.sort_order || 0)

  if (form.type === 'scheduled') {
    const intervalUnit = form.interval_unit
    const intervalValue = Number(form.interval_value || 0)
    const customSeconds =
      intervalUnit === 'custom' ? Number(form.custom_seconds || 0) : 0

    return {
      type: form.type,
      target_scope: form.target_scope,
      name: form.name.trim(),
      description: form.description.trim(),
      amount,
      threshold_amount: 0,
      threshold_quota: 0,
      interval_unit: intervalUnit,
      interval_value: intervalValue,
      custom_seconds: customSeconds,
      charge_immediately: form.charge_immediately,
      sort_order: sortOrder,
      enabled: form.enabled,
    }
  }

  return {
    type: form.type,
    target_scope: form.target_scope,
    name: form.name.trim(),
    description: form.description.trim(),
    amount,
    threshold_amount: 0,
    threshold_quota: parseQuotaFromDollars(Number(form.threshold_quota || 0)),
    interval_unit: 'month',
    interval_value: 1,
    custom_seconds: 0,
    charge_immediately: false,
    sort_order: sortOrder,
    enabled: form.enabled,
  }
}

export function parseOptionAmountList(value: string): number[] {
  return [
    ...new Set(
      value
        .split(/[\s,]+/)
        .map((item) => Math.floor(Number(item.trim())))
        .filter((item) => Number.isFinite(item) && item >= 0)
    ),
  ].sort((left, right) => left - right)
}

export function parseOptionBalanceList(value: string): number[] {
  return [
    ...new Set(
      value
        .split(/[\s,]+/)
        .map((item) => Number(item.trim()))
        .filter((item) => Number.isFinite(item) && item >= 0)
    ),
  ].sort((left, right) => left - right)
}

export function formatOptionAmountList(values: number[]): string {
  return [
    ...new Set(
      values
        .map((item) => Math.floor(Number(item)))
        .filter((item) => Number.isFinite(item) && item >= 0)
    ),
  ]
    .sort((left, right) => left - right)
    .join(', ')
}

export function formatOptionBalanceList(values: number[]): string {
  return [
    ...new Set(
      values
        .map((item) => Number(item))
        .filter((item) => Number.isFinite(item) && item >= 0)
    ),
  ]
    .sort((left, right) => left - right)
    .map(formatBalanceInput)
    .join(', ')
}

type OptionAmountDrafts = Record<string, string>

export function updateOptionAmountDrafts(
  drafts: OptionAmountDrafts,
  key: string,
  value: string
): OptionAmountDrafts {
  return { ...drafts, [key]: value }
}

export function getOptionAmountDraftValue(
  drafts: OptionAmountDrafts,
  key: string,
  values: number[]
): string {
  return Object.prototype.hasOwnProperty.call(drafts, key)
    ? drafts[key]
    : formatOptionAmountList(values)
}

export function getOptionBalanceDraftValue(
  drafts: OptionAmountDrafts,
  key: string,
  values: number[]
): string {
  return Object.prototype.hasOwnProperty.call(drafts, key)
    ? drafts[key]
    : formatOptionBalanceList(values)
}

function makeQuickPeriod(
  kind: 'daily' | 'weekly' | 'monthly'
): ScheduledPeriodOption {
  if (kind === 'daily') {
    return {
      key: 'daily',
      kind,
      interval_unit: 'day',
      interval_value: 1,
      custom_seconds: 0,
    }
  }
  if (kind === 'weekly') {
    return {
      key: 'weekly',
      kind,
      interval_unit: 'day',
      interval_value: 7,
      custom_seconds: 0,
    }
  }
  return {
    key: 'monthly',
    kind,
    interval_unit: 'month',
    interval_value: 1,
    custom_seconds: 0,
  }
}

function makeCustomPeriod(customSeconds: number): ScheduledPeriodOption {
  const seconds = Math.max(Math.floor(customSeconds), 1)
  return {
    key: `custom:1:${seconds}`,
    kind: 'custom',
    interval_unit: 'custom',
    interval_value: 1,
    custom_seconds: seconds,
  }
}

export function canAddScheduledPeriod(periods: AdminScheduledPeriodOption[]) {
  return periods.length === 0
}

function getIntervalSummary(
  preset: WalletAutoRechargePreset,
  t: SummaryTranslator = (key) => key
) {
  if (preset.interval_unit === 'custom') {
    return t('{{seconds}}s', { seconds: preset.custom_seconds ?? 0 })
  }

  const intervalUnitLabel =
    preset.interval_unit === 'day' ? t('Day(s)') : t('Month(s)')

  return t('{{value}} {{unit}}', {
    value: preset.interval_value || 1,
    unit: intervalUnitLabel,
  })
}

export function getPresetSummary(
  preset: WalletAutoRechargePreset,
  t: SummaryTranslator = (key) => key
) {
  if (preset.type === 'scheduled') {
    return t('{{amount}} / {{interval}}', {
      amount: preset.amount,
      interval: getIntervalSummary(preset, t),
    })
  }
  return t('Below {{threshold}} -> {{amount}}', {
    threshold: formatQuota(preset.threshold_quota ?? 0),
    amount: preset.amount,
  })
}

export function WalletAutoRechargePresetsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [optionState, setOptionState] = useState<AdminAutoRechargeOptionState>(
    () => buildAdminBalanceOptionState([])
  )
  const [amountDrafts, setAmountDrafts] = useState<OptionAmountDrafts>({})

  const presetQuery = useQuery({
    queryKey: PRESET_QUERY_KEY,
    queryFn: listAdminWalletAutoRechargePresets,
  })

  const presets = useMemo(
    () =>
      [...(presetQuery.data?.data ?? [])].sort((left, right) => {
        if (left.type !== right.type) {
          return left.type.localeCompare(right.type)
        }
        const sortOrder = (left.sort_order ?? 0) - (right.sort_order ?? 0)
        if (sortOrder !== 0) return sortOrder
        return left.id - right.id
      }),
    [presetQuery.data?.data]
  )

  useEffect(() => {
    setOptionState(buildAdminBalanceOptionState(presets))
    setAmountDrafts({})
  }, [presets])

  const createMutation = useMutation({
    mutationFn: createAdminWalletAutoRechargePreset,
  })

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      payload,
    }: {
      id: number
      payload: WalletAutoRechargePresetRequest
    }) => updateAdminWalletAutoRechargePreset(id, payload),
  })

  const deleteMutation = useMutation({
    mutationFn: deleteAdminWalletAutoRechargePreset,
  })

  const isSaving =
    createMutation.isPending ||
    updateMutation.isPending ||
    deleteMutation.isPending

  const setScheduledTargetScope = (scope: WalletAutoRechargeTargetScope) => {
    setOptionState((current) => ({
      ...current,
      scheduled: { ...current.scheduled, targetScope: scope },
    }))
  }

  const setThresholdTargetScope = (scope: WalletAutoRechargeTargetScope) => {
    setOptionState((current) => ({
      ...current,
      threshold: { ...current.threshold, targetScope: scope },
    }))
  }

  const addScheduledPeriod = (period: ScheduledPeriodOption) => {
    setOptionState((current) => {
      if (
        current.scheduled.periods.some((item) => item.period.key === period.key)
      ) {
        return current
      }
      return {
        ...current,
        scheduled: {
          ...current.scheduled,
          periods: [...current.scheduled.periods, { period, amounts: [] }],
        },
      }
    })
  }

  const removeScheduledPeriod = (periodKey: string) => {
    setOptionState((current) => ({
      ...current,
      scheduled: {
        ...current.scheduled,
        periods: current.scheduled.periods.filter(
          (item) => item.period.key !== periodKey
        ),
      },
    }))
  }

  const updateScheduledPeriodAmounts = (periodKey: string, value: string) => {
    setAmountDrafts((current) =>
      updateOptionAmountDrafts(current, `scheduled:${periodKey}`, value)
    )
    const amounts = parseOptionAmountList(value).filter((item) => item > 0)
    setOptionState((current) => ({
      ...current,
      scheduled: {
        ...current.scheduled,
        periods: current.scheduled.periods.map((item) =>
          item.period.key === periodKey ? { ...item, amounts } : item
        ),
      },
    }))
  }

  const updateCustomPeriodSeconds = (periodKey: string, value: string) => {
    const seconds = Math.max(Math.floor(Number(value || 0)), 1)
    setOptionState((current) => ({
      ...current,
      scheduled: {
        ...current.scheduled,
        periods: current.scheduled.periods.map((item) =>
          item.period.key === periodKey
            ? { ...item, period: makeCustomPeriod(seconds) }
            : item
        ),
      },
    }))
  }

  const handleSaveOptions = async () => {
    const plan = buildPresetSavePlanFromBalanceThresholds(presets, optionState)
    try {
      for (const payload of plan.create) {
        const response = await createMutation.mutateAsync(payload)
        if (!isApiSuccess(response)) {
          throw new Error(response.message || t('Failed to create preset'))
        }
      }
      for (const item of plan.update) {
        const response = await updateMutation.mutateAsync({
          id: item.id,
          payload: item.request,
        })
        if (!isApiSuccess(response)) {
          throw new Error(response.message || t('Failed to update preset'))
        }
      }
      for (const preset of plan.disable) {
        const response = await deleteMutation.mutateAsync(preset.id)
        if (!isApiSuccess(response)) {
          throw new Error(response.message || t('Failed to delete preset'))
        }
      }
      await queryClient.invalidateQueries({ queryKey: PRESET_QUERY_KEY })
      setAmountDrafts({})
      toast.success(t('Options saved'))
    } catch (error) {
      await queryClient.invalidateQueries({ queryKey: PRESET_QUERY_KEY })
      toast.error(
        error instanceof Error ? error.message : t('Failed to update preset')
      )
    }
  }

  const canAddPeriod = canAddScheduledPeriod(optionState.scheduled.periods)

  return (
    <SettingsSection title={t('Auto Recharge Presets')}>
      <div className='flex items-center justify-between gap-3'>
        <div className='flex flex-col gap-1'>
          <div className='text-sm font-medium'>{t('Admin preset library')}</div>
          <div className='text-muted-foreground text-sm'>
            {t(
              'Manage the scheduled and threshold options shown in wallet auto recharge flows.'
            )}
          </div>
        </div>
        <Button type='button' disabled={isSaving} onClick={handleSaveOptions}>
          {isSaving ? <Spinner data-icon='inline-start' /> : null}
          {t('Save options')}
        </Button>
      </div>

      <Separator />

      {presetQuery.isLoading ? (
        <div className='flex flex-col gap-4'>
          <Skeleton className='h-40 w-full rounded-lg' />
          <Skeleton className='h-40 w-full rounded-lg' />
        </div>
      ) : (
        <div className='flex flex-col gap-4'>
          <Card>
            <CardHeader>
              <CardTitle className='text-base'>
                {t('Scheduled recharge options')}
              </CardTitle>
            </CardHeader>
            <CardContent className='space-y-4'>
              <div className='grid gap-4 sm:grid-cols-2'>
                <div className='space-y-2'>
                  <div className='text-sm font-medium'>{t('Target scope')}</div>
                  <Select
                    items={TARGET_SCOPE_OPTIONS}
                    value={optionState.scheduled.targetScope}
                    onValueChange={(value) =>
                      setScheduledTargetScope(
                        value as WalletAutoRechargeTargetScope
                      )
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder={t('Select target scope')} />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {TARGET_SCOPE_OPTIONS.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {t(option.label)}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
                <div className='flex items-center justify-between gap-4 rounded-lg border p-4'>
                  <div className='flex flex-col gap-1'>
                    <div className='text-sm font-medium'>
                      {t('Charge immediately')}
                    </div>
                    <div className='text-muted-foreground text-sm'>
                      {t(
                        'Start the first scheduled charge as soon as the preset is selected.'
                      )}
                    </div>
                  </div>
                  <Switch
                    checked={optionState.scheduled.chargeImmediately}
                    onCheckedChange={(checked) =>
                      setOptionState((current) => ({
                        ...current,
                        scheduled: {
                          ...current.scheduled,
                          chargeImmediately: checked,
                        },
                      }))
                    }
                  />
                </div>
              </div>

              <div className='flex flex-wrap gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={!canAddPeriod}
                  onClick={() => addScheduledPeriod(makeQuickPeriod('monthly'))}
                >
                  <Plus data-icon='inline-start' />
                  {t('Add monthly')}
                </Button>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={!canAddPeriod}
                  onClick={() => addScheduledPeriod(makeCustomPeriod(86400))}
                >
                  <Plus data-icon='inline-start' />
                  {t('Add test period')}
                </Button>
              </div>

              <div className='space-y-3'>
                {optionState.scheduled.periods.length === 0 ? (
                  <Empty className='rounded-lg border'>
                    <EmptyHeader>
                      <EmptyMedia variant='icon'>
                        <Plus />
                      </EmptyMedia>
                      <EmptyTitle>{t('No scheduled periods')}</EmptyTitle>
                      <EmptyDescription>
                        {t('Add a period and assign recharge amounts.')}
                      </EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                ) : null}
                {optionState.scheduled.periods.map((item) => (
                  <div
                    key={item.period.key}
                    className='grid gap-3 rounded-lg border p-3 md:grid-cols-[180px_1fr_auto]'
                  >
                    <div className='space-y-1'>
                      <div className='text-sm font-medium'>
                        {t(getScheduledPeriodLabelKey(item.period))}
                      </div>
                      <div className='text-muted-foreground text-xs'>
                        {formatScheduledPeriodSummary(item.period, t)}
                      </div>
                    </div>
                    <div className='grid gap-2 sm:grid-cols-2'>
                      <Input
                        value={getOptionAmountDraftValue(
                          amountDrafts,
                          `scheduled:${item.period.key}`,
                          item.amounts
                        )}
                        onChange={(event) =>
                          updateScheduledPeriodAmounts(
                            item.period.key,
                            event.target.value
                          )
                        }
                        placeholder={t('Recharge amounts')}
                      />
                      {item.period.kind === 'custom' ? (
                        <Input
                          type='number'
                          min={1}
                          value={item.period.custom_seconds}
                          onChange={(event) =>
                            updateCustomPeriodSeconds(
                              item.period.key,
                              event.target.value
                            )
                          }
                          placeholder={t('Custom seconds')}
                        />
                      ) : null}
                    </div>
                    <Button
                      type='button'
                      variant='outline'
                      size='icon'
                      onClick={() => removeScheduledPeriod(item.period.key)}
                      aria-label={t('Remove')}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className='text-base'>
                {t('Auto recharge options')}
              </CardTitle>
            </CardHeader>
            <CardContent className='space-y-4'>
              <div className='space-y-2'>
                <div className='text-sm font-medium'>{t('Target scope')}</div>
                <Select
                  items={TARGET_SCOPE_OPTIONS}
                  value={optionState.threshold.targetScope}
                  onValueChange={(value) =>
                    setThresholdTargetScope(
                      value as WalletAutoRechargeTargetScope
                    )
                  }
                >
                  <SelectTrigger>
                    <SelectValue placeholder={t('Select target scope')} />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {TARGET_SCOPE_OPTIONS.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {t(option.label)}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>

              <div className='grid gap-4 sm:grid-cols-2'>
                <div className='space-y-2'>
                  <div className='text-sm font-medium'>
                    {t('Recharge amounts')}
                  </div>
                  <Input
                    value={getOptionAmountDraftValue(
                      amountDrafts,
                      'threshold:recharge',
                      optionState.threshold.rechargeAmounts
                    )}
                    onChange={(event) => {
                      const value = event.target.value
                      setAmountDrafts((current) =>
                        updateOptionAmountDrafts(
                          current,
                          'threshold:recharge',
                          value
                        )
                      )
                      setOptionState((current) => ({
                        ...current,
                        threshold: {
                          ...current.threshold,
                          rechargeAmounts: parseOptionAmountList(value).filter(
                            (item) => item > 0
                          ),
                        },
                      }))
                    }}
                    placeholder='10000, 30000, 50000'
                  />
                </div>
                <div className='space-y-2'>
                  <div className='text-sm font-medium'>
                    {t('Threshold balances')}
                  </div>
                  <Input
                    value={getOptionBalanceDraftValue(
                      amountDrafts,
                      'threshold:quota',
                      optionState.threshold.thresholdQuotas
                    )}
                    onChange={(event) => {
                      const value = event.target.value
                      setAmountDrafts((current) =>
                        updateOptionAmountDrafts(
                          current,
                          'threshold:quota',
                          value
                        )
                      )
                      setOptionState((current) => ({
                        ...current,
                        threshold: {
                          ...current.threshold,
                          thresholdQuotas: parseOptionBalanceList(value),
                        },
                      }))
                    }}
                    placeholder='1, 5, 10'
                  />
                </div>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </SettingsSection>
  )
}
