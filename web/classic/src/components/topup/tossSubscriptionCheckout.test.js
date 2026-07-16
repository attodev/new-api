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
  createTossSubscriptionConfirmation,
  getTossPaymentWindowTargetOptions,
  isTossUserCancellation,
  isTossSubscriptionBillingSession,
  tossSubscriptionSessionMatchesConfirmation,
} from './tossSubscriptionCheckout';

describe('classic Toss callback window target', () => {
  test('keeps the SDK default for same-origin callbacks', () => {
    expect(
      getTossPaymentWindowTargetOptions(
        'https://app.example/api/toss/confirm',
        'https://app.example/api/toss/fail',
        'https://app.example',
      ),
    ).toEqual({});
  });

  test('uses self for cross-origin callbacks', () => {
    expect(
      getTossPaymentWindowTargetOptions(
        'https://api.example/api/toss/confirm',
        'https://api.example/api/toss/fail',
        'https://app.example',
      ),
    ).toEqual({ windowTarget: 'self' });
  });
});

describe('classic Toss SDK cancellation errors', () => {
  test('recognizes lifecycle-triggered payment request aborts', () => {
    expect(isTossUserCancellation({ code: 'PAYMENT_REQUEST_ABORTED' })).toBe(
      true,
    );
    expect(isTossUserCancellation({ code: 'UNKNOWN' })).toBe(false);
  });
});

const record = {
  plan: { id: 7, title: 'Pro', price_amount: 10, currency: 'USD' },
  toss_checkout: {
    plan_id: 7,
    plan_title: 'Pro',
    price_amount: 10,
    price_currency: 'USD',
    provider_amount: 13000,
    provider_currency: 'KRW',
    snapshot_fingerprint: 'a'.repeat(40),
  },
};

describe('classic Toss subscription checkout contract', () => {
  test('accepts only a session that matches the immutable displayed terms', () => {
    const confirmation = createTossSubscriptionConfirmation(record);
    const session = {
      client_key: 'test_ck_value',
      customer_key: 'customer-7',
      trade_no: 'toss_sub_abcdef',
      success_url:
        'https://billing.example/api/subscription/toss/confirm/toss_sub_abcdef',
      fail_url:
        'https://billing.example/api/subscription/toss/fail/toss_sub_abcdef',
      toss_checkout: { ...record.toss_checkout },
    };

    expect(confirmation).not.toBeNull();
    expect(isTossSubscriptionBillingSession(session)).toBe(true);
    expect(
      isTossSubscriptionBillingSession({
        ...session,
        customer_key: `cust_${'x'.repeat(46)}`,
      }),
    ).toBe(false);
    expect(
      tossSubscriptionSessionMatchesConfirmation(session, confirmation),
    ).toBe(true);
    expect(
      tossSubscriptionSessionMatchesConfirmation(
        {
          ...session,
          toss_checkout: { ...session.toss_checkout, provider_amount: 14000 },
        },
        confirmation,
      ),
    ).toBe(false);
  });

  test('rejects malformed browser boundary values', () => {
    const confirmation = createTossSubscriptionConfirmation(record);
    expect(
      isTossSubscriptionBillingSession({
        client_key: 'test_ck_value',
        customer_key: 'predictable',
        trade_no: 'short',
        success_url: 'javascript:alert(1)',
        fail_url: 'https://billing.example/api/subscription/toss/fail/short',
        toss_checkout: confirmation?.checkout,
      }),
    ).toBe(false);
  });

  test('binds both callback paths and origins to the pending trade', () => {
    const tradeNo = 'toss_sub_abcdef';
    const session = {
      client_key: 'test_ck_value',
      customer_key: 'customer-7',
      trade_no: tradeNo,
      success_url: `https://billing.example/api/subscription/toss/confirm/${tradeNo}`,
      fail_url: `https://billing.example/api/subscription/toss/fail/${tradeNo}`,
      toss_checkout: { ...record.toss_checkout },
    };

    expect(
      isTossSubscriptionBillingSession({
        ...session,
        success_url:
          'https://billing.example/api/subscription/toss/confirm/another_trade',
      }),
    ).toBe(false);
    expect(
      isTossSubscriptionBillingSession({
        ...session,
        fail_url: `https://other.example/api/subscription/toss/fail/${tradeNo}`,
      }),
    ).toBe(false);
  });
});
