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
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { loadTossPayments } from '@tosspayments/tosspayments-sdk'
import i18next from 'i18next'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { TitledCard } from '@/components/ui/titled-card'
import { SectionPageLayout } from '@/components/layout'
import { isApiSuccess } from '@/features/wallet/api'
import {
  AutoRechargeCard,
  getVisibleAutoRechargeModes,
} from '@/features/wallet/components/auto-recharge-card'
import { BillingHistoryDialog } from '@/features/wallet/components/dialogs/billing-history-dialog'
import { CreemConfirmDialog } from '@/features/wallet/components/dialogs/creem-confirm-dialog'
import { PaymentConfirmDialog } from '@/features/wallet/components/dialogs/payment-confirm-dialog'
import { RechargeFormCard } from '@/features/wallet/components/recharge-form-card'
import { WalletStatsCard } from '@/features/wallet/components/wallet-stats-card'
import { DEFAULT_DISCOUNT_RATE } from '@/features/wallet/constants'
import {
  useTopupInfo,
  useTossPaymentLifecycle,
  useWalletAutoRecharge,
} from '@/features/wallet/hooks'
import {
  getTossPaymentWindowTargetOptions,
  getDefaultPaymentType,
  getMinTopupAmount,
  isPayPalPayment,
  isStripePayment,
  isTossPayment,
  isTossUserCancellation,
  isValidTossPaymentSession,
  isWaffoPancakePayment,
  shouldBlockPaymentMethodBeforeQuote,
  shouldOpenPaymentConfirmDialog,
  submitPaymentForm,
} from '@/features/wallet/lib'
import type { TossPaymentLifecycleLease } from '@/features/wallet/lib'
import {
  buildWalletPaymentSettingTabs,
  getInitialWalletPaymentSetting,
  getWalletPaymentSettingTabsGridClass,
  WALLET_PAYMENT_SETTING_LOCK_MESSAGE,
  type WalletPaymentSettingKind,
} from '@/features/wallet/lib/payment-settings'
import {
  createTossPaymentConfirmation,
  getTossPreview,
  parseTossQuoteData,
  tossPaymentSessionMatchesConfirmation,
} from '@/features/wallet/lib/topup-amount-mode'
import type {
  CreemProduct,
  PaymentMethod,
  PresetAmount,
  TopupAmountMode,
  TossPaymentConfirmation,
  TossTopupQuote,
  UserWalletData,
  WalletAutoRechargePolicy,
  WalletAutoRechargeType,
} from '@/features/wallet/types'
import {
  calculateOrganizationAmount,
  calculateOrganizationPayPalAmount,
  calculateOrganizationStripeAmount,
  calculateOrganizationTossAmount,
  calculateOrganizationWaffoPancakeAmount,
  getOrganizationBillingHistory,
  getOrganizationWallet,
  requestOrganizationCreemPayment,
  requestOrganizationPayPalPayment,
  requestOrganizationPayment,
  requestOrganizationStripePayment,
  requestOrganizationTossPayment,
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

export function getOrganizationAmountModeRequest(
  amount: number,
  paymentType: string,
  amountMode: TopupAmountMode
) {
  return isTossPayment(paymentType)
    ? { amount, amount_mode: amountMode }
    : { amount }
}

export function shouldBlockOrganizationPaymentMethodBeforeQuote(
  method: Pick<PaymentMethod, 'type' | 'min_topup'>,
  amountForMinimum: number,
  fallbackMinimum: number
): boolean {
  return shouldBlockPaymentMethodBeforeQuote(
    method.type,
    amountForMinimum,
    method.min_topup || fallbackMinimum
  )
}

export function buildOrganizationWalletPaymentSettingTabs({
  visibleAutoRechargeModes,
  policies,
}: {
  visibleAutoRechargeModes: WalletAutoRechargeType[]
  policies: Array<Pick<WalletAutoRechargePolicy, 'type' | 'status'>>
}) {
  return buildWalletPaymentSettingTabs({
    hasActiveSubscription: false,
    visibleAutoRechargeModes,
    policies,
  })
}

interface OrganizationWalletProps {
  initialShowHistory?: boolean
}

export function OrganizationWallet({
  initialShowHistory = false,
}: OrganizationWalletProps = {}) {
  const { t } = useTranslation()
  const [paymentSettingTab, setPaymentSettingTab] =
    useState<WalletPaymentSettingKind | null>(null)
  const [organization, setOrganization] = useState<Organization | null>(null)
  const [organizationLoading, setOrganizationLoading] = useState(true)
  const [topupAmount, setTopupAmount] = useState(0)
  const [topupAmountMode, setTopupAmountMode] = useState<TopupAmountMode>('krw')
  const [selectedPreset, setSelectedPreset] = useState<number | null>(null)
  const [selectedPaymentMethod, setSelectedPaymentMethod] =
    useState<PaymentMethod>()
  const [paymentAmount, setPaymentAmount] = useState(0)
  const [tossQuote, setTossQuote] = useState<Partial<TossTopupQuote> | null>(
    null
  )
  const [tossPaymentConfirmation, setTossPaymentConfirmation] =
    useState<Readonly<TossPaymentConfirmation> | null>(null)
  const [calculating, setCalculating] = useState(false)
  const [processing, setProcessing] = useState(false)
  const [paymentLoading, setPaymentLoading] = useState<string | null>(null)
  const [confirmDialogOpen, setConfirmDialogOpen] = useState(false)
  const [billingDialogOpen, setBillingDialogOpen] = useState(false)
  const [creemDialogOpen, setCreemDialogOpen] = useState(false)
  const [selectedCreemProduct, setSelectedCreemProduct] =
    useState<CreemProduct | null>(null)
  const calculationRequestIdRef = useRef(0)
  const tossPaymentProcessingRef = useRef(false)
  const tossPaymentLifecycle = useTossPaymentLifecycle()

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

  useEffect(() => {
    if (!initialShowHistory) return
    setBillingDialogOpen(true)
    // Remove callback-only query data without adding a history entry that can
    // replay the Toss result when the user navigates back.
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

  const getAmountForMinimumCheck = useCallback(
    (amount: number, paymentType: string, amountMode: TopupAmountMode) => {
      if (isTossPayment(paymentType) && amountMode === 'quota') {
        return getTossPreview(amount, amountMode, topupInfo?.toss_unit_price)
          .chargeAmount
      }
      return amount
    },
    [topupInfo?.toss_unit_price]
  )

  const calculatePaymentAmount = useCallback(
    async (
      amount: number,
      paymentType: string,
      amountMode: TopupAmountMode = topupAmountMode
    ) => {
      const requestId = ++calculationRequestIdRef.current
      try {
        setCalculating(true)
        const request = getOrganizationAmountModeRequest(
          amount,
          paymentType,
          amountMode
        )
        const response = isStripePayment(paymentType)
          ? await calculateOrganizationStripeAmount(request)
          : isPayPalPayment(paymentType)
            ? await calculateOrganizationPayPalAmount(request)
            : isWaffoPancakePayment(paymentType)
              ? await calculateOrganizationWaffoPancakeAmount(request)
              : isTossPayment(paymentType)
                ? await calculateOrganizationTossAmount(request)
                : await calculateOrganizationAmount(request)

        if (requestId !== calculationRequestIdRef.current) {
          return { amount: 0, tossQuote: null, isCurrent: false }
        }

        if (isApiSuccess(response) && response.data) {
          const quote = isTossPayment(paymentType)
            ? parseTossQuoteData(response.data)
            : null
          setTossQuote(isTossPayment(paymentType) ? quote : null)
          const value =
            quote?.charge_amount ?? parseFloat(String(response.data))
          setPaymentAmount(value)
          return {
            amount: value,
            tossQuote: isTossPayment(paymentType) ? quote : null,
            isCurrent: true,
          }
        }
        setTossQuote(null)
        setPaymentAmount(0)
        return { amount: 0, tossQuote: null, isCurrent: true }
      } catch {
        const isCurrent = requestId === calculationRequestIdRef.current
        if (isCurrent) {
          setTossQuote(null)
          setPaymentAmount(0)
        }
        return { amount: 0, tossQuote: null, isCurrent }
      } finally {
        if (requestId === calculationRequestIdRef.current) {
          setCalculating(false)
        }
      }
    },
    [topupAmountMode]
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

  const handleTopupAmountModeChange = (mode: TopupAmountMode) => {
    setTossPaymentConfirmation(null)
    setTopupAmountMode(mode)
    setSelectedPreset(null)
    void calculatePaymentAmount(topupAmount, getCurrentPaymentType(), mode)
  }

  const handleSelectPreset = (preset: PresetAmount) => {
    setTossPaymentConfirmation(null)
    setTopupAmount(preset.value)
    setSelectedPreset(preset.value)
    void calculatePaymentAmount(preset.value, getCurrentPaymentType())
  }

  const handleTopupAmountChange = (amount: number) => {
    setTossPaymentConfirmation(null)
    setTopupAmount(amount)
    setSelectedPreset(null)
    void calculatePaymentAmount(amount, getCurrentPaymentType())
  }

  const handlePaymentMethodSelect = async (method: PaymentMethod) => {
    setSelectedPaymentMethod(method)
    setPaymentLoading(method.type)
    try {
      const amountForMinimum = getAmountForMinimumCheck(
        topupAmount,
        method.type,
        topupAmountMode
      )
      if (
        shouldBlockOrganizationPaymentMethodBeforeQuote(
          method,
          amountForMinimum,
          getMinTopupAmount(topupInfo)
        )
      ) {
        return
      }
      const calculation = await calculatePaymentAmount(topupAmount, method.type)
      if (!calculation.isCurrent) return
      if (shouldOpenPaymentConfirmDialog(method.type, calculation.amount)) {
        if (isTossPayment(method.type)) {
          const confirmation = createTossPaymentConfirmation(
            topupAmount,
            topupAmountMode,
            calculation.tossQuote
          )
          if (!confirmation) {
            toast.error(t('Payment request failed'))
            return
          }
          setTossPaymentConfirmation(confirmation)
        } else {
          setTossPaymentConfirmation(null)
        }
        setConfirmDialogOpen(true)
      }
    } finally {
      setPaymentLoading(null)
    }
  }

  const handlePaymentConfirm = async () => {
    if (!selectedPaymentMethod) return
    const paymentType = selectedPaymentMethod.type
    let tossPaymentLease: TossPaymentLifecycleLease | null = null
    if (
      isTossPayment(paymentType) &&
      (!tossPaymentConfirmation ||
        topupAmount !== tossPaymentConfirmation.input_amount ||
        topupAmountMode !== tossPaymentConfirmation.amount_mode)
    ) {
      toast.info(t('Payment details changed. Please review and confirm again.'))
      setConfirmDialogOpen(false)
      setTossPaymentConfirmation(null)
      return
    }
    if (isTossPayment(paymentType)) {
      if (tossPaymentProcessingRef.current) return
      const lifecycleLease = tossPaymentLifecycle.beginRequest()
      if (lifecycleLease === null) return
      tossPaymentProcessingRef.current = true
      tossPaymentLease = lifecycleLease
    }
    setProcessing(true)
    try {
      const amount = Math.floor(
        isTossPayment(paymentType)
          ? (tossPaymentConfirmation?.input_amount ?? 0)
          : topupAmount
      )
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
            : isTossPayment(paymentType)
              ? await requestOrganizationTossPayment({
                  amount,
                  amount_mode: tossPaymentConfirmation?.amount_mode,
                  payment_method: 'toss',
                })
              : await requestOrganizationPayment({
                  amount,
                  payment_method: paymentType,
                })

      if (!isApiSuccess(response)) {
        toast.error(response.message || i18next.t('Payment request failed'))
        return
      }

      if (isTossPayment(paymentType)) {
        const data = response.data
        if (!isValidTossPaymentSession(data)) {
          toast.error(response.message || i18next.t('Payment request failed'))
          return
        }
        if (
          !tossPaymentConfirmation ||
          !tossPaymentSessionMatchesConfirmation(data, tossPaymentConfirmation)
        ) {
          toast.info(
            t('Payment details changed. Please review and confirm again.')
          )
          setConfirmDialogOpen(false)
          setTossPaymentConfirmation(null)
          return
        }
        const {
          client_key,
          customer_key,
          order_id,
          order_name,
          amount: chargeAmount,
          success_url,
          fail_url,
        } = data

        try {
          const tossPayments = await loadTossPayments(client_key)
          const payment = tossPayments.payment({ customerKey: customer_key })
          if (!(await tossPaymentLifecycle.adopt(payment, tossPaymentLease))) {
            toast.error(t('Payment request failed'))
            return
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
          // Navigation is the normal SDK outcome. If a popup-capable browser
          // resolves the promise without replacing this page, close the
          // confirmation so the same reviewed quote cannot mint another order.
          setConfirmDialogOpen(false)
          setTossPaymentConfirmation(null)
        } catch (error) {
          if (isTossUserCancellation(error)) {
            toast.info(t('Cancelled'))
          } else {
            toast.error(t('Payment request failed'))
          }
        }
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
      if (isTossPayment(paymentType)) {
        tossPaymentLifecycle.finishRequest(tossPaymentLease)
        tossPaymentProcessingRef.current = false
      }
      setProcessing(false)
    }
  }

  const handleConfirmDialogOpenChange = (open: boolean) => {
    setConfirmDialogOpen(open)
    if (!open) {
      setTossPaymentConfirmation(null)
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

  const visibleAutoRechargeModes = useMemo(
    () =>
      getVisibleAutoRechargeModes(
        walletAutoRecharge.presets.filter((preset) => preset.enabled),
        walletAutoRecharge.policies,
        walletAutoRecharge.presetsLoaded,
        walletAutoRecharge.policiesLoaded
      ),
    [
      walletAutoRecharge.policies,
      walletAutoRecharge.presets,
      walletAutoRecharge.policiesLoaded,
      walletAutoRecharge.presetsLoaded,
    ]
  )
  const walletAutoRechargeGatewayEnabled =
    topupInfo?.enable_toss_wallet_auto_recharge === true

  const paymentSettingTabs = useMemo(
    () =>
      buildOrganizationWalletPaymentSettingTabs({
        visibleAutoRechargeModes,
        policies: walletAutoRecharge.policies,
      }),
    [visibleAutoRechargeModes, walletAutoRecharge.policies]
  )

  const paymentSettingLockMessageKey = paymentSettingTabs.find(
    (tab) => tab.disabled && tab.disabledMessageKey
  )?.disabledMessageKey

  useEffect(() => {
    const currentTab = paymentSettingTabs.find(
      (tab) => tab.kind === paymentSettingTab
    )
    if (currentTab && !currentTab.disabled) {
      return
    }
    setPaymentSettingTab(getInitialWalletPaymentSetting(paymentSettingTabs))
  }, [paymentSettingTab, paymentSettingTabs])

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

            <div
              className={
                paymentSettingTabs.length > 0
                  ? 'grid gap-4 xl:grid-cols-[minmax(0,1.05fr)_minmax(360px,0.95fr)] xl:items-start'
                  : 'grid gap-4'
              }
            >
              <div id='organization-wallet-add-funds' className='scroll-mt-4'>
                <RechargeFormCard
                  topupInfo={topupInfo}
                  presetAmounts={presetAmounts}
                  selectedPreset={selectedPreset}
                  onSelectPreset={handleSelectPreset}
                  topupAmount={topupAmount}
                  onTopupAmountChange={handleTopupAmountChange}
                  amountMode={topupAmountMode}
                  onAmountModeChange={handleTopupAmountModeChange}
                  tossUnitPrice={topupInfo?.toss_unit_price}
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

              {paymentSettingTabs.length > 0 && paymentSettingTab ? (
                <TitledCard
                  title={t('Payment settings')}
                  description={t('Choose one active payment setting at a time')}
                  contentClassName='space-y-4'
                >
                  <Tabs
                    value={paymentSettingTab}
                    onValueChange={(value) =>
                      setPaymentSettingTab(value as WalletPaymentSettingKind)
                    }
                  >
                    <TabsList
                      className={getWalletPaymentSettingTabsGridClass(
                        paymentSettingTabs.length
                      )}
                    >
                      {paymentSettingTabs.map((tab) => (
                        <TabsTrigger
                          key={tab.kind}
                          value={tab.kind}
                          disabled={tab.disabled}
                        >
                          {tab.kind === 'threshold'
                            ? t('Auto recharge')
                            : t('Scheduled recharge')}
                        </TabsTrigger>
                      ))}
                    </TabsList>
                    {paymentSettingLockMessageKey ? (
                      <p className='text-muted-foreground text-sm'>
                        {t(paymentSettingLockMessageKey)}
                      </p>
                    ) : null}

                    <TabsContent value='threshold'>
                      <AutoRechargeCard
                        mode='threshold'
                        policies={walletAutoRecharge.policies}
                        presets={walletAutoRecharge.presets}
                        loading={walletAutoRecharge.loading}
                        processing={walletAutoRecharge.processing}
                        canManage={canManageAutoRecharge}
                        permissionMessageKey='Only the organization owner can change auto payments'
                        creationDisabled={
                          !walletAutoRechargeGatewayEnabled ||
                          (paymentSettingTabs.find(
                            (tab) => tab.kind === 'threshold'
                          )?.disabled ??
                            false)
                        }
                        creationDisabledMessageKey={
                          walletAutoRechargeGatewayEnabled
                            ? WALLET_PAYMENT_SETTING_LOCK_MESSAGE
                            : 'Not available'
                        }
                        onCreateScheduled={walletAutoRecharge.createScheduled}
                        onCreateThreshold={walletAutoRecharge.createThreshold}
                        onCancel={walletAutoRecharge.cancel}
                      />
                    </TabsContent>

                    <TabsContent value='scheduled'>
                      <AutoRechargeCard
                        mode='scheduled'
                        policies={walletAutoRecharge.policies}
                        presets={walletAutoRecharge.presets}
                        loading={walletAutoRecharge.loading}
                        processing={walletAutoRecharge.processing}
                        canManage={canManageAutoRecharge}
                        permissionMessageKey='Only the organization owner can change auto payments'
                        creationDisabled={
                          !walletAutoRechargeGatewayEnabled ||
                          (paymentSettingTabs.find(
                            (tab) => tab.kind === 'scheduled'
                          )?.disabled ??
                            false)
                        }
                        creationDisabledMessageKey={
                          walletAutoRechargeGatewayEnabled
                            ? WALLET_PAYMENT_SETTING_LOCK_MESSAGE
                            : 'Not available'
                        }
                        onCreateScheduled={walletAutoRecharge.createScheduled}
                        onCreateThreshold={walletAutoRecharge.createThreshold}
                        onCancel={walletAutoRecharge.cancel}
                      />
                    </TabsContent>
                  </Tabs>
                </TitledCard>
              ) : null}
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <PaymentConfirmDialog
        open={confirmDialogOpen}
        onOpenChange={handleConfirmDialogOpenChange}
        onConfirm={handlePaymentConfirm}
        topupAmount={tossPaymentConfirmation?.input_amount ?? topupAmount}
        paymentAmount={tossPaymentConfirmation?.charge_amount ?? paymentAmount}
        paymentMethod={selectedPaymentMethod}
        amountMode={tossPaymentConfirmation?.amount_mode ?? topupAmountMode}
        tossUnitPrice={
          tossPaymentConfirmation?.unit_price ?? topupInfo?.toss_unit_price
        }
        tossQuote={tossPaymentConfirmation ?? tossQuote}
        calculating={tossPaymentConfirmation ? false : calculating}
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
