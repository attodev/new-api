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
import { loadTossPayments } from '@tosspayments/tosspayments-sdk'
import { paySubscriptionToss } from '../api'

// ============================================================================
// Toss Billing Hook
// ============================================================================

// Toss 자동결제 구독: 빌링키 인증창을 연다.
// Backend `/api/subscription/toss/pay` returns
// {client_key, customer_key, trade_no, success_url, fail_url}.
// The SDK `requestBillingAuth` redirects to successUrl on completion.
export function useTossBilling() {
  const [processing, setProcessing] = useState(false)

  const subscribeWithToss = useCallback(async (planId: number) => {
    try {
      setProcessing(true)
      const resp = await paySubscriptionToss({ plan_id: planId })
      // Backend returns { message: "success", data: {...} } without a `success` field
      // (same shape as the other subscription-pay endpoints), so accept either signal.
      const ok = resp?.success === true || resp?.message === 'success'
      if (!ok || !resp?.data) {
        toast.error(resp?.message || i18next.t('Payment request failed'))
        return false
      }
      const { client_key, customer_key, success_url, fail_url } = resp.data
      if (!client_key || !customer_key || !success_url || !fail_url) {
        toast.error(i18next.t('Payment request failed'))
        return false
      }
      const tossPayments = await loadTossPayments(client_key)
      const payment = tossPayments.payment({ customerKey: customer_key })
      await payment.requestBillingAuth({
        method: 'CARD',
        successUrl: success_url,
        failUrl: fail_url,
      })
      // requestBillingAuth redirects to successUrl — this line is not reached.
      return true
    } catch (err) {
      // Toss billing auth may use PAY_PROCESS_CANCELED; keep USER_CANCEL for SDK-version compatibility.
      const e = err as { code?: string; message?: string }
      if (
        e?.code &&
        e.code !== 'PAY_PROCESS_CANCELED' &&
        e.code !== 'USER_CANCEL'
      ) {
        toast.error(i18next.t('Payment request failed'))
      }
      return false
    } finally {
      setProcessing(false)
    }
  }, [])

  return { processing, subscribeWithToss }
}
