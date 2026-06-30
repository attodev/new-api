import { describe, expect, test } from 'bun:test'
import {
  formatOptionAmountList,
  getOptionAmountDraftValue,
  getPresetSummary,
  normalizePresetForm,
  parseOptionAmountList,
  updateOptionAmountDrafts,
} from './wallet-auto-recharge-presets-section'
import {
  buildAdminOptionState,
  buildPresetSavePlan,
} from '@/features/wallet/lib/auto-recharge-options'

const identityT = (key: string, options?: Record<string, unknown>) => {
  if (!options) return key
  return key.replace(/\{\{(\w+)\}\}/g, (_, token) =>
    String(options[token] ?? '')
  )
}

describe('wallet auto recharge preset admin helpers', () => {
  test('normalizes scheduled preset form values', () => {
    expect(
      normalizePresetForm({
        type: 'scheduled',
        target_scope: 'all',
        name: ' 월 1회 ',
        description: '',
        amount: '10000',
        threshold_amount: '',
        interval_unit: 'month',
        interval_value: '1',
        custom_seconds: '',
        charge_immediately: true,
        sort_order: '10',
        enabled: true,
      })
    ).toEqual({
      type: 'scheduled',
      target_scope: 'all',
      name: '월 1회',
      description: '',
      amount: 10000,
      threshold_amount: 0,
      interval_unit: 'month',
      interval_value: 1,
      custom_seconds: 0,
      charge_immediately: true,
      sort_order: 10,
      enabled: true,
    })
  })

  test('normalizes threshold preset by clearing stale scheduled-only values', () => {
    expect(
      normalizePresetForm({
        type: 'threshold',
        target_scope: 'organization',
        name: 'Low balance',
        description: 'uses threshold',
        amount: '25000',
        threshold_amount: '7000',
        interval_unit: 'custom',
        interval_value: '6',
        custom_seconds: '900',
        charge_immediately: true,
        sort_order: '3',
        enabled: false,
      })
    ).toEqual({
      type: 'threshold',
      target_scope: 'organization',
      name: 'Low balance',
      description: 'uses threshold',
      amount: 25000,
      threshold_amount: 7000,
      interval_unit: 'month',
      interval_value: 1,
      custom_seconds: 0,
      charge_immediately: false,
      sort_order: 3,
      enabled: false,
    })
  })

  test('normalizes scheduled preset by clearing stale threshold values after a type switch', () => {
    expect(
      normalizePresetForm({
        type: 'scheduled',
        target_scope: 'all',
        name: 'Every day',
        description: 'switched from threshold',
        amount: '12000',
        threshold_amount: '5000',
        interval_unit: 'day',
        interval_value: '1',
        custom_seconds: '300',
        charge_immediately: true,
        sort_order: '2',
        enabled: true,
      })
    ).toEqual({
      type: 'scheduled',
      target_scope: 'all',
      name: 'Every day',
      description: 'switched from threshold',
      amount: 12000,
      threshold_amount: 0,
      interval_unit: 'day',
      interval_value: 1,
      custom_seconds: 0,
      charge_immediately: true,
      sort_order: 2,
      enabled: true,
    })
  })

  test('summarizes threshold preset', () => {
    expect(
      getPresetSummary({
        id: 1,
        type: 'threshold',
        target_scope: 'user',
        name: '자동',
        amount: 20000,
        threshold_amount: 5000,
        enabled: true,
      }, identityT)
    ).toBe('Below 5000 -> 20000')
  })

  test('summarizes scheduled custom interval with translator', () => {
    expect(
      getPresetSummary({
        id: 2,
        type: 'scheduled',
        target_scope: 'all',
        name: 'Hourly',
        amount: 15000,
        interval_unit: 'custom',
        interval_value: 99,
        custom_seconds: 3600,
        enabled: true,
      }, identityT)
    ).toBe('15000 / 3600s')
  })

  test('translates scheduled interval units before interpolation', () => {
    const seenKeys: string[] = []
    const fakeT = (key: string, options?: Record<string, unknown>) => {
      seenKeys.push(key)
      if (key === 'Month(s)') return 'meses'
      if (!options) return key
      return key.replace(/\{\{(\w+)\}\}/g, (_, token) =>
        String(options[token] ?? '')
      )
    }

    expect(
      getPresetSummary(
        {
          id: 3,
          type: 'scheduled',
          target_scope: 'all',
          name: 'Monthly',
          amount: 30000,
          interval_unit: 'month',
          interval_value: 2,
          custom_seconds: 0,
          enabled: true,
        },
        fakeT
      )
    ).toBe('30000 / 2 meses')
    expect(seenKeys).toContain('Month(s)')
    expect(seenKeys).not.toContain('month')
  })

  test('parses option amount lists into sorted unique numbers', () => {
    expect(parseOptionAmountList('30000, 10000 10000\n5000')).toEqual([
      5000,
      10000,
      30000,
    ])
  })

  test('formats option amount lists for editing', () => {
    expect(formatOptionAmountList([30000, 10000, 10000, 5000])).toBe(
      '5000, 10000, 30000'
    )
  })

  test('keeps in-progress multi amount input draft instead of reformatting it', () => {
    const drafts = updateOptionAmountDrafts({}, 'monthly', '1000, ')

    expect(getOptionAmountDraftValue(drafts, 'monthly', [1000])).toBe('1000, ')
    expect(getOptionAmountDraftValue(drafts, 'weekly', [3000, 1000])).toBe(
      '1000, 3000'
    )
  })

  test('converts preset rows into admin option state', () => {
    const state = buildAdminOptionState([
      {
        id: 1,
        type: 'scheduled',
        target_scope: 'all',
        name: 'Monthly',
        amount: 10000,
        interval_unit: 'month',
        interval_value: 1,
        custom_seconds: 0,
        enabled: true,
      },
      {
        id: 2,
        type: 'threshold',
        target_scope: 'all',
        name: 'Auto',
        amount: 30000,
        threshold_amount: 5000,
        enabled: true,
      },
    ])

    expect(state.scheduled.periods[0].amounts).toEqual([10000])
    expect(state.threshold.rechargeAmounts).toEqual([30000])
    expect(state.threshold.thresholdAmounts).toEqual([5000])
  })

  test('builds save plan that disables removed admin combinations', () => {
    const current = [
      {
        id: 1,
        type: 'scheduled' as const,
        target_scope: 'all' as const,
        name: 'Monthly',
        amount: 10000,
        interval_unit: 'month' as const,
        interval_value: 1,
        custom_seconds: 0,
        charge_immediately: true,
        enabled: true,
        sort_order: 0,
      },
    ]
    const desired = buildAdminOptionState(current)
    desired.scheduled.periods = []

    const plan = buildPresetSavePlan(current, desired)

    expect(plan.disable.map((preset) => preset.id)).toEqual([1])
  })
})
