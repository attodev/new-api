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
import { useState, useEffect, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'
import { getSelf } from '@/lib/api'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { TitledCard } from '@/components/ui/titled-card'
import { SectionPageLayout } from '@/components/layout'
import {
  cancelTossAutoRenew,
  getSelfSubscriptionFull,
  updateBillingPreference,
} from '@/features/subscriptions/api'
import type { UserSubscriptionRecord } from '@/features/subscriptions/types'
import { AffiliateRewardsCard } from './components/affiliate-rewards-card'
import {
  AutoRechargeCard,
  getVisibleAutoRechargeModes,
} from './components/auto-recharge-card'
import { BillingHistoryDialog } from './components/dialogs/billing-history-dialog'
import { CreemConfirmDialog } from './components/dialogs/creem-confirm-dialog'
import { PaymentConfirmDialog } from './components/dialogs/payment-confirm-dialog'
import { TransferDialog } from './components/dialogs/transfer-dialog'
import { RechargeFormCard } from './components/recharge-form-card'
import { SubscriptionPlansCard } from './components/subscription-plans-card'
import { WalletStatsCard } from './components/wallet-stats-card'
import { WalletSubscriptionStatusCard } from './components/wallet-subscription-status-card'
import { DEFAULT_DISCOUNT_RATE } from './constants'
import {
  useTopupInfo,
  usePayment,
  useAffiliate,
  useRedemption,
  useCreemPayment,
  useWalletAutoRecharge,
  useWaffoPayment,
  useWaffoPancakePayment,
  useTossPayment,
} from './hooks'
import {
  getDefaultPaymentType,
  getMinTopupAmount,
  isWaffoPancakePayment,
  isTossPayment,
  shouldBlockPaymentMethodBeforeQuote,
  shouldOpenPaymentConfirmDialog,
} from './lib'
import {
  buildWalletPaymentSettingTabs,
  getInitialWalletPaymentSetting,
  getWalletPaymentSettingTabsGridClass,
  WALLET_PAYMENT_SETTING_LOCK_MESSAGE,
  type WalletPaymentSettingKind,
} from './lib/payment-settings'
import {
  createTossPaymentConfirmation,
  getTossPreview,
} from './lib/topup-amount-mode'
import type {
  UserWalletData,
  PaymentMethod,
  PresetAmount,
  CreemProduct,
  TopupAmountMode,
  TossPaymentConfirmation,
} from './types'

interface WalletProps {
  initialShowHistory?: boolean
}

export function Wallet(props: WalletProps) {
  const { t } = useTranslation()
  const [paymentSettingTab, setPaymentSettingTab] =
    useState<WalletPaymentSettingKind | null>(null)
  const [user, setUser] = useState<UserWalletData | null>(null)
  const [userLoading, setUserLoading] = useState(true)
  const setAuthUser = useAuthStore((state) => state.auth.setUser)
  const [topupAmount, setTopupAmount] = useState(0)
  const [topupAmountMode, setTopupAmountMode] = useState<TopupAmountMode>('krw')
  const [selectedPreset, setSelectedPreset] = useState<number | null>(null)
  const [selectedPaymentMethod, setSelectedPaymentMethod] =
    useState<PaymentMethod>()
  const [paymentLoading, setPaymentLoading] = useState<string | null>(null)
  const [confirmDialogOpen, setConfirmDialogOpen] = useState(false)
  const [tossPaymentConfirmation, setTossPaymentConfirmation] =
    useState<Readonly<TossPaymentConfirmation> | null>(null)
  const [transferDialogOpen, setTransferDialogOpen] = useState(false)
  const [billingDialogOpen, setBillingDialogOpen] = useState(false)
  const [redemptionCode, setRedemptionCode] = useState('')
  const [creemDialogOpen, setCreemDialogOpen] = useState(false)
  const [selectedCreemProduct, setSelectedCreemProduct] =
    useState<CreemProduct | null>(null)
  const [activeSubscriptions, setActiveSubscriptions] = useState<
    UserSubscriptionRecord[]
  >([])
  const [allSubscriptions, setAllSubscriptions] = useState<
    UserSubscriptionRecord[]
  >([])
  const [billingPreference, setBillingPreference] =
    useState('subscription_first')
  const [subscriptionStatusKnown, setSubscriptionStatusKnown] = useState(false)
  const [subscriptionRefreshing, setSubscriptionRefreshing] = useState(false)
  const [cancellingAutoRenew, setCancellingAutoRenew] = useState(false)

  const { status } = useStatus()
  const { currency } = useSystemConfig()
  const { topupInfo, presetAmounts, loading: topupLoading } = useTopupInfo()
  const walletAutoRecharge = useWalletAutoRecharge('user', true)

  // Calculate effective exchange rate - when display type is USD, use rate of 1
  const effectiveUsdExchangeRate = useMemo(() => {
    return currency?.quotaDisplayType === 'USD'
      ? 1
      : currency?.usdExchangeRate || 1
  }, [currency?.quotaDisplayType, currency?.usdExchangeRate])
  const {
    amount: paymentAmount,
    calculating,
    processing,
    calculatePaymentAmount,
    processPayment,
    tossQuote,
  } = usePayment()
  const {
    affiliateLink,
    loading: affiliateLoading,
    transferQuota,
    transferring,
  } = useAffiliate()
  const { redeeming, redeemCode } = useRedemption()
  const { processing: creemProcessing, processCreemPayment } = useCreemPayment()
  const { processWaffoPayment } = useWaffoPayment()
  const { processing: pancakeProcessing, processWaffoPancakePayment } =
    useWaffoPancakePayment()
  const { processing: tossProcessing, processTossPayment } = useTossPayment()

  const getAmountModeForPaymentType = useCallback(
    (paymentType: string) =>
      isTossPayment(paymentType) ? topupAmountMode : undefined,
    [topupAmountMode]
  )

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

  // Fetch and refresh user data
  const fetchUser = useCallback(async () => {
    try {
      setUserLoading(true)
      const response = await getSelf()
      if (response.success && response.data) {
        const userData = response.data as UserWalletData
        setUser(userData)
        setAuthUser(userData as AuthUser)
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch user data:', error)
    } finally {
      setUserLoading(false)
    }
  }, [setAuthUser])

  useEffect(() => {
    fetchUser()
  }, [fetchUser])

  const fetchSelfSubscription = useCallback(async () => {
    try {
      const res = await getSelfSubscriptionFull()
      if (res.success && res.data) {
        setBillingPreference(
          res.data.billing_preference || 'subscription_first'
        )
        setActiveSubscriptions(res.data.subscriptions || [])
        setAllSubscriptions(res.data.all_subscriptions || [])
        setSubscriptionStatusKnown(true)
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch subscription data:', error)
    }
  }, [])

  useEffect(() => {
    const init = async () => {
      await fetchSelfSubscription()
    }
    void init()
  }, [fetchSelfSubscription])

  useEffect(() => {
    if (props.initialShowHistory) {
      setBillingDialogOpen(true)
      window.history.replaceState({}, '', window.location.pathname)
    }
  }, [props.initialShowHistory])

  // Initialize topup amount when topup info is loaded
  useEffect(() => {
    if (topupInfo && topupAmount === 0) {
      const minTopup = getMinTopupAmount(topupInfo)
      setTopupAmount(minTopup)

      // Calculate initial payment amount with default payment type
      const defaultPaymentType = getDefaultPaymentType(topupInfo)
      calculatePaymentAmount(
        minTopup,
        defaultPaymentType,
        getAmountModeForPaymentType(defaultPaymentType)
      )
    }
  }, [
    topupInfo,
    topupAmount,
    calculatePaymentAmount,
    getAmountModeForPaymentType,
  ])

  // Get current payment type (selected or default)
  const getCurrentPaymentType = useCallback(() => {
    return selectedPaymentMethod?.type || getDefaultPaymentType(topupInfo)
  }, [selectedPaymentMethod, topupInfo])

  const handleTopupAmountModeChange = (mode: TopupAmountMode) => {
    setTossPaymentConfirmation(null)
    setTopupAmountMode(mode)
    setSelectedPreset(null)
    const paymentType = getCurrentPaymentType()
    calculatePaymentAmount(
      topupAmount,
      paymentType,
      isTossPayment(paymentType) ? mode : undefined
    )
  }

  // Handle preset selection
  const handleSelectPreset = (preset: PresetAmount) => {
    setTossPaymentConfirmation(null)
    setTopupAmount(preset.value)
    setSelectedPreset(preset.value)
    const paymentType = getCurrentPaymentType()
    calculatePaymentAmount(
      preset.value,
      paymentType,
      getAmountModeForPaymentType(paymentType)
    )
  }

  // Handle topup amount change
  const handleTopupAmountChange = (amount: number) => {
    setTossPaymentConfirmation(null)
    setTopupAmount(amount)
    setSelectedPreset(null)
    const paymentType = getCurrentPaymentType()
    calculatePaymentAmount(
      amount,
      paymentType,
      getAmountModeForPaymentType(paymentType)
    )
  }

  // Handle payment method selection
  const handlePaymentMethodSelect = async (method: PaymentMethod) => {
    setSelectedPaymentMethod(method)
    setPaymentLoading(method.type)

    try {
      // Validate minimum topup
      const minTopup = method.min_topup || getMinTopupAmount(topupInfo)
      const amountForMinimum = getAmountForMinimumCheck(
        topupAmount,
        method.type,
        topupAmountMode
      )
      if (
        shouldBlockPaymentMethodBeforeQuote(
          method.type,
          amountForMinimum,
          minTopup
        )
      ) {
        return
      }

      // Calculate payment amount and show confirmation dialog
      const amountMode = getAmountModeForPaymentType(method.type)
      const calculation = await calculatePaymentAmount(
        topupAmount,
        method.type,
        amountMode
      )
      if (!calculation.isCurrent) return
      if (shouldOpenPaymentConfirmDialog(method.type, calculation.amount)) {
        if (isTossPayment(method.type)) {
          const confirmation = createTossPaymentConfirmation(
            topupAmount,
            amountMode ?? topupAmountMode,
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

  // Handle payment confirmation
  const handlePaymentConfirm = async () => {
    if (!selectedPaymentMethod) return

    const type = selectedPaymentMethod.type
    let success = false
    if (isWaffoPancakePayment(type)) {
      success = await processWaffoPancakePayment(topupAmount)
    } else if (isTossPayment(type)) {
      if (
        !tossPaymentConfirmation ||
        topupAmount !== tossPaymentConfirmation.input_amount ||
        topupAmountMode !== tossPaymentConfirmation.amount_mode
      ) {
        toast.info(
          t('Payment details changed. Please review and confirm again.')
        )
        setConfirmDialogOpen(false)
        setTossPaymentConfirmation(null)
        return
      }
      const result = await processTossPayment(tossPaymentConfirmation)
      if (result === 'confirmation_changed') {
        setConfirmDialogOpen(false)
        setTossPaymentConfirmation(null)
        return
      }
      success = result === 'started'
    } else {
      success = await processPayment(topupAmount, type)
    }

    if (success) {
      setConfirmDialogOpen(false)
      await fetchUser()
    }
  }

  const handleConfirmDialogOpenChange = (open: boolean) => {
    setConfirmDialogOpen(open)
    if (!open) {
      setTossPaymentConfirmation(null)
    }
  }

  // Handle redemption
  const handleRedeem = async () => {
    if (!redemptionCode) return

    const success = await redeemCode(redemptionCode)
    if (success) {
      setRedemptionCode('')
      await fetchUser()
    }
  }

  // Handle transfer
  const handleTransfer = async (amount: number) => {
    const success = await transferQuota(amount)
    if (success) {
      await fetchUser()
    }
    return success
  }

  // Handle Creem product selection
  const handleCreemProductSelect = (product: CreemProduct) => {
    setSelectedCreemProduct(product)
    setCreemDialogOpen(true)
  }

  // Handle Creem payment confirmation
  const handleCreemConfirm = async () => {
    if (!selectedCreemProduct) return

    const success = await processCreemPayment(selectedCreemProduct.productId)
    if (success) {
      setCreemDialogOpen(false)
      setSelectedCreemProduct(null)
      await fetchUser()
    }
  }

  const handleWaffoMethodSelect = async (_method: unknown, index: number) => {
    const loadingKey = `waffo-${index}`
    setPaymentLoading(loadingKey)

    try {
      await processWaffoPayment(topupAmount, index)
    } finally {
      setPaymentLoading(null)
    }
  }

  // Get discount rate for current topup amount
  const getDiscountRate = useCallback(() => {
    return topupInfo?.discount?.[topupAmount] || DEFAULT_DISCOUNT_RATE
  }, [topupInfo, topupAmount])

  const handleSubscriptionRefresh = useCallback(async () => {
    setSubscriptionRefreshing(true)
    try {
      await fetchSelfSubscription()
    } finally {
      setSubscriptionRefreshing(false)
    }
  }, [fetchSelfSubscription])

  const handleBillingPreferenceChange = useCallback(
    async (pref: string) => {
      const previous = billingPreference
      setBillingPreference(pref)
      try {
        const res = await updateBillingPreference(pref)
        if (res.success) {
          toast.success(t('Updated successfully'))
          setBillingPreference(res.data?.billing_preference || pref)
        } else {
          toast.error(res.message || t('Update failed'))
          setBillingPreference(previous)
        }
      } catch {
        toast.error(t('Request failed'))
        setBillingPreference(previous)
      }
    },
    [billingPreference, t]
  )

  const handleCancelTossAutoRenew = useCallback(async () => {
    setCancellingAutoRenew(true)
    try {
      const res = await cancelTossAutoRenew()
      if (res.success) {
        toast.success(t('Auto-renew cancelled'))
        await fetchSelfSubscription()
        return true
      } else {
        toast.error(res.message || t('Request failed'))
        return false
      }
    } catch {
      toast.error(t('Request failed'))
      return false
    } finally {
      setCancellingAutoRenew(false)
    }
  }, [fetchSelfSubscription, t])

  const activePaymentSubscriptions = activeSubscriptions

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
      buildWalletPaymentSettingTabs({
        hasActiveSubscription: activePaymentSubscriptions.length > 0,
        subscriptionStatusKnown,
        visibleAutoRechargeModes,
        policies: walletAutoRecharge.policies,
      }),
    [
      activePaymentSubscriptions.length,
      subscriptionStatusKnown,
      visibleAutoRechargeModes,
      walletAutoRecharge.policies,
    ]
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
        <SectionPageLayout.Title>{t('Wallet')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
            <WalletStatsCard user={user} loading={userLoading} />

            <SubscriptionPlansCard
              topupInfo={topupInfo}
              userQuota={user?.quota}
              showStatus={false}
              onPurchaseSuccess={async () => {
                await Promise.all([fetchUser(), fetchSelfSubscription()])
              }}
            />

            <div
              className={
                paymentSettingTabs.length > 0
                  ? 'grid gap-4 xl:grid-cols-[minmax(0,1.05fr)_minmax(360px,0.95fr)] xl:items-start'
                  : 'grid gap-4'
              }
            >
              <div id='wallet-add-funds' className='scroll-mt-4'>
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
                  redemptionCode={redemptionCode}
                  onRedemptionCodeChange={setRedemptionCode}
                  onRedeem={handleRedeem}
                  redeeming={redeeming}
                  topupLink={topupInfo?.topup_link}
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
                          {tab.kind === 'subscription'
                            ? t('Subscription')
                            : tab.kind === 'threshold'
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

                    <TabsContent value='subscription'>
                      <WalletSubscriptionStatusCard
                        activeSubscriptions={activePaymentSubscriptions}
                        allSubscriptions={allSubscriptions}
                        billingPreference={billingPreference}
                        refreshing={subscriptionRefreshing}
                        cancellingAutoRenew={cancellingAutoRenew}
                        onRefresh={handleSubscriptionRefresh}
                        onBillingPreferenceChange={
                          handleBillingPreferenceChange
                        }
                        onCancelTossAutoRenew={handleCancelTossAutoRenew}
                      />
                    </TabsContent>

                    <TabsContent value='threshold'>
                      <AutoRechargeCard
                        mode='threshold'
                        policies={walletAutoRecharge.policies}
                        presets={walletAutoRecharge.presets}
                        loading={walletAutoRecharge.loading}
                        processing={walletAutoRecharge.processing}
                        canManage
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
                        canManage
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

            <AffiliateRewardsCard
              user={user}
              affiliateLink={affiliateLink}
              onTransfer={() => setTransferDialogOpen(true)}
              complianceConfirmed={
                topupInfo?.payment_compliance_confirmed !== false
              }
              loading={affiliateLoading}
            />
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
        processing={processing || pancakeProcessing || tossProcessing}
        discountRate={getDiscountRate()}
        usdExchangeRate={effectiveUsdExchangeRate}
      />

      <TransferDialog
        open={transferDialogOpen}
        onOpenChange={setTransferDialogOpen}
        onConfirm={handleTransfer}
        availableQuota={user?.aff_quota ?? 0}
        transferring={transferring}
      />

      <BillingHistoryDialog
        open={billingDialogOpen}
        onOpenChange={setBillingDialogOpen}
      />

      <CreemConfirmDialog
        open={creemDialogOpen}
        onOpenChange={setCreemDialogOpen}
        onConfirm={handleCreemConfirm}
        product={selectedCreemProduct}
        processing={creemProcessing}
      />
    </>
  )
}
