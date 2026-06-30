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
import {
  getAutoRechargeModeTitleKey,
  parseMoneyInput,
} from './auto-recharge-card'

describe('auto recharge card helpers', () => {
  test('uses English source i18n keys for mode titles', () => {
    assert.equal(getAutoRechargeModeTitleKey('scheduled'), 'Scheduled recharge')
    assert.equal(getAutoRechargeModeTitleKey('threshold'), 'Auto recharge')
  })

  test('treats blank money input as invalid while preserving explicit zero', () => {
    assert.equal(parseMoneyInput(''), null)
    assert.equal(parseMoneyInput('   '), null)
    assert.equal(parseMoneyInput('0'), 0)
    assert.equal(parseMoneyInput(' 0 '), 0)
    assert.equal(parseMoneyInput('12.9'), 12)
  })
})
