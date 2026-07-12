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
  buildCompleteTossRepairUpdates,
  completeTossCredentialOptionPairs,
  TOSS_ATOMIC_OPTION_KEYS,
} from './toss-option-updates'

const credentialValues = {
  TossClientKey: 'live_ck_normal',
  TossSecretKey: 'live_sk_rotated',
  TossTestClientKey: 'test_ck_normal',
  TossTestSecretKey: '',
  TossBillingClientKey: 'live_ck_billing',
  TossBillingSecretKey: '',
  TossBillingTestClientKey: 'test_ck_billing',
  TossBillingTestSecretKey: '',
}

describe('Toss credential option updates', () => {
  test('includes the wallet contract gate in the atomic Toss update set', () => {
    assert.equal(
      TOSS_ATOMIC_OPTION_KEYS.has('TossWalletAutoRechargeEnabled'),
      true
    )
  })

  test('always sends the matching client key with a secret rotation', () => {
    assert.deepEqual(
      completeTossCredentialOptionPairs(
        [{ key: 'TossSecretKey', value: 'live_sk_rotated' }],
        credentialValues
      ),
      [
        { key: 'TossSecretKey', value: 'live_sk_rotated' },
        { key: 'TossClientKey', value: 'live_ck_normal' },
      ]
    )
  })

  test('sends both empty values when an inactive credential pair is cleared', () => {
    assert.deepEqual(
      completeTossCredentialOptionPairs(
        [{ key: 'TossTestClientKey', value: '' }],
        { ...credentialValues, TossTestClientKey: '' }
      ),
      [
        { key: 'TossTestClientKey', value: '' },
        { key: 'TossTestSecretKey', value: '' },
      ]
    )
  })

  test('does not add credentials to non-credential Toss updates', () => {
    assert.deepEqual(
      completeTossCredentialOptionPairs(
        [{ key: 'TossEnabled', value: true }],
        credentialValues
      ),
      [{ key: 'TossEnabled', value: true }]
    )
  })

  test('builds the exact complete ordered all-disabled repair payload', () => {
    const values = {
      TossEnabled: false,
      TossBillingEnabled: false,
      TossWalletAutoRechargeEnabled: false,
      TossTestMode: false,
      TossClientKey: 'live_ck_normal',
      TossSecretKey: 'live_sk_normal',
      TossTestClientKey: '',
      TossTestSecretKey: '',
      TossBillingClientKey: 'live_ck_billing',
      TossBillingSecretKey: 'live_sk_billing',
      TossBillingTestClientKey: '',
      TossBillingTestSecretKey: '',
      TossUnitPrice: 1300,
      TossMinTopUp: 100,
    }
    const updates = buildCompleteTossRepairUpdates(values)
    assert.equal(updates.length, 14)
    assert.deepEqual(
      updates.slice(0, 3).map((update) => update.value),
      [false, false, false]
    )
    assert.deepEqual(
      updates.map((update) => update.key),
      [...TOSS_ATOMIC_OPTION_KEYS]
    )
  })
})
