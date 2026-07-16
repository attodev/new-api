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
  destroyTossPaymentSafely,
  TossPaymentLifecycle,
} from './tossPaymentLifecycle';

function paymentHandle(onDestroy = () => undefined) {
  return {
    async destroy() {
      onDestroy();
    },
  };
}

describe('classic Toss payment lifecycle', () => {
  test('keeps the duplicate guard until the request finishes', () => {
    const lifecycle = new TossPaymentLifecycle();
    lifecycle.activate();
    const lease = lifecycle.beginRequest();
    expect(lease).not.toBeNull();
    expect(lifecycle.beginRequest()).toBeNull();
    lifecycle.finishRequest(lease);
    expect(lifecycle.beginRequest()).not.toBeNull();
  });

  test('destroys the active iframe on unmount', async () => {
    let destroys = 0;
    const lifecycle = new TossPaymentLifecycle();
    lifecycle.activate();
    const lease = lifecycle.beginRequest();
    expect(
      await lifecycle.adopt(
        paymentHandle(() => destroys++),
        lease,
      ),
    ).toBe(true);
    lifecycle.deactivate();
    await lifecycle.waitForIdle();
    expect(destroys).toBe(1);
  });

  test('destroys an SDK instance loaded after cleanup or StrictMode replay', async () => {
    let staleDestroys = 0;
    const lifecycle = new TossPaymentLifecycle();
    lifecycle.activate();
    const staleLease = lifecycle.beginRequest();
    lifecycle.deactivate();
    lifecycle.activate();

    expect(
      await lifecycle.adopt(
        paymentHandle(() => staleDestroys++),
        staleLease,
      ),
    ).toBe(false);
    expect(staleDestroys).toBe(1);
  });

  test('contains documented destroy terminal errors', async () => {
    for (const code of [
      'PAYMENT_REQUEST_ABORTED',
      'NO_ACTIVE_PAYMENT_REQUEST',
    ]) {
      expect(
        await destroyTossPaymentSafely({
          async destroy() {
            throw { code };
          },
        }),
      ).toBe(true);
    }
  });

  test('keeps older SDK runtimes without destroy compatible', async () => {
    expect(await destroyTossPaymentSafely({})).toBe(true);
  });

  test('retries an orphan-risking destroy before adopting another window', async () => {
    let previousDestroyAttempts = 0;
    let rejectedReplacementDestroys = 0;
    let acceptedReplacementDestroys = 0;
    const lifecycle = new TossPaymentLifecycle();
    lifecycle.activate();

    const initialLease = lifecycle.beginRequest();
    expect(
      await lifecycle.adopt(
        {
          async destroy() {
            previousDestroyAttempts += 1;
            if (previousDestroyAttempts === 1) throw new Error('transient');
          },
        },
        initialLease,
      ),
    ).toBe(true);
    lifecycle.finishRequest(initialLease);

    const rejectedLease = lifecycle.beginRequest();
    expect(
      await lifecycle.adopt(
        paymentHandle(() => rejectedReplacementDestroys++),
        rejectedLease,
      ),
    ).toBe(false);
    expect(previousDestroyAttempts).toBe(1);
    expect(rejectedReplacementDestroys).toBe(1);
    lifecycle.finishRequest(rejectedLease);

    const retryLease = lifecycle.beginRequest();
    expect(
      await lifecycle.adopt(
        paymentHandle(() => acceptedReplacementDestroys++),
        retryLease,
      ),
    ).toBe(true);
    expect(previousDestroyAttempts).toBe(2);
    expect(acceptedReplacementDestroys).toBe(0);

    lifecycle.deactivate();
    await lifecycle.waitForIdle();
    expect(acceptedReplacementDestroys).toBe(1);
  });

  test('retains a cleanup failure for StrictMode reactivation to retry', async () => {
    let previousDestroyAttempts = 0;
    let replacementDestroys = 0;
    const lifecycle = new TossPaymentLifecycle();
    lifecycle.activate();
    const initialLease = lifecycle.beginRequest();
    await lifecycle.adopt(
      {
        async destroy() {
          previousDestroyAttempts += 1;
          if (previousDestroyAttempts === 1) throw new Error('transient');
        },
      },
      initialLease,
    );

    lifecycle.deactivate();
    await lifecycle.waitForIdle();
    expect(previousDestroyAttempts).toBe(1);

    lifecycle.activate();
    const retryLease = lifecycle.beginRequest();
    expect(
      await lifecycle.adopt(
        paymentHandle(() => replacementDestroys++),
        retryLease,
      ),
    ).toBe(true);
    expect(previousDestroyAttempts).toBe(2);

    lifecycle.deactivate();
    await lifecycle.waitForIdle();
    expect(replacementDestroys).toBe(1);
  });
});
