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
import { requestTossPayment, isApiSuccess } from '../api'
import {
  getTossPaymentWindowTargetOptions,
  isTossUserCancellation,
  isValidTossPaymentSession,
} from '../lib'
import { tossPaymentSessionMatchesConfirmation } from '../lib/topup-amount-mode'
import type { TossPaymentConfirmation } from '../types'
import { useTossPaymentLifecycle } from './use-toss-payment-lifecycle'

// ============================================================================
// Toss Payment Hook
// ============================================================================

// Toss 충전: 백엔드에서 결제 세션 정보를 받아 SDK로 결제창을 연다.
export function useTossPayment() {
  const [processing, setProcessing] = useState(false)
  // React state updates are not synchronous. Keep an imperative guard as well
  // so a double click cannot create two durable Toss orders before the button
  // has re-rendered as disabled.
  const processingRef = useRef(false)
  const paymentLifecycle = useTossPaymentLifecycle()

  const processTossPayment = useCallback(
    async (
      confirmation: Readonly<TossPaymentConfirmation>
    ): Promise<'started' | 'failed' | 'confirmation_changed'> => {
      if (processingRef.current) return 'failed'
      const lifecycleLease = paymentLifecycle.beginRequest()
      if (lifecycleLease === null) return 'failed'
      processingRef.current = true
      try {
        setProcessing(true)
        const response = await requestTossPayment({
          amount: confirmation.input_amount,
          amount_mode: confirmation.amount_mode,
          payment_method: 'toss',
        })

        if (
          !isApiSuccess(response) ||
          !isValidTossPaymentSession(response.data)
        ) {
          toast.error(response.message || i18next.t('Payment request failed'))
          return 'failed'
        }

        if (
          !tossPaymentSessionMatchesConfirmation(response.data, confirmation)
        ) {
          toast.info(
            i18next.t(
              'Payment details changed. Please review and confirm again.'
            )
          )
          return 'confirmation_changed'
        }

        const {
          client_key,
          customer_key,
          order_id,
          order_name,
          amount: chargeAmount,
          success_url,
          fail_url,
        } = response.data

        const tossPayments = await loadTossPayments(client_key)
        const payment = tossPayments.payment({ customerKey: customer_key })
        if (!(await paymentLifecycle.adopt(payment, lifecycleLease))) {
          return 'failed'
        }

        await payment.requestPayment({
          method: 'CARD',
          amount: { currency: 'KRW', value: chargeAmount },
          taxFreeAmount: 0,
          card: { useEscrow: false, taxExemptionAmount: 0 },
          orderId: order_id,
          orderName: order_name,
          successUrl: success_url,
          failUrl: fail_url,
          ...getTossPaymentWindowTargetOptions(success_url, fail_url),
        })
        // requestPayment가 결제창으로 리다이렉트하므로 이 지점 이후는 도달하지 않는다.
        return 'started'
      } catch (err) {
        // SDK v2 rejects an in-page close as USER_CANCEL. A failUrl redirect
        // can use PAY_PROCESS_CANCELED, while lifecycle cleanup rejects an
        // active request as PAYMENT_REQUEST_ABORTED. None is a payment failure.
        if (isTossUserCancellation(err)) {
          toast.info(i18next.t('Cancelled'))
        } else {
          toast.error(i18next.t('Payment request failed'))
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

  return { processing, processTossPayment }
}
