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
import i18next from 'i18next'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { isApiSuccess } from '@/features/wallet/api'
import { BillingHistoryDialog } from '@/features/wallet/components/dialogs/billing-history-dialog'
import { CreemConfirmDialog } from '@/features/wallet/components/dialogs/creem-confirm-dialog'
import { PaymentConfirmDialog } from '@/features/wallet/components/dialogs/payment-confirm-dialog'
import { RechargeFormCard } from '@/features/wallet/components/recharge-form-card'
import { WalletStatsCard } from '@/features/wallet/components/wallet-stats-card'
import { DEFAULT_DISCOUNT_RATE } from '@/features/wallet/constants'
import {
  requestPaymentAmount,
  useTopupInfo,
  type PaymentAmountCalculators,
} from '@/features/wallet/hooks'
import {
  getDefaultPaymentType,
  getMinTopupAmount,
  getPaymentErrorTranslation,
  submitPaymentForm,
} from '@/features/wallet/lib'
import type {
  ApiResponse as WalletApiResponse,
  CreemProduct,
  PaymentMethod,
  PresetAmount,
  UserWalletData,
  WaffoPayMethod,
} from '@/features/wallet/types'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'

import {
  calculateOrganizationAmount,
  calculateOrganizationPayPalAmount,
  calculateOrganizationStripeAmount,
  calculateOrganizationWaffoPancakeAmount,
  getOrganizationBillingHistory,
  getOrganizationWallet,
  requestOrganizationCreemPayment,
  requestOrganizationPayPalPayment,
  requestOrganizationPayment,
  requestOrganizationStripePayment,
  requestOrganizationWaffoPancakePayment,
  requestOrganizationWaffoPayment,
} from '../api'
import { getOrganizationWalletPaymentFlow } from '../lib/organization-wallet-payment'
import type { Organization } from '../types'

/**
 * Amount quotes for the organization wallet come from the organization
 * endpoints. Waffo has no organization quote client, so it is priced by the
 * regular organization calculator, as the team wallet did.
 */
const ORGANIZATION_AMOUNT_CALCULATORS: PaymentAmountCalculators = {
  regular: calculateOrganizationAmount,
  stripe: calculateOrganizationStripeAmount,
  paypal: calculateOrganizationPayPalAmount,
  waffo: calculateOrganizationAmount,
  waffoPancake: calculateOrganizationWaffoPancakeAmount,
}

function getPaymentUrl(data: unknown): string | null {
  if (!data || typeof data !== 'object') return null
  if ('payment_url' in data && typeof data.payment_url === 'string') {
    return data.payment_url
  }
  if ('checkout_url' in data && typeof data.checkout_url === 'string') {
    return data.checkout_url
  }
  return null
}

function getPayLink(data: unknown): string | null {
  if (!data || typeof data !== 'object') return null
  if ('pay_link' in data && typeof data.pay_link === 'string') {
    return data.pay_link
  }
  return null
}

function isRecord(data: unknown): data is Record<string, unknown> {
  return !!data && typeof data === 'object'
}

