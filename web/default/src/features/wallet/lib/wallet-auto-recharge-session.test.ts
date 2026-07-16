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
import type { WalletAutoRechargeRequest } from '../types'
import {
  cancelRecoverableWalletAutoRechargeBillingSession,
  isWalletAutoRechargeBillingSessionMatchingRequest,
} from './wallet-auto-recharge-session'

const fingerprint = 'a'.repeat(64)
const expectedPolicy = {
  preset_id: 7,
  type: 'scheduled' as const,
  target_scope: 'user' as const,
  amount: 10_000,
  threshold_amount: 0,
  threshold_quota: 0,
  interval_unit: 'month' as const,
  interval_value: 1,
  custom_seconds: 0,
  charge_immediately: true,
  enabled: true,
}
const request: WalletAutoRechargeRequest = {
  preset_id: expectedPolicy.preset_id,
  preset_fingerprint: fingerprint,
  expected_policy: expectedPolicy,
}
const validSession = {
  client_key: 'test_ck_valid',
  customer_key: 'customer_key_valid',
  trade_no: 'wallet_auth_valid',
  success_url:
    'https://example.com/api/wallet/auto-recharge/toss/confirm?trade_no=wallet_auth_valid',
  fail_url:
    'https://example.com/api/wallet/auto-recharge/toss/fail?trade_no=wallet_auth_valid',
  preset_fingerprint: fingerprint,
  policy: expectedPolicy,
}

describe('wallet auto recharge billing session contract', () => {
  test('requires the response fingerprint and immutable policy snapshot to match the displayed preset', () => {
    assert.equal(
      isWalletAutoRechargeBillingSessionMatchingRequest(validSession, request),
      true
    )
    assert.equal(
      isWalletAutoRechargeBillingSessionMatchingRequest(
        {
          ...validSession,
          policy: { ...expectedPolicy, amount: 100_000 },
        },
        request
      ),
      false
    )
    assert.equal(
      isWalletAutoRechargeBillingSessionMatchingRequest(
        { ...validSession, preset_fingerprint: 'b'.repeat(64) },
        request
      ),
      false
    )
    assert.equal(
      isWalletAutoRechargeBillingSessionMatchingRequest(
        {
          ...validSession,
          success_url:
            'https://example.com/api/wallet/auto-recharge/toss/confirm?trade_no=another_trade',
        },
        request
      ),
      false
    )
  })

  test('cancels a durable pending trade even when another session field is malformed', async () => {
    const calls: Array<{ tradeNo: string; scope: string }> = []
    const cancelled = await cancelRecoverableWalletAutoRechargeBillingSession(
      { ...validSession, client_key: '' },
      'organization',
      async (tradeNo, scope) => {
        calls.push({ tradeNo, scope })
      }
    )

    assert.equal(cancelled, true)
    assert.deepEqual(calls, [
      { tradeNo: validSession.trade_no, scope: 'organization' },
    ])
  })
})
