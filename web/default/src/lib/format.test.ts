import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { formatTokens } from './format'

describe('formatTokens', () => {
  test('keeps small and mid-range rendering unchanged', () => {
    assert.equal(formatTokens(0), '-')
    assert.equal(formatTokens(999), '999')
    assert.equal(formatTokens(1500), '1.5K')
    assert.equal(formatTokens(1e6), '1.00M')
    assert.equal(formatTokens(-1500), '-1500')
  })

  test('uses B and T tiers for large token counts', () => {
    assert.equal(formatTokens(1e9), '1.00B')
    assert.equal(formatTokens(2e9), '2.00B')
    assert.equal(formatTokens(1e12), '1.00T')
    // common.MaxQuota -- the largest value a quota field can hold
    assert.equal(formatTokens(5e14), '500.00T')
  })
})