function isSafeHttpUrl(value: string): boolean {
  try {
    const url = new URL(value.trim())
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

/**
 * Organization payment endpoints answer failures (including the owner-only
 * check) as {"message": "error", "data": text}; show the text, not "error".
 */
function getPaymentErrorMessage(response: WalletApiResponse<unknown>): string {
  const translated = getPaymentErrorTranslation(response)
  if (translated) return translated
  if (typeof response.data === 'string' && response.data.trim()) {
    return response.data
  }
  if (
    response.message &&
    response.message !== 'error' &&
    response.message !== 'success'
  ) {
    return response.message
  }
  return i18next.t('Payment request failed')
}

interface OrganizationWalletProps {
  initialShowHistory?: boolean
}

export function OrganizationWallet({
  initialShowHistory = false,
}: OrganizationWalletProps = {}) {
  const { t } = useTranslation()
  const [organization, setOrganization] = useState<Organization | null>(null)
  const [organizationLoading, setOrganizationLoading] = useState(true)
  const [topupAmount, setTopupAmount] = useState(0)
  const [selectedPreset, setSelectedPreset] = useState<number | null>(null)
  const [selectedPaymentMethod, setSelectedPaymentMethod] =
    useState<PaymentMethod>()
  const [paymentAmount, setPaymentAmount] = useState(0)
  const [calculating, setCalculating] = useState(false)
  const [processing, setProcessing] = useState(false)
  const [paymentLoading, setPaymentLoading] = useState<string | null>(null)
  const [confirmDialogOpen, setConfirmDialogOpen] = useState(false)
  const [billingDialogOpen, setBillingDialogOpen] = useState(false)
  const [creemDialogOpen, setCreemDialogOpen] = useState(false)
  const [selectedCreemProduct, setSelectedCreemProduct] =
    useState<CreemProduct | null>(null)
  const calculationRequestIdRef = useRef(0)
  const topupAmountInitializedRef = useRef(false)

  const { status } = useStatus()
  const { currency } = useSystemConfig()
  const { topupInfo, presetAmounts, loading: topupLoading } = useTopupInfo()

  useEffect(() => {
    if (!initialShowHistory) return
    setBillingDialogOpen(true)
    // Drop the callback-only query without adding a history entry.
    window.history.replaceState({}, '', window.location.pathname)
  }, [initialShowHistory])

  const effectiveUsdExchangeRate = useMemo(() => {
    return currency?.quotaDisplayType === 'USD'
      ? 1
      : currency?.usdExchangeRate || 1
  }, [currency?.quotaDisplayType, currency?.usdExchangeRate])

  const walletUser = useMemo<UserWalletData | null>(() => {
    if (!organization) return null
    return {
      id: organization.id,
      username: organization.name,
      quota: organization.quota,
      used_quota: organization.used_quota,
      request_count: 0,
      aff_quota: 0,
      aff_history_quota: 0,
      aff_count: 0,
      group: '',
    }
  }, [organization])

  const fetchOrganization = useCallback(async () => {
    try {
      setOrganizationLoading(true)
      const response = await getOrganizationWallet()
      if (response.success && response.data) {
        setOrganization(response.data)
      } else {
        toast.error(response.message || t('Failed to load organization'))
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch organization wallet:', error)
      toast.error(t('Failed to load organization'))
    } finally {
      setOrganizationLoading(false)
    }
  }, [t])

  useEffect(() => {
    void fetchOrganization()
  }, [fetchOrganization])

  /** Returns false when a newer quote superseded this one. */
  const calculatePaymentAmount = useCallback(
    async (amount: number, paymentType: string): Promise<boolean> => {
      const requestId = ++calculationRequestIdRef.current
      try {
        setCalculating(true)
        const value = await requestPaymentAmount(
          amount,
          paymentType,
          ORGANIZATION_AMOUNT_CALCULATORS
        )
        if (requestId !== calculationRequestIdRef.current) return false
        setPaymentAmount(value)
        return true
      } catch {
        if (requestId !== calculationRequestIdRef.current) return false
        setPaymentAmount(0)
        return true
      } finally {
        if (requestId === calculationRequestIdRef.current) {
          setCalculating(false)
        }
      }
    },
    []
  )

  useEffect(() => {
    if (!topupInfo || topupAmountInitializedRef.current) return
    topupAmountInitializedRef.current = true
    const minTopup = getMinTopupAmount(topupInfo)
    setTopupAmount(minTopup)
    void calculatePaymentAmount(minTopup, getDefaultPaymentType(topupInfo))
  }, [calculatePaymentAmount, topupInfo])

  const getCurrentPaymentType = useCallback(() => {
    return selectedPaymentMethod?.type || getDefaultPaymentType(topupInfo)
  }, [selectedPaymentMethod, topupInfo])

  const handleSelectPreset = (preset: PresetAmount) => {
    setTopupAmount(preset.value)
    setSelectedPreset(preset.value)
    void calculatePaymentAmount(preset.value, getCurrentPaymentType())
  }

  const handleTopupAmountChange = (amount: number) => {
    setTopupAmount(amount)
    setSelectedPreset(null)
    void calculatePaymentAmount(amount, getCurrentPaymentType())
  }

  const handlePaymentMethodSelect = async (method: PaymentMethod) => {
    setSelectedPaymentMethod(method)
    setPaymentLoading(method.type)
    try {
      if (topupAmount < getMinTopupAmount(topupInfo)) return
      const isCurrent = await calculatePaymentAmount(topupAmount, method.type)
      if (!isCurrent) return
      setConfirmDialogOpen(true)
    } finally {
      setPaymentLoading(null)
    }
  }

  const handlePaymentConfirm = async () => {
    if (!selectedPaymentMethod) return
    const paymentType = selectedPaymentMethod.type
    const flow = getOrganizationWalletPaymentFlow(paymentType)
    setProcessing(true)
    try {
      const amount = Math.floor(topupAmount)
      let response: WalletApiResponse<unknown> & { url?: string }
      switch (flow) {
        case 'stripe':
          response = await requestOrganizationStripePayment({
            amount,
            payment_method: 'stripe',
          })
          break
        case 'paypal':
          response = await requestOrganizationPayPalPayment({
            amount,
            payment_method: 'paypal',
          })
          break
        case 'waffo_pancake':
          response = await requestOrganizationWaffoPancakePayment({ amount })
          break
        default:
          response = await requestOrganizationPayment({
            amount,
            payment_method: paymentType,
          })
      }

      if (!isApiSuccess(response)) {
        toast.error(getPaymentErrorMessage(response))
        return
      }

      if (flow === 'stripe' || flow === 'paypal') {
        const payLink = getPayLink(response.data)
        if (!payLink || !isSafeHttpUrl(payLink)) {
          toast.error(t('Invalid payment redirect URL'))
          return
        }
        if (flow === 'paypal') {
          // PayPal returns the browser to /api/paypal/capture, so the current
          // window must navigate.
          window.location.href = payLink
        } else {
          window.open(payLink, '_blank')
        }
        toast.success(t('Redirecting to payment page...'))
        setConfirmDialogOpen(false)
        return
      }

      const redirectUrl = getPaymentUrl(response.data)
      if (redirectUrl) {
        if (!isSafeHttpUrl(redirectUrl)) {
          toast.error(t('Invalid payment redirect URL'))
          return
        }
        window.location.href = redirectUrl
        toast.success(t('Redirecting to payment page...'))
        setConfirmDialogOpen(false)
        return
      }

      const formUrl = response.url
      const formData = isRecord(response.data) ? response.data : null
      if (flow === 'generic' && formUrl && formData) {
        if (!isSafeHttpUrl(formUrl)) {
          toast.error(t('Invalid payment redirect URL'))
          return
        }
        submitPaymentForm(formUrl, formData)
        toast.success(t('Redirecting to payment page...'))
        setConfirmDialogOpen(false)
        return
      }

      toast.error(t('Payment request failed'))
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setProcessing(false)
    }
  }

  const handleCreemProductSelect = (product: CreemProduct) => {
    setSelectedCreemProduct(product)
    setCreemDialogOpen(true)
  }

  const handleCreemConfirm = async () => {
    if (!selectedCreemProduct) return
    setProcessing(true)
    try {
      const response = await requestOrganizationCreemPayment({
        product_id: selectedCreemProduct.productId,
        payment_method: 'creem',
      })
      if (isApiSuccess(response) && response.data?.checkout_url) {
        window.open(response.data.checkout_url, '_blank')
        toast.success(t('Redirecting to Creem checkout...'))
        setCreemDialogOpen(false)
        setSelectedCreemProduct(null)
      } else {
        toast.error(getPaymentErrorMessage(response))
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setProcessing(false)
    }
  }

  // Waffo opens its hosted page directly (no organization quote client).
  const handleWaffoMethodSelect = async (
    _method: WaffoPayMethod,
    index: number
  ) => {
    const minTopup = getMinTopupAmount(topupInfo)
    if (topupAmount < minTopup) {
      toast.error(t('Minimum topup amount: {{amount}}', { amount: minTopup }))
      return
    }
    setPaymentLoading(`waffo-${index}`)
    setProcessing(true)
    try {
      const response = await requestOrganizationWaffoPayment({
        amount: Math.floor(topupAmount),
        pay_method_index: index,
      })
      if (isApiSuccess(response)) {
        const paymentUrl = getPaymentUrl(response.data)
        if (paymentUrl) {
          if (!isSafeHttpUrl(paymentUrl)) {
            toast.error(t('Invalid payment redirect URL'))
            return
          }
          window.open(paymentUrl, '_blank')
          toast.success(t('Redirecting to payment page...'))
          return
        }
      }
      toast.error(getPaymentErrorMessage(response))
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaymentLoading(null)
      setProcessing(false)
    }
  }

  const getDiscountRate = useCallback(() => {
    return topupInfo?.discount?.[topupAmount] || DEFAULT_DISCOUNT_RATE
  }, [topupAmount, topupInfo])

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          <span className='inline-flex items-center gap-2'>
            {t('Organization wallet')}
            {organization?.name ? (
              <Badge variant='secondary'>{organization.name}</Badge>
            ) : null}
          </span>
        </SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
            <WalletStatsCard user={walletUser} loading={organizationLoading} />

            <div id='organization-wallet-add-funds' className='scroll-mt-4'>
              <RechargeFormCard
                topupInfo={topupInfo}
                presetAmounts={presetAmounts}
                selectedPreset={selectedPreset}
                onSelectPreset={handleSelectPreset}
                topupAmount={topupAmount}
                onTopupAmountChange={handleTopupAmountChange}
                paymentAmount={paymentAmount}
                calculating={calculating}
                onPaymentMethodSelect={handlePaymentMethodSelect}
                paymentLoading={paymentLoading}
                redemptionCode=''
                onRedemptionCodeChange={() => undefined}
                onRedeem={() => undefined}
                redeeming={false}
                loading={topupLoading}
                priceRatio={(status?.price as number) || 1}
                usdExchangeRate={effectiveUsdExchangeRate}
                onOpenBilling={() => setBillingDialogOpen(true)}
                creemProducts={topupInfo?.creem_products}
                enableCreemTopup={topupInfo?.enable_creem_topup}
                onCreemProductSelect={handleCreemProductSelect}
                enableWaffoTopup={topupInfo?.enable_waffo_topup}
                waffoPayMethods={topupInfo?.waffo_pay_methods}
                waffoMinTopup={topupInfo?.waffo_min_topup}
                onWaffoMethodSelect={handleWaffoMethodSelect}
                enableWaffoPancakeTopup={topupInfo?.enable_waffo_pancake_topup}
                showRedemption={false}
              />
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <PaymentConfirmDialog
        open={confirmDialogOpen}
        onOpenChange={setConfirmDialogOpen}
        onConfirm={handlePaymentConfirm}
        topupAmount={topupAmount}
        paymentAmount={paymentAmount}
        paymentMethod={selectedPaymentMethod}
        calculating={calculating}
        processing={processing}
        discountRate={getDiscountRate()}
        usdExchangeRate={effectiveUsdExchangeRate}
      />

      <BillingHistoryDialog
        open={billingDialogOpen}
        onOpenChange={setBillingDialogOpen}
        getBillingHistory={getOrganizationBillingHistory}
        forceSelfHistory
        description={t('Organization wallet top-up history')}
      />

      <CreemConfirmDialog
        open={creemDialogOpen}
        onOpenChange={setCreemDialogOpen}
        onConfirm={handleCreemConfirm}
        product={selectedCreemProduct}
        processing={processing}
      />
    </>
  )
}
