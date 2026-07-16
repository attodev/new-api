import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'
import type {
  WalletAutoRechargeIntervalUnit,
  WalletAutoRechargePreset,
  WalletAutoRechargePresetRequest,
  WalletAutoRechargeTargetScope,
} from '../types'

export type ScheduledPeriodKind = 'daily' | 'weekly' | 'monthly' | 'custom'

export interface ScheduledPeriodOption {
  key: string
  kind: ScheduledPeriodKind
  interval_unit: WalletAutoRechargeIntervalUnit
  interval_value: number
  custom_seconds: number
}

export interface ScheduledAmountOption {
  amount: number
  preset: WalletAutoRechargePreset
}

export interface ScheduledPresetGroup {
  period: ScheduledPeriodOption
  amounts: ScheduledAmountOption[]
}

export interface ThresholdRechargeAmountOption {
  amount: number
  preset: WalletAutoRechargePreset
}

export interface ThresholdQuotaGroup {
  thresholdQuota: number
  amounts: ThresholdRechargeAmountOption[]
}

export interface AdminScheduledPeriodOption {
  period: ScheduledPeriodOption
  amounts: number[]
}

export interface AdminScheduledOptionState {
  targetScope: WalletAutoRechargeTargetScope
  chargeImmediately: boolean
  periods: AdminScheduledPeriodOption[]
}

export interface AdminThresholdOptionState {
  targetScope: WalletAutoRechargeTargetScope
  rechargeAmounts: number[]
  thresholdQuotas: number[]
}

export interface AdminAutoRechargeOptionState {
  scheduled: AdminScheduledOptionState
  threshold: AdminThresholdOptionState
}

export interface PresetSavePlan {
  create: WalletAutoRechargePresetRequest[]
  update: Array<{ id: number; request: WalletAutoRechargePresetRequest }>
  disable: WalletAutoRechargePreset[]
}

const byPresetOrder = (
  left: WalletAutoRechargePreset,
  right: WalletAutoRechargePreset
) => {
  const sortOrder = (left.sort_order ?? 0) - (right.sort_order ?? 0)
  if (sortOrder !== 0) return sortOrder
  return left.id - right.id
}

const uniqueNumbers = (values: number[]) =>
  [...new Set(values.filter((value) => Number.isFinite(value)))]
    .filter((value) => value >= 0)
    .sort((left, right) => left - right)

export function getScheduledPeriodOption(
  preset: Pick<
    WalletAutoRechargePreset,
    'interval_unit' | 'interval_value' | 'custom_seconds'
  >
): ScheduledPeriodOption {
  const intervalUnit = preset.interval_unit ?? 'month'
  const intervalValue = preset.interval_value || 1
  const customSeconds = preset.custom_seconds || 0

  let kind: ScheduledPeriodKind = 'custom'
  if (intervalUnit === 'day' && intervalValue === 1) kind = 'daily'
  if (intervalUnit === 'day' && intervalValue === 7) kind = 'weekly'
  if (intervalUnit === 'month' && intervalValue === 1) kind = 'monthly'

  const key =
    kind === 'custom'
      ? `${intervalUnit}:${intervalValue}:${customSeconds}`
      : kind

  return {
    key,
    kind,
    interval_unit: intervalUnit,
    interval_value: intervalValue,
    custom_seconds: customSeconds,
  }
}

export function getScheduledPeriodKey(preset: WalletAutoRechargePreset) {
  return getScheduledPeriodOption(preset).key
}

export function getScheduledPeriodLabelKey(period: ScheduledPeriodOption) {
  if (period.kind === 'daily') return 'Daily'
  if (period.kind === 'weekly') return 'Weekly'
  if (period.kind === 'monthly') return 'Monthly'
  return 'Test period'
}

export function formatScheduledPeriodSummary(
  period: ScheduledPeriodOption,
  t: (key: string, options?: Record<string, unknown>) => string = (key) => key
) {
  if (period.kind === 'custom') {
    return t('{{seconds}}s', { seconds: period.custom_seconds })
  }
  if (period.kind === 'daily') return t('Every day')
  if (period.kind === 'weekly') return t('Every week')
  if (period.kind === 'monthly') return t('Every month')
  return t('{{value}} {{unit}}', {
    value: period.interval_value,
    unit: period.interval_unit,
  })
}

