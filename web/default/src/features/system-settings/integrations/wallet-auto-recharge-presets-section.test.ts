import { describe, expect, test } from 'bun:test'
import {
  getPresetSummary,
  normalizePresetForm,
} from './wallet-auto-recharge-presets-section'

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
      })
    ).toContain('5000')
  })
})
