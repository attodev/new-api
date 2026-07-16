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
  destroyTossPaymentSafely,
  TossPaymentLifecycle,
} from './toss-payment-lifecycle'

function paymentHandle(onDestroy: () => void = () => undefined) {
  return {
    async destroy() {
      onDestroy()
    },
  }
}

describe('Toss payment lifecycle', () => {
  test('keeps a synchronous duplicate guard until the request finishes', () => {
    const lifecycle = new TossPaymentLifecycle()
    lifecycle.activate()
    const lease = lifecycle.beginRequest()
    assert.notEqual(lease, null)
    assert.equal(lifecycle.beginRequest(), null)
    lifecycle.finishRequest(lease)
    assert.notEqual(lifecycle.beginRequest(), null)
  })

  test('destroys the active iframe on unmount', async () => {
    let destroys = 0
    const lifecycle = new TossPaymentLifecycle()
    lifecycle.activate()
    const lease = lifecycle.beginRequest()
    assert.equal(
      await lifecycle.adopt(
        paymentHandle(() => destroys++),
        lease
      ),
      true
    )
    lifecycle.deactivate()
    await lifecycle.waitForIdle()
    assert.equal(destroys, 1)
  })

  test('destroys an SDK instance that resolves after cleanup or StrictMode replay', async () => {
    let staleDestroys = 0
    const lifecycle = new TossPaymentLifecycle()
    lifecycle.activate()
    const staleLease = lifecycle.beginRequest()
    lifecycle.deactivate()
    lifecycle.activate()

    assert.equal(
      await lifecycle.adopt(
        paymentHandle(() => staleDestroys++),
        staleLease
      ),
      false
    )
    assert.equal(staleDestroys, 1)
  })

  test('treats documented destroy terminal errors as safe cleanup', async () => {
    for (const code of [
      'PAYMENT_REQUEST_ABORTED',
      'NO_ACTIVE_PAYMENT_REQUEST',
    ]) {
      assert.equal(
        await destroyTossPaymentSafely({
          async destroy() {
            throw { code }
          },
        }),
        true
      )
    }
  })

  test('keeps older SDK runtimes without destroy compatible', async () => {
    assert.equal(await destroyTossPaymentSafely({}), true)
  })

  test('retries an orphan-risking destroy before adopting another window', async () => {
    let previousDestroyAttempts = 0
    let rejectedReplacementDestroys = 0
    let acceptedReplacementDestroys = 0
    const lifecycle = new TossPaymentLifecycle()
    lifecycle.activate()

    const initialLease = lifecycle.beginRequest()
    assert.equal(
      await lifecycle.adopt(
        {
          async destroy() {
            previousDestroyAttempts += 1
            if (previousDestroyAttempts === 1) throw new Error('transient')
          },
        },
        initialLease
      ),
      true
    )
    lifecycle.finishRequest(initialLease)

    const rejectedLease = lifecycle.beginRequest()
    assert.equal(
      await lifecycle.adopt(
        paymentHandle(() => rejectedReplacementDestroys++),
        rejectedLease
      ),
      false
    )
    assert.equal(previousDestroyAttempts, 1)
    assert.equal(rejectedReplacementDestroys, 1)
    lifecycle.finishRequest(rejectedLease)

    const retryLease = lifecycle.beginRequest()
    assert.equal(
      await lifecycle.adopt(
        paymentHandle(() => acceptedReplacementDestroys++),
        retryLease
      ),
      true
    )
    assert.equal(previousDestroyAttempts, 2)
    assert.equal(acceptedReplacementDestroys, 0)

    lifecycle.deactivate()
    await lifecycle.waitForIdle()
    assert.equal(acceptedReplacementDestroys, 1)
  })

  test('retains a cleanup failure for StrictMode reactivation to retry', async () => {
    let previousDestroyAttempts = 0
    let replacementDestroys = 0
    const lifecycle = new TossPaymentLifecycle()
    lifecycle.activate()
    const initialLease = lifecycle.beginRequest()
    await lifecycle.adopt(
      {
        async destroy() {
          previousDestroyAttempts += 1
          if (previousDestroyAttempts === 1) throw new Error('transient')
        },
      },
      initialLease
    )

    lifecycle.deactivate()
    await lifecycle.waitForIdle()
    assert.equal(previousDestroyAttempts, 1)

    lifecycle.activate()
    const retryLease = lifecycle.beginRequest()
    assert.equal(
      await lifecycle.adopt(
        paymentHandle(() => replacementDestroys++),
        retryLease
      ),
      true
    )
    assert.equal(previousDestroyAttempts, 2)

    lifecycle.deactivate()
    await lifecycle.waitForIdle()
    assert.equal(replacementDestroys, 1)
  })
})
