import { describe, expect, test } from 'bun:test'
import {
  buildAdminOptionState,
  buildPresetSavePlan,
  groupScheduledPresetOptions,
  groupThresholdPresetOptions,
} from './auto-recharge-options'
import type { WalletAutoRechargePreset } from '../types'

const preset = (
  patch: Partial<WalletAutoRechargePreset>
): WalletAutoRechargePreset => ({
  id: patch.id ?? 1,
  type: patch.type ?? 'scheduled',
  target_scope: patch.target_scope ?? 'all',
  name: patch.name ?? 'preset',
  description: patch.description ?? '',
  amount: patch.amount ?? 10000,
  threshold_amount: patch.threshold_amount ?? 0,
  threshold_quota: patch.threshold_quota ?? 0,
  interval_unit: patch.interval_unit ?? 'month',
  interval_value: patch.interval_value ?? 1,
  custom_seconds: patch.custom_seconds ?? 0,
  charge_immediately: patch.charge_immediately ?? true,
  sort_order: patch.sort_order ?? 0,
  enabled: patch.enabled ?? true,
})

describe('auto recharge option helpers', () => {
  test('groups scheduled presets by period and keeps first duplicate by sort order', () => {
    const groups = groupScheduledPresetOptions([
      preset({
        id: 2,
        amount: 30000,
        interval_unit: 'month',
        interval_value: 1,
        sort_order: 2,
      }),
      preset({
        id: 1,
        amount: 10000,
        interval_unit: 'month',
        interval_value: 1,
        sort_order: 1,
      }),
      preset({
        id: 3,
        amount: 10000,
        interval_unit: 'month',
        interval_value: 1,
        sort_order: 3,
      }),
      preset({
        id: 4,
        amount: 5000,
        interval_unit: 'day',
        interval_value: 7,
        sort_order: 4,
      }),
    ])

    expect(groups.map((group) => group.period.kind)).toEqual([
      'monthly',
      'weekly',
    ])
    expect(
      groups[0].amounts.map((amount) => [amount.amount, amount.preset.id])
    ).toEqual([
      [10000, 1],
      [30000, 2],
    ])
  })

  test('groups threshold presets by quota before recharge amount', () => {
    const groups = groupThresholdPresetOptions([
      preset({
        id: 1,
        type: 'threshold',
        amount: 10000,
        threshold_quota: 500000,
      }),
      preset({
        id: 2,
        type: 'threshold',
        amount: 30000,
        threshold_quota: 500000,
      }),
      preset({
        id: 3,
        type: 'threshold',
        amount: 10000,
        threshold_quota: 1000000,
      }),
    ])

    expect(groups.map((group) => group.thresholdQuota)).toEqual([
      500000,
      1000000,
    ])
    expect(
      groups[0].amounts.map((item) => [item.amount, item.preset.id])
    ).toEqual([
      [10000, 1],
      [30000, 2],
    ])
  })

  test('ignores zero quota threshold presets', () => {
    const groups = groupThresholdPresetOptions([
      preset({
        id: 1,
        type: 'threshold',
        amount: 10000,
        threshold_amount: 5000,
        threshold_quota: 0,
      }),
      preset({
        id: 2,
        type: 'threshold',
        amount: 30000,
        threshold_quota: 500000,
      }),
    ])

    expect(groups.map((group) => group.thresholdQuota)).toEqual([500000])
    expect(groups[0].amounts.map((item) => item.preset.id)).toEqual([2])
  })

  test('builds admin option state from existing presets', () => {
    const state = buildAdminOptionState([
      preset({
        id: 1,
        type: 'scheduled',
        amount: 10000,
        interval_unit: 'day',
        interval_value: 1,
      }),
      preset({
        id: 2,
        type: 'threshold',
        amount: 30000,
        threshold_quota: 500000,
      }),
    ])

    expect(state.scheduled.periods[0].period.kind).toBe('daily')
    expect(state.scheduled.periods[0].amounts).toEqual([10000])
    expect(state.threshold.rechargeAmounts).toEqual([30000])
    expect(state.threshold.thresholdQuotas).toEqual([500000])
  })

  test('builds admin option state with threshold quota values', () => {
    const state = buildAdminOptionState([
      preset({
        id: 1,
        type: 'threshold',
        amount: 30000,
        threshold_quota: 500000,
      }),
    ])

    expect(state.threshold.rechargeAmounts).toEqual([30000])
    expect(state.threshold.thresholdQuotas).toEqual([500000])
  })

  test('plans create, update, and disable operations for option save', () => {
    const current = [
      preset({
        id: 1,
        type: 'scheduled',
        amount: 10000,
        interval_unit: 'month',
        interval_value: 1,
        enabled: true,
      }),
      preset({
        id: 2,
        type: 'threshold',
        amount: 30000,
        threshold_quota: 500000,
        enabled: true,
      }),
    ]
    const desired = buildAdminOptionState(current)
    desired.scheduled.periods[0].amounts = [10000, 50000]
    desired.threshold.thresholdQuotas = [1000000]

    const plan = buildPresetSavePlan(current, desired)

    expect(
      plan.create.map((item) => [
        item.type,
        item.amount,
        item.threshold_amount,
        item.threshold_quota,
      ])
    ).toEqual([
      ['scheduled', 50000, 0, 0],
      ['threshold', 30000, 0, 1000000],
    ])
    expect(plan.update.map((item) => item.id)).toEqual([1])
    expect(plan.disable.map((item) => item.id)).toEqual([2])
  })

  test('save plan writes threshold_quota and clears threshold_amount', () => {
    const plan = buildPresetSavePlan([], {
      scheduled: { targetScope: 'all', chargeImmediately: true, periods: [] },
      threshold: {
        targetScope: 'all',
        rechargeAmounts: [10000],
        thresholdQuotas: [500000],
      },
    })

    expect(plan.create[0]).toMatchObject({
      type: 'threshold',
      amount: 10000,
      threshold_amount: 0,
      threshold_quota: 500000,
    })
  })

  test('save plan skips non-positive threshold quotas when creating presets', () => {
    const plan = buildPresetSavePlan([], {
      scheduled: { targetScope: 'all', chargeImmediately: true, periods: [] },
      threshold: {
        targetScope: 'all',
        rechargeAmounts: [10000],
        thresholdQuotas: [0, 500000],
      },
    })

    expect(plan.create).toHaveLength(1)
    expect(plan.create[0]).toMatchObject({
      type: 'threshold',
      amount: 10000,
      threshold_amount: 0,
      threshold_quota: 500000,
    })
  })
})
