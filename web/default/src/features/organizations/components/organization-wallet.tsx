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
import { useCallback, useEffect, useMemo, useState } from 'react'
import i18next from 'i18next'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { isApiSuccess } from '@/features/wallet/api'
import { AutoRechargeCard } from '@/features/wallet/components/auto-recharge-card'
import { BillingHistoryDialog } from '@/features/wallet/components/dialogs/billing-history-dialog'
import { CreemConfirmDialog } from '@/features/wallet/components/dialogs/creem-confirm-dialog'
import { PaymentConfirmDialog } from '@/features/wallet/components/dialogs/payment-confirm-dialog'
import { RechargeFormCard } from '@/features/wallet/components/recharge-form-card'
import { WalletStatsCard } from '@/features/wallet/components/wallet-stats-card'
import { DEFAULT_DISCOUNT_RATE } from '@/features/wallet/constants'
import { useTopupInfo, useWalletAutoRecharge } from '@/features/wallet/hooks'
import {
  getDefaultPaymentType,
  getMinTopupAmount,
  isPayPalPayment,
  isStripePayment,
  isWaffoPancakePayment,
  submitPaymentForm,
} from '@/features/wallet/lib'
import type {
  CreemProduct,
  PaymentMethod,
  PresetAmount,
  UserWalletData,
} from '@/features/wallet/types'
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
import type { Organization } from '../types'

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

export function canManageOrganizationAutoRecharge(
  ownerUserId: number | null | undefined,
  currentUserId: number | null | undefined
) {
  return (
    ownerUserId !== null &&
    ownerUserId !== undefined &&
    ownerUserId === currentUserId
  )
}