export function groupScheduledPresetOptions(
  presets: WalletAutoRechargePreset[]
): ScheduledPresetGroup[] {
  const map = new Map<string, ScheduledPresetGroup>()

  for (const preset of [...presets]
    .filter((item) => item.enabled && item.type === 'scheduled')
    .sort(byPresetOrder)) {
    const period = getScheduledPeriodOption(preset)
    const group = map.get(period.key) ?? { period, amounts: [] }
    if (!group.amounts.some((item) => item.amount === preset.amount)) {
      group.amounts.push({ amount: preset.amount, preset })
    }
    map.set(period.key, group)
  }

  return [...map.values()].map((group) => ({
    ...group,
    amounts: group.amounts.sort((left, right) => left.amount - right.amount),
  }))
}

export function groupMonthlyScheduledPresetOptions(
  presets: WalletAutoRechargePreset[]
): ScheduledPresetGroup[] {
  return groupScheduledPresetOptions(presets).filter(
    (group) => group.period.kind === 'monthly'
  )
}

function isManagedScheduledPeriod(period: ScheduledPeriodOption) {
  return period.kind === 'monthly' || period.kind === 'custom'
}

export function groupUserScheduledPresetOptions(
  presets: WalletAutoRechargePreset[]
): ScheduledPresetGroup[] {
  return groupScheduledPresetOptions(presets).filter((group) =>
    isManagedScheduledPeriod(group.period)
  )
}

export function groupThresholdPresetOptions(
  presets: WalletAutoRechargePreset[]
): ThresholdQuotaGroup[] {
  const map = new Map<number, ThresholdQuotaGroup>()

  for (const preset of [...presets]
    .filter(
      (item) =>
        item.enabled &&
        item.type === 'threshold' &&
        (item.threshold_quota ?? 0) > 0
    )
    .sort(byPresetOrder)) {
    const thresholdQuota = preset.threshold_quota ?? 0
    const group = map.get(thresholdQuota) ?? { thresholdQuota, amounts: [] }
    if (!group.amounts.some((item) => item.amount === preset.amount)) {
      group.amounts.push({ amount: preset.amount, preset })
    }
    map.set(thresholdQuota, group)
  }

  return [...map.values()]
    .sort((left, right) => left.thresholdQuota - right.thresholdQuota)
    .map((group) => ({
      ...group,
      amounts: group.amounts.sort((left, right) => left.amount - right.amount),
    }))
}

function firstScope(
  presets: WalletAutoRechargePreset[],
  fallback: WalletAutoRechargeTargetScope = 'all'
) {
  return [...presets].sort(byPresetOrder)[0]?.target_scope ?? fallback
}

function periodSortValue(period: ScheduledPeriodOption) {
  if (period.kind === 'daily') return 1
  if (period.kind === 'weekly') return 2
  if (period.kind === 'monthly') return 3
  return 4
}

export function buildAdminOptionState(
  presets: WalletAutoRechargePreset[]
): AdminAutoRechargeOptionState {
  const scheduledPresets = presets.filter(
    (preset) => preset.type === 'scheduled' && preset.enabled
  )
  const thresholdPresets = presets.filter(
    (preset) => preset.type === 'threshold' && preset.enabled
  )
  const scheduledGroups = groupScheduledPresetOptions(scheduledPresets)
  const thresholdGroups = groupThresholdPresetOptions(thresholdPresets)

  return {
    scheduled: {
      targetScope: firstScope(scheduledPresets),
      chargeImmediately:
        scheduledPresets.sort(byPresetOrder)[0]?.charge_immediately !== false,
      periods: scheduledGroups
        .filter((group) => isManagedScheduledPeriod(group.period))
        .map((group) => ({
          period: group.period,
          amounts: uniqueNumbers(group.amounts.map((item) => item.amount)),
        }))
        .sort((left, right) => {
          const kindSort =
            periodSortValue(left.period) - periodSortValue(right.period)
          if (kindSort !== 0) return kindSort
          return left.period.key.localeCompare(right.period.key)
        }),
    },
    threshold: {
      targetScope: firstScope(thresholdPresets),
      rechargeAmounts: uniqueNumbers(
        thresholdGroups.flatMap((group) =>
          group.amounts.map((item) => item.amount)
        )
      ),
      thresholdQuotas: uniqueNumbers(
        thresholdGroups.map((group) => group.thresholdQuota)
      ),
    },
  }
}

export function buildAdminBalanceOptionState(
  presets: WalletAutoRechargePreset[]
): AdminAutoRechargeOptionState {
  const state = buildAdminOptionState(presets)
  return {
    ...state,
    threshold: {
      ...state.threshold,
      thresholdQuotas: uniqueNumbers(
        state.threshold.thresholdQuotas.map((quota) =>
          quotaUnitsToDollars(quota)
        )
      ),
    },
  }
}

