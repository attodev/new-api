import { describe, expect, test } from 'bun:test'
import {
  getPresetSummary,
  normalizePresetForm,
} from './wallet-auto-recharge-presets-section'

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
})
