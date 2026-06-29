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
import { useState, useCallback } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { loadTossPayments, ANONYMOUS } from '@tosspayments/tosspayments-sdk'
import { requestTossPayment, isApiSuccess } from '../api'

// ============================================================================
// Toss Payment Hook
// ============================================================================

// Toss 충전: 백엔드에서 결제 세션 정보를 받아 SDK로 결제창을 연다.
export function useTossPayment() {
  const [processing, setProcessing] = useState(false)

  const processTossPayment = useCallback(async (topupAmount: number) => {
    try {
      setProcessing(true)
      const amount = Math.floor(topupAmount)
      const response = await requestTossPayment({ amount, payment_method: 'toss' })

      if (!isApiSuccess(response) || !response.data) {
        toast.error(response.message || i18next.t('Payment request failed'))
        return false
      }

      const { client_key, order_id, order_name, amount: chargeAmount, success_url, fail_url } =
        response.data

      const tossPayments = await loadTossPayments(client_key)
      const payment = tossPayments.payment({ customerKey: ANONYMOUS })

      await payment.requestPayment({
        method: 'CARD',
        amount: { currency: 'KRW', value: chargeAmount },
        orderId: order_id,
        orderName: order_name,
        successUrl: success_url,
        failUrl: fail_url,
      })
      // requestPayment가 결제창으로 리다이렉트하므로 이 지점 이후는 도달하지 않는다.
      return true
    } catch (err) {
      // 유저가 결제창을 닫으면 SDK가 reject한다 — 조용히 실패 처리.
      const message = (err as { message?: string })?.message
      if (message) toast.error(message)
      return false
    } finally {
      setProcessing(false)
    }
  }, [])

  return { processing, processTossPayment }
}