function presetComboKey(
  preset: Pick<
    WalletAutoRechargePreset,
    | 'type'
    | 'amount'
    | 'threshold_amount'
    | 'threshold_quota'
    | 'interval_unit'
    | 'interval_value'
    | 'custom_seconds'
  >
) {
  if (preset.type === 'threshold') {
    return `threshold:${preset.amount}:${preset.threshold_quota ?? 0}`
  }
  const period = getScheduledPeriodOption(preset)
  return `scheduled:${period.key}:${preset.amount}`
}

function scheduledRequest(
  period: ScheduledPeriodOption,
  amount: number,
  state: AdminScheduledOptionState,
  sortOrder: number
): WalletAutoRechargePresetRequest {
  return {
    type: 'scheduled',
    target_scope: state.targetScope,
    name: `${getScheduledPeriodLabelKey(period)} ${amount}`,
    description: '',
    amount,
    threshold_amount: 0,
    threshold_quota: 0,
    interval_unit: period.interval_unit,
    interval_value: period.interval_value,
    custom_seconds: period.custom_seconds,
    charge_immediately: period.kind === 'custom' && state.chargeImmediately,
    sort_order: sortOrder,
    enabled: true,
  }
}

function thresholdRequest(
  amount: number,
  thresholdQuota: number,
  state: AdminThresholdOptionState,
  sortOrder: number
): WalletAutoRechargePresetRequest {
  return {
    type: 'threshold',
    target_scope: state.targetScope,
    name: `Auto ${amount} below quota ${thresholdQuota}`,
    description: '',
    amount,
    threshold_amount: 0,
    threshold_quota: thresholdQuota,
    interval_unit: 'month',
    interval_value: 1,
    custom_seconds: 0,
    charge_immediately: false,
    sort_order: sortOrder,
    enabled: true,
  }
}

export function buildPresetSavePlan(
  current: WalletAutoRechargePreset[],
  desired: AdminAutoRechargeOptionState
): PresetSavePlan {
  const create: WalletAutoRechargePresetRequest[] = []
  const update: Array<{
    id: number
    request: WalletAutoRechargePresetRequest
  }> = []
  const disable: WalletAutoRechargePreset[] = []
  const desiredByKey = new Map<string, WalletAutoRechargePresetRequest>()
  let sortOrder = 0

  for (const period of desired.scheduled.periods) {
    for (const amount of uniqueNumbers(period.amounts).filter(
      (value) => value > 0
    )) {
      const request = scheduledRequest(
        period.period,
        amount,
        desired.scheduled,
        sortOrder++
      )
      desiredByKey.set(presetComboKey(request), request)
    }
  }

  for (const amount of uniqueNumbers(desired.threshold.rechargeAmounts).filter(
    (value) => value > 0
  )) {
    for (const thresholdQuota of uniqueNumbers(
      desired.threshold.thresholdQuotas
    ).filter((value) => value > 0)) {
      const request = thresholdRequest(
        amount,
        thresholdQuota,
        desired.threshold,
        sortOrder++
      )
      desiredByKey.set(presetComboKey(request), request)
    }
  }

  const currentByKey = new Map<string, WalletAutoRechargePreset>()
  for (const preset of [...current].sort(byPresetOrder)) {
    const key = presetComboKey(preset)
    if (!currentByKey.has(key)) {
      currentByKey.set(key, preset)
    }
  }

  for (const [key, request] of desiredByKey) {
    const existing = currentByKey.get(key)
    if (existing) {
      update.push({ id: existing.id, request })
    } else {
      create.push(request)
    }
  }

  for (const preset of current
    .filter((item) => item.enabled)
    .sort(byPresetOrder)) {
    if (!desiredByKey.has(presetComboKey(preset))) {
      disable.push(preset)
    }
  }

  return { create, update, disable }
}

export function buildPresetSavePlanFromBalanceThresholds(
  current: WalletAutoRechargePreset[],
  desired: AdminAutoRechargeOptionState
): PresetSavePlan {
  return buildPresetSavePlan(current, {
    ...desired,
    threshold: {
      ...desired.threshold,
      thresholdQuotas: uniqueNumbers(
        desired.threshold.thresholdQuotas.map((balance) =>
          parseQuotaFromDollars(balance)
        )
      ),
    },
  })
}
