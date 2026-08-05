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

import { useEffect, useRef } from 'react';

const EXPECTED_DESTROY_ERROR_CODES = new Set([
  'PAYMENT_REQUEST_ABORTED',
  'NO_ACTIVE_PAYMENT_REQUEST',
]);

function getErrorCode(error) {
  return error && typeof error === 'object' && typeof error.code === 'string'
    ? error.code
    : '';
}

export function isExpectedTossPaymentDestroyError(error) {
  return EXPECTED_DESTROY_ERROR_CODES.has(getErrorCode(error));
}

export async function destroyTossPaymentSafely(payment) {
  if (!payment) return false;
  // The installed SDK's declaration can lag the remotely loaded runtime.
  // Keep an older runtime without destroy() compatible with another attempt.
  if (typeof payment.destroy !== 'function') return true;
  try {
    await payment.destroy();
    return true;
  } catch (error) {
    return isExpectedTossPaymentDestroyError(error);
  }
}

export class TossPaymentLifecycle {
  constructor() {
    this.active = false;
    this.epoch = 0;
    this.requestInFlight = false;
    this.payment = null;
    this.tail = Promise.resolve();
  }

  activate() {
    this.epoch += 1;
    this.active = true;
    this.requestInFlight = false;
  }

  beginRequest() {
    if (!this.active || this.requestInFlight) return null;
    this.requestInFlight = true;
    return this.epoch;
  }

  finishRequest(lease) {
    if (lease !== null && this.active && lease === this.epoch) {
      this.requestInFlight = false;
    }
  }

  async adopt(payment, lease) {
    return this.enqueue(async () => {
      if (!this.isCurrentLease(lease)) {
        await destroyTossPaymentSafely(payment);
        return false;
      }

      const previous = this.payment;
      if (previous && previous !== payment) {
        if (!(await destroyTossPaymentSafely(previous))) {
          await destroyTossPaymentSafely(payment);
          return false;
        }
        if (this.payment === previous) this.payment = null;
      }

      if (!this.isCurrentLease(lease)) {
        await destroyTossPaymentSafely(payment);
        return false;
      }
      this.payment = payment;
      return true;
    });
  }

  deactivate() {
    this.active = false;
    this.requestInFlight = false;
    this.epoch += 1;
    void this.enqueue(async () => {
      const payment = this.payment;
      if (
        payment &&
        (await destroyTossPaymentSafely(payment)) &&
        this.payment === payment
      ) {
        this.payment = null;
      }
    });
  }

  waitForIdle() {
    return this.tail;
  }

  isCurrentLease(lease) {
    return (
      lease !== null &&
      this.active &&
      this.requestInFlight &&
      lease === this.epoch
    );
  }

  enqueue(task) {
    const result = this.tail.then(task, task);
    this.tail = result.then(
      () => undefined,
      () => undefined,
    );
    return result;
  }
}

export function useTossPaymentLifecycle() {
  const lifecycleRef = useRef(null);
  if (!lifecycleRef.current) {
    lifecycleRef.current = new TossPaymentLifecycle();
  }

  useEffect(() => {
    const lifecycle = lifecycleRef.current;
    lifecycle.activate();
    return () => lifecycle.deactivate();
  }, []);

  return lifecycleRef.current;
}
