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

export type TossPaymentLifecycleHandle = object

export type TossPaymentLifecycleLease = number

const EXPECTED_DESTROY_ERROR_CODES = new Set([
  'PAYMENT_REQUEST_ABORTED',
  'NO_ACTIVE_PAYMENT_REQUEST',
])

function getErrorCode(error: unknown): string {
  if (!error || typeof error !== 'object') return ''
  const code = (error as { code?: unknown }).code
  return typeof code === 'string' ? code : ''
}

export function isExpectedTossPaymentDestroyError(error: unknown): boolean {
  return EXPECTED_DESTROY_ERROR_CODES.has(getErrorCode(error))
}

/**
 * SDK destroy is best-effort during React cleanup. Expected lifecycle errors
 * are successful terminal outcomes; unexpected errors are contained so an
 * unmount can never create an unhandled rejection.
 */
export async function destroyTossPaymentSafely(
  payment: TossPaymentLifecycleHandle | null | undefined
): Promise<boolean> {
  if (!payment) return false
  const destroy = (payment as { destroy?: () => Promise<void> }).destroy
  // The installed SDK's declaration can lag the remotely loaded runtime.
  // Treat an older runtime without destroy() as a compatibility-safe no-op so
  // a completed/cancelled attempt cannot permanently block the next checkout.
  if (typeof destroy !== 'function') return true
  try {
    await destroy.call(payment)
    return true
  } catch (error) {
    return isExpectedTossPaymentDestroyError(error)
  }
}

/**
 * Owns one SDK payment instance for a mounted view. The epoch makes an SDK
 * instance loaded after unmount stale, while the serialized adoption queue
 * prevents a replacement request from racing destruction of an older iframe.
 */
export class TossPaymentLifecycle {
  private active = false
  private epoch = 0
  private requestInFlight = false
  private payment: TossPaymentLifecycleHandle | null = null
  private tail: Promise<void> = Promise.resolve()

  activate(): void {
    this.epoch += 1
    this.active = true
    this.requestInFlight = false
  }

  beginRequest(): TossPaymentLifecycleLease | null {
    if (!this.active || this.requestInFlight) return null
    this.requestInFlight = true
    return this.epoch
  }

  finishRequest(lease: TossPaymentLifecycleLease | null): void {
    if (lease !== null && this.active && lease === this.epoch) {
      this.requestInFlight = false
    }
  }

  async adopt(
    payment: TossPaymentLifecycleHandle,
    lease: TossPaymentLifecycleLease | null
  ): Promise<boolean> {
    return this.enqueue(async () => {
      if (!this.isCurrentLease(lease)) {
        await destroyTossPaymentSafely(payment)
        return false
      }

      const previous = this.payment
      if (previous && previous !== payment) {
        if (!(await destroyTossPaymentSafely(previous))) {
          await destroyTossPaymentSafely(payment)
          return false
        }
        if (this.payment === previous) this.payment = null
      }

      if (!this.isCurrentLease(lease)) {
        await destroyTossPaymentSafely(payment)
        return false
      }
      this.payment = payment
      return true
    })
  }

  deactivate(): void {
    this.active = false
    this.requestInFlight = false
    this.epoch += 1
    void this.enqueue(async () => {
      const payment = this.payment
      if (
        payment &&
        (await destroyTossPaymentSafely(payment)) &&
        this.payment === payment
      ) {
        this.payment = null
      }
    })
  }

  waitForIdle(): Promise<void> {
    return this.tail
  }

  private isCurrentLease(
    lease: TossPaymentLifecycleLease | null
  ): lease is TossPaymentLifecycleLease {
    return (
      lease !== null &&
      this.active &&
      this.requestInFlight &&
      lease === this.epoch
    )
  }

  private enqueue<T>(task: () => Promise<T>): Promise<T> {
    const result = this.tail.then(task, task)
    this.tail = result.then(
      () => undefined,
      () => undefined
    )
    return result
  }
}
