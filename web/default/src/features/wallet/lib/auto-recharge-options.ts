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

export interface ThresholdOption {
  thresholdAmount: number
  preset: WalletAutoRechargePreset
}

export interface ThresholdAmountGroup {
  amount: number
  thresholds: ThresholdOption[]
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
  thresholdAmounts: number[]
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
  return 'Custom period'
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

export function groupThresholdPresetOptions(
  presets: WalletAutoRechargePreset[]
): ThresholdAmountGroup[] {
  const map = new Map<number, ThresholdAmountGroup>()

  for (const preset of [...presets]
    .filter((item) => item.enabled && item.type === 'threshold')
    .sort(byPresetOrder)) {
    const thresholdAmount = preset.threshold_amount ?? 0
    const group = map.get(preset.amount) ?? { amount: preset.amount, thresholds: [] }
    if (
      !group.thresholds.some(
        (item) => item.thresholdAmount === thresholdAmount
      )
    ) {
      group.thresholds.push({ thresholdAmount, preset })
    }
    map.set(preset.amount, group)
  }

  return [...map.values()]
    .sort((left, right) => left.amount - right.amount)
    .map((group) => ({
      ...group,
      thresholds: group.thresholds.sort(
        (left, right) => left.thresholdAmount - right.thresholdAmount
      ),
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
        thresholdGroups.map((group) => group.amount)
      ),
      thresholdAmounts: uniqueNumbers(
        thresholdGroups.flatMap((group) =>
          group.thresholds.map((item) => item.thresholdAmount)
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
    | 'interval_unit'
    | 'interval_value'
    | 'custom_seconds'
  >
) {
  if (preset.type === 'threshold') {
    return `threshold:${preset.amount}:${preset.threshold_amount ?? 0}`
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
    interval_unit: period.interval_unit,
    interval_value: period.interval_value,
    custom_seconds: period.custom_seconds,
    charge_immediately: state.chargeImmediately,
    sort_order: sortOrder,
    enabled: true,
  }
}

function thresholdRequest(
  amount: number,
  thresholdAmount: number,
  state: AdminThresholdOptionState,
  sortOrder: number
): WalletAutoRechargePresetRequest {
  return {
    type: 'threshold',
    target_scope: state.targetScope,
    name: `Auto ${amount} below ${thresholdAmount}`,
    description: '',
    amount,
    threshold_amount: thresholdAmount,
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
  const update: Array<{ id: number; request: WalletAutoRechargePresetRequest }> =
    []
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
    for (const thresholdAmount of uniqueNumbers(
      desired.threshold.thresholdAmounts
    )) {
      const request = thresholdRequest(
        amount,
        thresholdAmount,
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

  for (const preset of current.filter((item) => item.enabled).sort(byPresetOrder)) {
    if (!desiredByKey.has(presetComboKey(preset))) {
      disable.push(preset)
    }
  }

  return { create, update, disable }
}
