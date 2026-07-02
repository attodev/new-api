import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { getOrganizationAmountModeRequest } from './organization-wallet'

describe('organization wallet topup amount mode request', () => {
  test('includes amount mode only for Toss requests', () => {
    assert.deepEqual(getOrganizationAmountModeRequest(10, 'toss', 'quota'), {
      amount: 10,
      amount_mode: 'quota',
    })
    assert.deepEqual(getOrganizationAmountModeRequest(10, 'stripe', 'quota'), {
      amount: 10,
    })
  })
})
