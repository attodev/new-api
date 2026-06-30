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
import { useCallback, useEffect, useState } from 'react'
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
import type {
  WalletAutoRechargePolicy,
  WalletAutoRechargePreset,
  WalletAutoRechargeRequest,
  WalletAutoRechargeTossResponse,
} from '../types'

export function useWalletAutoRecharge(
  scope: 'user' | 'organization',
  canManage: boolean
) {
  const [policies, setPolicies] = useState<WalletAutoRechargePolicy[]>([])
  const [presets, setPresets] = useState<WalletAutoRechargePreset[]>([])
  const [loading, setLoading] = useState(false)
  const [processing, setProcessing] = useState(false)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const response = await getWalletAutoRecharge(scope)
      if (isApiSuccess(response) && Array.isArray(response.data)) {
        setPolicies(response.data)
      }
    } finally {
      setLoading(false)
    }
  }, [scope])

  const refreshPresets = useCallback(async () => {
    const response = await getWalletAutoRechargePresets(scope)
    if (isApiSuccess(response) && Array.isArray(response.data)) {
      setPresets(response.data)
    }
  }, [scope])

  useEffect(() => {
    void refresh()
    void refreshPresets()
  }, [refresh, refreshPresets])

  const startBillingAuth = useCallback(
    async (response: WalletAutoRechargeTossResponse) => {
      if (!isApiSuccess(response) || !response.data) {
        toast.error(response.message || i18next.t('Payment request failed'))
        return false
      }

      const { client_key, customer_key, success_url, fail_url, trade_no } =
        response.data
      if (!client_key || !customer_key || !success_url || !fail_url) {
        toast.error(i18next.t('Payment request failed'))
        return false
      }

      try {
        const tossPayments = await loadTossPayments(client_key)
        const payment = tossPayments.payment({ customerKey: customer_key })
        await payment.requestBillingAuth({
          method: 'CARD',
          successUrl: success_url,
          failUrl: fail_url,
        })
        return true
      } catch (err) {
        const error = err as { code?: string }
        if (error.code && error.code !== 'USER_CANCEL') {
          toast.error(i18next.t('Payment request failed'))
        }
        if (trade_no) {
          await cancelPendingWalletAutoRecharge(trade_no, scope).catch(() => {})
          await refresh()
        }
        return false
      }
    },
    [refresh, scope]
  )

  const createScheduled = useCallback(
    async (payload: WalletAutoRechargeRequest) => {
      if (!canManage) return false

      setProcessing(true)
      try {
        const response = await requestWalletScheduledRecharge(payload, scope)
        return await startBillingAuth(response)
      } finally {
        setProcessing(false)
      }
    },
    [canManage, scope, startBillingAuth]
  )

  const createThreshold = useCallback(
    async (payload: WalletAutoRechargeRequest) => {
      if (!canManage) return false

      setProcessing(true)
      try {
        const response = await requestWalletThresholdRecharge(payload, scope)
        return await startBillingAuth(response)
      } finally {
        setProcessing(false)
      }
    },
    [canManage, scope, startBillingAuth]
  )

  const cancel = useCallback(
    async (id: number) => {
      if (!canManage) return false

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
        setProcessing(false)
      }
    },
    [canManage, refresh, scope]
  )

  return {
    policies,
    presets,
    loading,
    processing,
    refresh,
    createScheduled,
    createThreshold,
    cancel,
  }
}
