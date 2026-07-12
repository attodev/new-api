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
import { useState, useCallback, useRef } from 'react'
import { loadTossPayments } from '@tosspayments/tosspayments-sdk'
import i18next from 'i18next'
import { toast } from 'sonner'
import { useTossPaymentLifecycle } from '@/features/wallet/hooks/use-toss-payment-lifecycle'
import {
  getTossPaymentWindowTargetOptions,
  isTossUserCancellation,
  isValidTossBillingAuthSession,
} from '@/features/wallet/lib'
import { cancelPendingTossSubscription, paySubscriptionToss } from '../api'
import {
  tossSubscriptionSessionMatchesConfirmation,
  type TossSubscriptionPaymentConfirmation,
} from '../lib/toss-checkout'

// ============================================================================
// Toss Billing Hook
// ============================================================================

// Toss 자동결제 구독: 빌링키 인증창을 연다.
// Backend `/api/subscription/toss/pay` returns
// {client_key, customer_key, trade_no, success_url, fail_url, toss_checkout}.
// The SDK `requestBillingAuth` redirects to successUrl on completion.
export function useTossBilling() {
  const [processing, setProcessing] = useState(false)
  const processingRef = useRef(false)
  const paymentLifecycle = useTossPaymentLifecycle()

  const subscribeWithToss = useCallback(
    async (
      confirmation: Readonly<TossSubscriptionPaymentConfirmation>
    ): Promise<'started' | 'failed' | 'confirmation_changed'> => {
      // Avoid two pending purchase reservations when two click events are
      // delivered before React commits the disabled state.
      if (processingRef.current) return 'failed'
      const lifecycleLease = paymentLifecycle.beginRequest()
      if (lifecycleLease === null) return 'failed'
      processingRef.current = true
      let pendingTradeNo = ''
      try {
        setProcessing(true)
        const resp = await paySubscriptionToss({
          plan_id: confirmation.plan.id,
        })
        if (typeof resp?.data?.trade_no === 'string') {
          pendingTradeNo = resp.data.trade_no
        }
        // Backend returns { message: "success", data: {...} } without a `success` field
        // (same shape as the other subscription-pay endpoints), so accept either signal.
        const ok = resp?.success === true || resp?.message === 'success'
        if (!ok || !isValidTossBillingAuthSession(resp?.data, 'subscription')) {
          if (pendingTradeNo) {
            await cancelPendingTossSubscription(pendingTradeNo).catch(() => {})
          }
          toast.error(resp?.message || i18next.t('Payment request failed'))
          return 'failed'
        }
        if (
          !tossSubscriptionSessionMatchesConfirmation(resp.data, confirmation)
        ) {
          await cancelPendingTossSubscription(pendingTradeNo).catch(() => {})
          toast.info(
            i18next.t(
              'Payment details changed. Please review and confirm again.'
            )
          )
          return 'confirmation_changed'
        }
        const { client_key, customer_key, success_url, fail_url } = resp.data
        const tossPayments = await loadTossPayments(client_key)
        const payment = tossPayments.payment({ customerKey: customer_key })
        if (!(await paymentLifecycle.adopt(payment, lifecycleLease))) {
          if (pendingTradeNo) {
            await cancelPendingTossSubscription(pendingTradeNo).catch(() => {})
          }
          return 'failed'
        }
        await payment.requestBillingAuth({
          method: 'CARD',
          successUrl: success_url,
          failUrl: fail_url,
          ...getTossPaymentWindowTargetOptions(success_url, fail_url),
        })
        // requestBillingAuth redirects to successUrl — this line is not reached.
        return 'started'
      } catch (err) {
        if (isTossUserCancellation(err)) {
          toast.info(i18next.t('Cancelled'))
        } else {
          toast.error(i18next.t('Payment request failed'))
        }
        if (pendingTradeNo) {
          await cancelPendingTossSubscription(pendingTradeNo).catch(() => {})
        }
        return 'failed'
      } finally {
        paymentLifecycle.finishRequest(lifecycleLease)
        processingRef.current = false
        setProcessing(false)
      }
    },
    [paymentLifecycle]
  )

  return { processing, subscribeWithToss }
}