export function OrganizationWallet() {
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

  const currentUser = useAuthStore((state) => state.auth.user)
  const { status } = useStatus()
  const { currency } = useSystemConfig()
  const { topupInfo, presetAmounts, loading: topupLoading } = useTopupInfo()
  const canManageAutoRecharge = canManageOrganizationAutoRecharge(
    organization?.owner_user_id,
    currentUser?.id
  )
  const walletAutoRecharge = useWalletAutoRecharge(
    'organization',
    canManageAutoRecharge
  )

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

  const calculatePaymentAmount = useCallback(
    async (amount: number, paymentType: string) => {
      try {
        setCalculating(true)
        const request = { amount }
        const response = isStripePayment(paymentType)
          ? await calculateOrganizationStripeAmount(request)
          : isPayPalPayment(paymentType)
            ? await calculateOrganizationPayPalAmount(request)
            : isWaffoPancakePayment(paymentType)
              ? await calculateOrganizationWaffoPancakeAmount(request)
              : await calculateOrganizationAmount(request)

        if (isApiSuccess(response) && response.data) {
          const value = parseFloat(response.data)
          setPaymentAmount(value)
          return value
        }
        setPaymentAmount(0)
        return 0
      } catch {
        setPaymentAmount(0)
        return 0
      } finally {
        setCalculating(false)
      }
    },
    []
  )

  useEffect(() => {
    if (topupInfo && topupAmount === 0) {
      const minTopup = getMinTopupAmount(topupInfo)
      setTopupAmount(minTopup)
      void calculatePaymentAmount(minTopup, getDefaultPaymentType(topupInfo))
    }
  }, [calculatePaymentAmount, topupAmount, topupInfo])

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
      await calculatePaymentAmount(topupAmount, method.type)
      setConfirmDialogOpen(true)
    } finally {
      setPaymentLoading(null)
    }
  }

  const handlePaymentConfirm = async () => {
    if (!selectedPaymentMethod) return
    setProcessing(true)
    try {
      const amount = Math.floor(topupAmount)
      const paymentType = selectedPaymentMethod.type
      const response = isStripePayment(paymentType)
        ? await requestOrganizationStripePayment({
            amount,
            payment_method: 'stripe',
          })
        : isPayPalPayment(paymentType)
          ? await requestOrganizationPayPalPayment({
              amount,
              payment_method: 'paypal',
            })
          : isWaffoPancakePayment(paymentType)
            ? await requestOrganizationWaffoPancakePayment({ amount })
            : await requestOrganizationPayment({
                amount,
                payment_method: paymentType,
              })

      if (!isApiSuccess(response)) {
        toast.error(response.message || i18next.t('Payment request failed'))
        return
      }

      const payLink = getPayLink(response.data)
      if (
        (isStripePayment(paymentType) || isPayPalPayment(paymentType)) &&
        payLink
      ) {
        if (isPayPalPayment(paymentType)) {
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

      const formUrl = (response as unknown as { url?: string }).url
      const formData = isRecord(response.data) ? response.data : null
      if (formUrl && formData && !isSafeHttpUrl(formUrl)) {
        toast.error(t('Invalid payment redirect URL'))
        return
      }
      if (formUrl && formData) {
        submitPaymentForm(formUrl, formData)
        toast.success(t('Redirecting to payment page...'))
        setConfirmDialogOpen(false)
      }
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
        toast.error(response.message || t('Payment request failed'))
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setProcessing(false)
    }
  }

  const handleWaffoMethodSelect = async (_method: unknown, index: number) => {
    const loadingKey = `waffo-${index}`
    setPaymentLoading(loadingKey)
    setProcessing(true)
    try {
      const response = await requestOrganizationWaffoPayment({
        amount: Math.floor(topupAmount),
        pay_method_index: index,
      })
      if (isApiSuccess(response)) {
        const paymentUrl = getPaymentUrl(response.data)
        if (paymentUrl) {
          window.open(paymentUrl, '_blank')
          toast.success(t('Redirecting to payment page...'))
          return
        }
      }
      toast.error(response.message || t('Payment request failed'))
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

            <Tabs defaultValue='topup' className='w-full gap-4'>
              <TabsList className='grid w-full grid-cols-3 sm:w-fit'>
                <TabsTrigger value='topup'>{t('Top up')}</TabsTrigger>
                <TabsTrigger value='scheduled'>
                  {t('Scheduled recharge')}
                </TabsTrigger>
                <TabsTrigger value='threshold'>
                  {t('Auto recharge')}
                </TabsTrigger>
              </TabsList>

              <TabsContent value='topup'>
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
                    enableWaffoPancakeTopup={
                      topupInfo?.enable_waffo_pancake_topup
                    }
                    showRedemption={false}
                  />
                </div>
              </TabsContent>

              <TabsContent value='scheduled'>
                <AutoRechargeCard
                  mode='scheduled'
                  policies={walletAutoRecharge.policies}
                  loading={walletAutoRecharge.loading}
                  processing={walletAutoRecharge.processing}
                  canManage={canManageAutoRecharge}
                  permissionMessageKey='Only the organization owner can change auto payments'
                  minTopup={
                    topupInfo?.toss_min_topup || getMinTopupAmount(topupInfo)
                  }
                  onCreateScheduled={walletAutoRecharge.createScheduled}
                  onCreateThreshold={walletAutoRecharge.createThreshold}
                  onCancel={walletAutoRecharge.cancel}
                />
              </TabsContent>

              <TabsContent value='threshold'>
                <AutoRechargeCard
                  mode='threshold'
                  policies={walletAutoRecharge.policies}
                  loading={walletAutoRecharge.loading}
                  processing={walletAutoRecharge.processing}
                  canManage={canManageAutoRecharge}
                  permissionMessageKey='Only the organization owner can change auto payments'
                  minTopup={
                    topupInfo?.toss_min_topup || getMinTopupAmount(topupInfo)
                  }
                  onCreateScheduled={walletAutoRecharge.createScheduled}
                  onCreateThreshold={walletAutoRecharge.createThreshold}
                  onCancel={walletAutoRecharge.cancel}
                />
              </TabsContent>
            </Tabs>
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
        description={t('View organization topup transaction records')}
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
