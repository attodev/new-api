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
import { useCallback, useEffect, useRef, useState } from 'react'
import { loadTossPayments } from '@tosspayments/tosspayments-sdk'
import i18next from 'i18next'
import { toast } from 'sonner'
import {
  cancelPendingWalletAutoRecharge,
  cancelWalletAutoRecharge,
  getWalletAutoRecharge,
  getWalletAutoRechargePresets,
  isApiSuccess,
  requestWalletScheduledRecharge,
  requestWalletThresholdRecharge,
} from '../api'
import {
  cancelRecoverableWalletAutoRechargeBillingSession,
  getTossPaymentWindowTargetOptions,
  isTossUserCancellation,
  isWalletAutoRechargeBillingSessionMatchingRequest,
} from '../lib'
import type { TossPaymentLifecycleLease } from '../lib'
import type {
  WalletAutoRechargePolicy,
  WalletAutoRechargePreset,
  WalletAutoRechargeRequest,
  WalletAutoRechargeTossResponse,
} from '../types'
import { useTossPaymentLifecycle } from './use-toss-payment-lifecycle'

export function useWalletAutoRecharge(
  scope: 'user' | 'organization',
  canManage: boolean
) {
  const [policies, setPolicies] = useState<WalletAutoRechargePolicy[]>([])
  const [presets, setPresets] = useState<WalletAutoRechargePreset[]>([])
  const [loading, setLoading] = useState(false)
  const [processing, setProcessing] = useState(false)
  const [presetsLoading, setPresetsLoading] = useState(false)
  const [presetsLoaded, setPresetsLoaded] = useState(false)
  const [policiesLoaded, setPoliciesLoaded] = useState(false)
  const processingRef = useRef(false)
  const paymentLifecycle = useTossPaymentLifecycle()

  const refresh = useCallback(async () => {
    setLoading(true)
    setPoliciesLoaded(false)
    try {
      const response = await getWalletAutoRecharge(scope)
      if (isApiSuccess(response) && Array.isArray(response.data)) {
        setPolicies(response.data)
        setPoliciesLoaded(true)
      }
    } finally {
      setLoading(false)
    }
  }, [scope])

  const refreshPresets = useCallback(async () => {
    setPresetsLoading(true)
    setPresetsLoaded(false)
    try {
      const response = await getWalletAutoRechargePresets(scope)
      if (isApiSuccess(response) && Array.isArray(response.data)) {
        setPresets(response.data)
        setPresetsLoaded(true)
      }
    } finally {
      setPresetsLoading(false)
    }
  }, [scope])

  useEffect(() => {
    void refresh()
    void refreshPresets()
  }, [refresh, refreshPresets])

  const startBillingAuth = useCallback(
    async (
      response: WalletAutoRechargeTossResponse,
      request: WalletAutoRechargeRequest,
      lifecycleLease: TossPaymentLifecycleLease
    ) => {
      // A backend response can contain a durable pending trade before another
      // field fails client validation. Capture only a strictly valid order ID
      // first so every malformed/stale session can still be cancelled.
      if (
        !isApiSuccess(response) ||
        !isWalletAutoRechargeBillingSessionMatchingRequest(
          response.data,
          request
        )
      ) {
        await cancelRecoverableWalletAutoRechargeBillingSession(
          response?.data,
          scope,
          cancelPendingWalletAutoRecharge
        ).catch(() => false)
        await Promise.allSettled([refresh(), refreshPresets()])
        toast.error(response.message || i18next.t('Payment request failed'))
        return false
      }

      const { client_key, customer_key, success_url, fail_url, trade_no } =
        response.data
      try {
        const tossPayments = await loadTossPayments(client_key)
        const payment = tossPayments.payment({ customerKey: customer_key })
        if (!(await paymentLifecycle.adopt(payment, lifecycleLease))) {
          if (trade_no) {
            await cancelPendingWalletAutoRecharge(trade_no, scope).catch(
              () => {}
            )
          }
          return false
        }
        await payment.requestBillingAuth({
          method: 'CARD',
          successUrl: success_url,
          failUrl: fail_url,
          ...getTossPaymentWindowTargetOptions(success_url, fail_url),
        })
        return true
      } catch (err) {
        if (isTossUserCancellation(err)) {
          toast.info(i18next.t('Cancelled'))
        } else {
          toast.error(i18next.t('Payment request failed'))
        }
        if (trade_no) {
          await cancelPendingWalletAutoRecharge(trade_no, scope).catch(() => {})
          await refresh()
        }
        return false
      }
    },
    [paymentLifecycle, refresh, refreshPresets, scope]
  )

  const createScheduled = useCallback(
    async (payload: WalletAutoRechargeRequest) => {
      if (!canManage || processingRef.current) return false
      const lifecycleLease = paymentLifecycle.beginRequest()
      if (lifecycleLease === null) return false

      processingRef.current = true
      setProcessing(true)
      try {
        const response = await requestWalletScheduledRecharge(payload, scope)
        const started = await startBillingAuth(
          response,
          payload,
          lifecycleLease
        )
        if (started) await refresh()
        return started
      } catch {
        await Promise.allSettled([refresh(), refreshPresets()])
        return false
      } finally {
        paymentLifecycle.finishRequest(lifecycleLease)
        processingRef.current = false
        setProcessing(false)
      }
    },
    [
      canManage,
      paymentLifecycle,
      refresh,
      refreshPresets,
      scope,
      startBillingAuth,
    ]
  )

  const createThreshold = useCallback(
    async (payload: WalletAutoRechargeRequest) => {
      if (!canManage || processingRef.current) return false
      const lifecycleLease = paymentLifecycle.beginRequest()
      if (lifecycleLease === null) return false

      processingRef.current = true
      setProcessing(true)
      try {
        const response = await requestWalletThresholdRecharge(payload, scope)
        const started = await startBillingAuth(
          response,
          payload,
          lifecycleLease
        )
        if (started) await refresh()
        return started
      } catch {
        await Promise.allSettled([refresh(), refreshPresets()])
        return false
      } finally {
        paymentLifecycle.finishRequest(lifecycleLease)
        processingRef.current = false
        setProcessing(false)
      }
    },
    [
      canManage,
      paymentLifecycle,
      refresh,
      refreshPresets,
      scope,
      startBillingAuth,
    ]
  )

  const cancel = useCallback(
    async (id: number) => {
      if (!canManage || processingRef.current) return false

      processingRef.current = true
      setProcessing(true)
      try {
        const response = await cancelWalletAutoRecharge(id, scope)
        if (!isApiSuccess(response)) {
          toast.error(response.message || i18next.t('Request failed'))
          return false
        }
        await refresh()
        return true
      } catch {
        toast.error(i18next.t('Request failed'))
        return false
      } finally {
        processingRef.current = false
        setProcessing(false)
      }
    },
    [canManage, refresh, scope]
  )

  return {
    policies,
    policiesLoaded,
    presets,
    presetsLoading,
    presetsLoaded,
    loading,
    processing,
    refresh,
    createScheduled,
    createThreshold,
    cancel,
  }
}
