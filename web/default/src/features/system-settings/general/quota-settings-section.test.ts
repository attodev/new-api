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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { quotaSchema } from './quota-settings-section'

function parse(trust: unknown, multiplier: unknown) {
  return quotaSchema.safeParse({
    QuotaForNewUser: 0,
    QuotaForInviter: 0,
    QuotaForInvitee: 0,
    TopUpLink: '',
    general_setting: { docs_link: '' },
    quota_setting: {
      enable_free_model_pre_consume: true,
      trust_quota_usd: trust,
      pre_consume_multiplier: multiplier,
    },
  })
}

describe('quota settings pre-consume fields', () => {
  test('accepts defaults and decimal values', () => {
    const result = parse(10, 1)
    assert.equal(result.success, true)
    assert.equal(parse('10.5', '1.5').success, true)
    assert.equal(parse(0, 0.5).success, true)
  })

  test('coerces string input to numbers', () => {
    const result = parse('2.5', '0.25')
    assert.equal(result.success, true)
    if (result.success) {
      assert.equal(result.data.quota_setting.trust_quota_usd, 2.5)
      assert.equal(result.data.quota_setting.pre_consume_multiplier, 0.25)
    }
  })

  test('rejects a negative or empty trust threshold', () => {
    assert.equal(parse(-1, 1).success, false)
    assert.equal(parse('', 1).success, false)
  })

  test('rejects a zero, negative or empty multiplier', () => {
    assert.equal(parse(10, 0).success, false)
    assert.equal(parse(10, -0.5).success, false)
    assert.equal(parse(10, '').success, false)
  })
})
