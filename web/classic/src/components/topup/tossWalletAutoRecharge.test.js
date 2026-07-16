/*
Copyright (C) 2025 QuantumNous

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

import { describe, expect, test } from 'bun:test';
import {
  hasValidWalletAutoRechargeFingerprint,
  isUserVisibleWalletAutoRechargePolicy,
  walletAutoRechargeSessionMatchesPreset,
} from './tossWalletAutoRecharge';

const preset = {
  id: 3,
  type: 'threshold',
  target_scope: 'user',
  amount: 5000,
  threshold_amount: 0,
  threshold_quota: 1000,
  interval_unit: '',
  interval_value: 0,
  custom_seconds: 0,
  charge_immediately: false,
  enabled: true,
  terms_fingerprint: 'b'.repeat(64),
};

describe('classic Toss wallet auto recharge contract', () => {
  test('keeps provider cleanup-only rows out of the configurable policy slot', () => {
    expect(isUserVisibleWalletAutoRechargePolicy({ status: 'pending' })).toBe(
      true,
    );
    expect(isUserVisibleWalletAutoRechargePolicy({ status: 'active' })).toBe(
      true,
    );
    expect(
      isUserVisibleWalletAutoRechargePolicy({ status: 'cancel_pending' }),
    ).toBe(false);
  });

  test('matches every immutable preset field', () => {
    const session = {
      client_key: 'test_ck_value',
      customer_key: 'customer-3',
      trade_no: 'wallet_auto_abcdef',
      success_url:
        'https://billing.example/api/wallet/auto-recharge/toss/confirm?trade_no=wallet_auto_abcdef',
      fail_url:
        'https://billing.example/api/wallet/auto-recharge/toss/fail?trade_no=wallet_auto_abcdef',
      preset_fingerprint: preset.terms_fingerprint,
      policy: {
        preset_id: 3,
        type: 'threshold',
        target_scope: 'user',
        amount: 5000,
        threshold_amount: 0,
        threshold_quota: 1000,
        interval_unit: '',
        interval_value: 0,
        custom_seconds: 0,
        charge_immediately: false,
        enabled: true,
      },
    };

    expect(hasValidWalletAutoRechargeFingerprint(preset)).toBe(true);
    expect(walletAutoRechargeSessionMatchesPreset(session, preset)).toBe(true);
    expect(
      walletAutoRechargeSessionMatchesPreset(
        { ...session, policy: { ...session.policy, amount: 6000 } },
        preset,
      ),
    ).toBe(false);
    expect(
      walletAutoRechargeSessionMatchesPreset(
        {
          ...session,
          fail_url:
            'https://billing.example/api/wallet/auto-recharge/toss/fail?trade_no=another_trade',
        },
        preset,
      ),
    ).toBe(false);
  });
});
