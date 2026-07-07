# Wallet Payment Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rework the personal wallet payment area so normal top-up stays on the left and subscription, auto recharge, and scheduled recharge are grouped on the right with FE-only mutual exclusion.

**Architecture:** Keep backend APIs unchanged. Add small frontend helper functions for payment-setting visibility/locking, extend the existing auto recharge card with a creation lock, add a subscription-status-only card, then compose them in `Wallet`.

**Tech Stack:** React 19, TypeScript, Base UI/shadcn-style local components, Tailwind CSS, i18next, Bun test runner.

## Global Constraints

- Work in `/tmp/new-api-wallet-auto-recharge` on branch `codex/wallet-auto-recharge`.
- Target the personal wallet screen first: `web/default/src/features/wallet/index.tsx`.
- Do not change backend APIs or database models.
- Hide wallet-page subscription UI when there is no active subscription.
- Hide subscription purchase plans from the wallet page; purchase remains in the existing subscription area.
- Keep existing protected project identifiers unchanged.
- Use `bun` for frontend commands from `web/default/`.
- Any new user-facing text must use `t('English source key')` and be added through the existing i18n sync flow.

---

## File Structure

- Create `web/default/src/features/wallet/lib/payment-settings.ts`
  - Pure helper for visible payment-setting tabs, active setting detection, selected tab fallback, and disabled states.
- Create `web/default/src/features/wallet/lib/payment-settings.test.ts`
  - Unit tests for the helper.
- Modify `web/default/src/features/wallet/components/auto-recharge-card.tsx`
  - Add `creationDisabled` and `creationDisabledMessageKey` props.
  - Disable preset selection buttons when another payment setting is active.
- Modify `web/default/src/features/wallet/components/auto-recharge-card.test.ts`
  - Add server-render tests for the disabled creation state.
- Create `web/default/src/features/wallet/components/wallet-subscription-status-card.tsx`
  - Status-only card for active subscriptions, billing preference, refresh, and cancel auto-renew actions.
- Create `web/default/src/features/wallet/components/wallet-subscription-status-card.test.ts`
  - Server-render tests proving it renders active subscriptions and no purchase controls.
- Modify `web/default/src/features/wallet/index.tsx`
  - Remove top-level wallet tabs.
  - Build 2-column layout.
  - Fetch subscription state needed by the right-side payment settings card.
  - Compose `RechargeFormCard`, `WalletSubscriptionStatusCard`, and `AutoRechargeCard`.
- Modify `web/default/src/i18n/locales/*.json`
  - Add `Payment settings`, `Choose one active payment setting at a time`, and `Cancel the current payment setting before choosing another one.` via `bun run i18n:sync`.

---

### Task 1: Payment Setting Helper

**Files:**

- Create: `web/default/src/features/wallet/lib/payment-settings.ts`
- Create: `web/default/src/features/wallet/lib/payment-settings.test.ts`

**Interfaces:**

- Consumes:
  - `WalletAutoRechargePolicy` from `web/default/src/features/wallet/types.ts`
  - `WalletAutoRechargeType` from `web/default/src/features/wallet/types.ts`
- Produces:
  - `WalletPaymentSettingKind = 'subscription' | 'threshold' | 'scheduled'`
  - `getConfiguredAutoRechargeTypes(policies): WalletAutoRechargeType[]`
  - `buildWalletPaymentSettingTabs(input): WalletPaymentSettingTab[]`
  - `getInitialWalletPaymentSetting(tabs): WalletPaymentSettingKind | null`

- [ ] **Step 1: Write the failing helper tests**

Create `web/default/src/features/wallet/lib/payment-settings.test.ts`:

```ts
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildWalletPaymentSettingTabs,
  getConfiguredAutoRechargeTypes,
  getInitialWalletPaymentSetting,
} from './payment-settings'

describe('wallet payment setting helpers', () => {
  test('hides subscription when there is no active subscription', () => {
    const tabs = buildWalletPaymentSettingTabs({
      hasActiveSubscription: false,
      visibleAutoRechargeModes: [],
      policies: [],
    })

    assert.deepEqual(tabs, [])
    assert.equal(getInitialWalletPaymentSetting(tabs), null)
  })

  test('shows subscription and disables unconfigured recharge choices when subscription is active', () => {
    const tabs = buildWalletPaymentSettingTabs({
      hasActiveSubscription: true,
      visibleAutoRechargeModes: ['threshold', 'scheduled'],
      policies: [],
    })

    assert.deepEqual(
      tabs.map((tab) => [tab.kind, tab.disabled]),
      [
        ['subscription', false],
        ['threshold', true],
        ['scheduled', true],
      ]
    )
    assert.equal(getInitialWalletPaymentSetting(tabs), 'subscription')
  })

  test('keeps configured auto recharge enabled and disables scheduled recharge', () => {
    const tabs = buildWalletPaymentSettingTabs({
      hasActiveSubscription: false,
      visibleAutoRechargeModes: ['threshold', 'scheduled'],
      policies: [{ id: 1, type: 'threshold', status: 'active' } as never],
    })

    assert.deepEqual(
      tabs.map((tab) => [tab.kind, tab.disabled]),
      [
        ['threshold', false],
        ['scheduled', true],
      ]
    )
    assert.equal(getInitialWalletPaymentSetting(tabs), 'threshold')
  })

  test('keeps every already configured exception tab enabled so users can cancel', () => {
    const tabs = buildWalletPaymentSettingTabs({
      hasActiveSubscription: true,
      visibleAutoRechargeModes: ['threshold', 'scheduled'],
      policies: [
        { id: 1, type: 'threshold', status: 'pending' },
        { id: 2, type: 'scheduled', status: 'active' },
      ] as never,
    })

    assert.deepEqual(
      tabs.map((tab) => [tab.kind, tab.disabled]),
      [
        ['subscription', false],
        ['threshold', false],
        ['scheduled', false],
      ]
    )
  })

  test('ignores cancelled and failed auto recharge policies', () => {
    assert.deepEqual(
      getConfiguredAutoRechargeTypes([
        { id: 1, type: 'threshold', status: 'cancelled' },
        { id: 2, type: 'scheduled', status: 'failed' },
      ] as never),
      []
    )
  })
})
```

- [ ] **Step 2: Run the helper tests and verify RED**

Run:

```bash
cd web/default
bun test src/features/wallet/lib/payment-settings.test.ts
```

Expected: FAIL because `./payment-settings` does not exist.

- [ ] **Step 3: Implement the helper**

Create `web/default/src/features/wallet/lib/payment-settings.ts`:

```ts
import type {
  WalletAutoRechargePolicy,
  WalletAutoRechargeType,
} from '../types'

export type WalletPaymentSettingKind =
  | 'subscription'
  | WalletAutoRechargeType

export interface WalletPaymentSettingTab {
  kind: WalletPaymentSettingKind
  disabled: boolean
  configured: boolean
  disabledMessageKey?: string
}

export interface WalletPaymentSettingInput {
  hasActiveSubscription: boolean
  visibleAutoRechargeModes: WalletAutoRechargeType[]
  policies: Array<Pick<WalletAutoRechargePolicy, 'type' | 'status'>>
}

const ACTIVE_POLICY_STATUSES = new Set(['active', 'pending'])
const PAYMENT_SETTING_ORDER: WalletPaymentSettingKind[] = [
  'subscription',
  'threshold',
  'scheduled',
]

export const WALLET_PAYMENT_SETTING_LOCK_MESSAGE =
  'Cancel the current payment setting before choosing another one.'

export function getConfiguredAutoRechargeTypes(
  policies: Array<Pick<WalletAutoRechargePolicy, 'type' | 'status'>>
): WalletAutoRechargeType[] {
  const configured = new Set<WalletAutoRechargeType>()
  for (const policy of policies) {
    if (ACTIVE_POLICY_STATUSES.has(policy.status)) {
      configured.add(policy.type)
    }
  }
  return PAYMENT_SETTING_ORDER.filter(
    (kind): kind is WalletAutoRechargeType =>
      kind !== 'subscription' && configured.has(kind)
  )
}

export function buildWalletPaymentSettingTabs(
  input: WalletPaymentSettingInput
): WalletPaymentSettingTab[] {
  const visible = new Set<WalletPaymentSettingKind>()
  if (input.hasActiveSubscription) visible.add('subscription')
  for (const mode of input.visibleAutoRechargeModes) visible.add(mode)

  const configured = new Set<WalletPaymentSettingKind>(
    getConfiguredAutoRechargeTypes(input.policies)
  )
  if (input.hasActiveSubscription) configured.add('subscription')

  const hasConfigured = configured.size > 0

  return PAYMENT_SETTING_ORDER.filter((kind) => visible.has(kind)).map(
    (kind) => {
      const isConfigured = configured.has(kind)
      const disabled = hasConfigured && !isConfigured
      return {
        kind,
        configured: isConfigured,
        disabled,
        disabledMessageKey: disabled
          ? WALLET_PAYMENT_SETTING_LOCK_MESSAGE
          : undefined,
      }
    }
  )
}

export function getInitialWalletPaymentSetting(
  tabs: WalletPaymentSettingTab[]
): WalletPaymentSettingKind | null {
  return (
    tabs.find((tab) => tab.configured && !tab.disabled)?.kind ??
    tabs.find((tab) => !tab.disabled)?.kind ??
    tabs[0]?.kind ??
    null
  )
}
```

- [ ] **Step 4: Run helper tests and verify GREEN**

Run:

```bash
cd web/default
bun test src/features/wallet/lib/payment-settings.test.ts
```

Expected: PASS, all tests in `payment-settings.test.ts` pass.

- [ ] **Step 5: Commit Task 1**

```bash
git add web/default/src/features/wallet/lib/payment-settings.ts web/default/src/features/wallet/lib/payment-settings.test.ts
git commit -m "feat(wallet): add payment setting helpers"
```

---

### Task 2: Auto Recharge Creation Lock

**Files:**

- Modify: `web/default/src/features/wallet/components/auto-recharge-card.tsx`
- Modify: `web/default/src/features/wallet/components/auto-recharge-card.test.ts`

**Interfaces:**

- Consumes:
  - `creationDisabled?: boolean`
  - `creationDisabledMessageKey?: string`
- Produces:
  - `AutoRechargeCard` disables preset selection buttons when `creationDisabled` is true and there is no current policy.

- [ ] **Step 1: Write the failing component tests**

Append to `describe('auto recharge card helpers', ...)` or the preset UI describe block in `web/default/src/features/wallet/components/auto-recharge-card.test.ts`:

```ts
test('disables scheduled preset creation when another payment setting is active', () => {
  const html = renderWithI18n(
    React.createElement(AutoRechargeCard, {
      mode: 'scheduled',
      policies: [],
      presets: [
        {
          id: 1,
          type: 'scheduled',
          target_scope: 'all',
          name: 'Monthly 10000',
          amount: 10000,
          interval_unit: 'month',
          interval_value: 1,
          enabled: true,
        },
      ],
      loading: false,
      processing: false,
      canManage: true,
      creationDisabled: true,
      creationDisabledMessageKey:
        'Cancel the current payment setting before choosing another one.',
      onCreateScheduled: async () => false,
      onCreateThreshold: async () => false,
      onCancel: async () => false,
    })
  )

  assert.match(
    html,
    /Cancel the current payment setting before choosing another one\./
  )
  assert.match(html, /disabled/)
})

test('does not block cancelling an existing policy when creation is locked', () => {
  const html = renderWithI18n(
    React.createElement(AutoRechargeCard, {
      mode: 'threshold',
      policies: [
        {
          id: 7,
          type: 'threshold',
          target_type: 'user',
          target_id: 1,
          status: 'active',
          amount: 10000,
          threshold_amount: 3000,
        },
      ],
      presets: [],
      loading: false,
      processing: false,
      canManage: true,
      creationDisabled: true,
      creationDisabledMessageKey:
        'Cancel the current payment setting before choosing another one.',
      onCreateScheduled: async () => false,
      onCreateThreshold: async () => false,
      onCancel: async () => false,
    })
  )

  assert.match(html, /Current policy/)
  assert.match(html, /Cancel/)
  assert.doesNotMatch(
    html,
    /Cancel the current payment setting before choosing another one\./
  )
})
```

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
cd web/default
bun test src/features/wallet/components/auto-recharge-card.test.ts
```

Expected: FAIL because `creationDisabled` props are not part of `AutoRechargeCardProps`.

- [ ] **Step 3: Extend `AutoRechargeCard` props and disabled logic**

In `AutoRechargeCardProps`, add:

```ts
  creationDisabled?: boolean
  creationDisabledMessageKey?: string
```

In the component parameter list, add defaults:

```ts
  creationDisabled = false,
  creationDisabledMessageKey = 'Cancel the current payment setting before choosing another one.',
```

Replace the existing disabled calculation:

```ts
  const disabled = loading || processing || !canManage
  const creationLocked = creationDisabled && !activePolicy
  const creationButtonDisabled = disabled || creationLocked
```

Add the lock message after the permission message block:

```tsx
        {creationLocked ? (
          <div className='text-muted-foreground text-sm'>
            {t(creationDisabledMessageKey)}
          </div>
        ) : null}
```

Use `creationButtonDisabled` on preset selection buttons, but keep cancel using `disabled`:

```tsx
disabled={creationButtonDisabled}
```

Apply this replacement to scheduled period buttons, scheduled amount buttons, threshold amount buttons, and threshold balance buttons.

- [ ] **Step 4: Run component tests and verify GREEN**

Run:

```bash
cd web/default
bun test src/features/wallet/components/auto-recharge-card.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit Task 2**

```bash
git add web/default/src/features/wallet/components/auto-recharge-card.tsx web/default/src/features/wallet/components/auto-recharge-card.test.ts
git commit -m "feat(wallet): lock auto recharge creation in UI"
```

---

### Task 3: Subscription Status-Only Card

**Files:**

- Create: `web/default/src/features/wallet/components/wallet-subscription-status-card.tsx`
- Create: `web/default/src/features/wallet/components/wallet-subscription-status-card.test.ts`

**Interfaces:**

- Consumes:
  - `UserSubscriptionRecord[]`
  - `billingPreference: string`
  - callbacks for refresh, billing preference update, and Toss auto-renew cancel
- Produces:
  - `WalletSubscriptionStatusCard`
  - A status-only subscription UI with no `Subscribe Now` or plan purchase grid.

- [ ] **Step 1: Write the failing render tests**

Create `web/default/src/features/wallet/components/wallet-subscription-status-card.test.ts`:

```ts
import React from 'react'
import i18next from 'i18next'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { before, describe, test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { WalletSubscriptionStatusCard } from './wallet-subscription-status-card'

const enMessages = (
  JSON.parse(
    readFileSync(
      new URL('../../../i18n/locales/en.json', import.meta.url),
      'utf8'
    )
  ) as { translation: Record<string, string> }
).translation

before(async () => {
  if (!i18next.isInitialized) {
    await i18next.use(initReactI18next).init({
      lng: 'en',
      fallbackLng: 'en',
      resources: { en: { translation: enMessages } },
      interpolation: { escapeValue: false },
    })
  }
})

function renderWithI18n(element: React.ReactElement) {
  return renderToStaticMarkup(
    React.createElement(I18nextProvider, { i18n: i18next }, element)
  )
}

describe('WalletSubscriptionStatusCard', () => {
  test('renders active subscription status without purchase controls', () => {
    const html = renderWithI18n(
      React.createElement(WalletSubscriptionStatusCard, {
        activeSubscriptions: [
          {
            subscription: {
              id: 11,
              plan_id: 3,
              status: 'active',
              amount_total: 1000,
              amount_used: 250,
              end_time: Math.floor(Date.now() / 1000) + 86400,
              auto_renew: true,
            },
            plan: { id: 3, title: 'Pro Plan' },
          },
        ] as never,
        allSubscriptions: [],
        billingPreference: 'subscription_first',
        refreshing: false,
        cancellingAutoRenew: false,
        onRefresh: async () => undefined,
        onBillingPreferenceChange: async () => undefined,
        onCancelTossAutoRenew: async () => undefined,
      })
    )

    assert.match(html, /My Subscriptions/)
    assert.match(html, /Pro Plan/)
    assert.match(html, /Auto-renew active/)
    assert.doesNotMatch(html, /Subscribe Now/)
    assert.doesNotMatch(html, /No plans available/)
  })

  test('renders nothing without active subscriptions', () => {
    const html = renderWithI18n(
      React.createElement(WalletSubscriptionStatusCard, {
        activeSubscriptions: [],
        allSubscriptions: [],
        billingPreference: 'wallet_first',
        refreshing: false,
        cancellingAutoRenew: false,
        onRefresh: async () => undefined,
        onBillingPreferenceChange: async () => undefined,
        onCancelTossAutoRenew: async () => undefined,
      })
    )

    assert.equal(html, '')
  })
})
```

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
cd web/default
bun test src/features/wallet/components/wallet-subscription-status-card.test.ts
```

Expected: FAIL because `wallet-subscription-status-card.tsx` does not exist.

- [ ] **Step 3: Implement the status-only component**

Create `web/default/src/features/wallet/components/wallet-subscription-status-card.tsx` by extracting only the active subscription display behavior from `SubscriptionPlansCard`. Use this public interface:

```ts
interface WalletSubscriptionStatusCardProps {
  activeSubscriptions: UserSubscriptionRecord[]
  allSubscriptions: UserSubscriptionRecord[]
  billingPreference: string
  refreshing: boolean
  cancellingAutoRenew: boolean
  onRefresh: () => void | Promise<void>
  onBillingPreferenceChange: (preference: string) => void | Promise<void>
  onCancelTossAutoRenew: () => void | Promise<void>
}
```

Implementation requirements:

- Return `null` when `activeSubscriptions.length === 0`.
- Use `TitledCard` with title `t('Subscription')`.
- Render the `My Subscriptions` header and billing preference select.
- Render only active subscriptions from `activeSubscriptions`.
- Keep `Auto-renew active` and `Cancel Auto-renew` controls.
- Do not import `getPublicPlans`.
- Do not render plan purchase cards.
- Do not render `SubscriptionPurchaseDialog`.

Use these helper signatures inside the file:

```ts
function getBillingPreferenceLabel(
  preference: string,
  t: (key: string) => string
): string

function getRemainingDays(sub: UserSubscriptionRecord): number

function getUsagePercent(sub: UserSubscriptionRecord): number
```

- [ ] **Step 4: Run status card tests and verify GREEN**

Run:

```bash
cd web/default
bun test src/features/wallet/components/wallet-subscription-status-card.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit Task 3**

```bash
git add web/default/src/features/wallet/components/wallet-subscription-status-card.tsx web/default/src/features/wallet/components/wallet-subscription-status-card.test.ts
git commit -m "feat(wallet): add subscription status card"
```

---

### Task 4: Wallet Page Integration and Verification

**Files:**

- Modify: `web/default/src/features/wallet/index.tsx`
- Modify: `web/default/src/i18n/locales/*.json`
- Test: `web/default/src/features/wallet/lib/payment-settings.test.ts`
- Test: `web/default/src/features/wallet/components/auto-recharge-card.test.ts`
- Test: `web/default/src/features/wallet/components/wallet-subscription-status-card.test.ts`

**Interfaces:**

- Consumes:
  - `buildWalletPaymentSettingTabs`
  - `getInitialWalletPaymentSetting`
  - `WalletSubscriptionStatusCard`
  - `AutoRechargeCard.creationDisabled`
- Produces:
  - `Wallet` renders `RechargeFormCard` on the left and a right-side `Payment settings` card only when there is a visible setting tab.

- [ ] **Step 1: Add subscription state loading to `Wallet`**

In `web/default/src/features/wallet/index.tsx`, import:

```ts
import { toast } from 'sonner'
import {
  cancelTossAutoRenew,
  getSelfSubscriptionFull,
  updateBillingPreference,
} from '@/features/subscriptions/api'
import type { UserSubscriptionRecord } from '@/features/subscriptions/types'
import {
  buildWalletPaymentSettingTabs,
  getInitialWalletPaymentSetting,
  WALLET_PAYMENT_SETTING_LOCK_MESSAGE,
  type WalletPaymentSettingKind,
} from './lib/payment-settings'
import { WalletSubscriptionStatusCard } from './components/wallet-subscription-status-card'
```

Add state:

```ts
  const [paymentSettingTab, setPaymentSettingTab] =
    useState<WalletPaymentSettingKind | null>(null)
  const [activeSubscriptions, setActiveSubscriptions] = useState<
    UserSubscriptionRecord[]
  >([])
  const [allSubscriptions, setAllSubscriptions] = useState<
    UserSubscriptionRecord[]
  >([])
  const [billingPreference, setBillingPreference] =
    useState('subscription_first')
  const [subscriptionLoading, setSubscriptionLoading] = useState(true)
  const [subscriptionRefreshing, setSubscriptionRefreshing] = useState(false)
  const [cancellingAutoRenew, setCancellingAutoRenew] = useState(false)
```

- [ ] **Step 2: Add subscription refresh handlers**

Add these callbacks in `Wallet`:

```ts
  const fetchSelfSubscription = useCallback(async () => {
    try {
      const res = await getSelfSubscriptionFull()
      if (res.success && res.data) {
        setBillingPreference(res.data.billing_preference || 'subscription_first')
        setActiveSubscriptions(res.data.subscriptions || [])
        setAllSubscriptions(res.data.all_subscriptions || [])
      } else {
        setActiveSubscriptions([])
        setAllSubscriptions([])
      }
    } catch {
      setActiveSubscriptions([])
      setAllSubscriptions([])
    }
  }, [])

  useEffect(() => {
    const init = async () => {
      setSubscriptionLoading(true)
      await fetchSelfSubscription()
      setSubscriptionLoading(false)
    }
    void init()
  }, [fetchSelfSubscription])

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
      } else {
        toast.error(res.message || t('Request failed'))
      }
    } catch {
      toast.error(t('Request failed'))
    } finally {
      setCancellingAutoRenew(false)
    }
  }, [fetchSelfSubscription, t])
```

- [ ] **Step 3: Build right-side tabs and selection state**

Replace the old `visibleAutoRechargeModes`, `activeTab`, `TabsList`, and `TabsContent` top-level tab usage with:

```ts
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

  const paymentSettingTabs = useMemo(
    () =>
      buildWalletPaymentSettingTabs({
        hasActiveSubscription: activeSubscriptions.length > 0,
        visibleAutoRechargeModes,
        policies: walletAutoRecharge.policies,
      }),
    [
      activeSubscriptions.length,
      visibleAutoRechargeModes,
      walletAutoRecharge.policies,
    ]
  )

  useEffect(() => {
    if (
      paymentSettingTab &&
      paymentSettingTabs.some((tab) => tab.kind === paymentSettingTab)
    ) {
      return
    }
    setPaymentSettingTab(getInitialWalletPaymentSetting(paymentSettingTabs))
  }, [paymentSettingTab, paymentSettingTabs])
```

- [ ] **Step 4: Replace the JSX layout**

In the main content after `WalletStatsCard`, use this structure:

```tsx
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
                    <TabsList className='grid w-full grid-cols-3'>
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

                    <TabsContent value='subscription'>
                      <WalletSubscriptionStatusCard
                        activeSubscriptions={activeSubscriptions}
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
                          paymentSettingTabs.find(
                            (tab) => tab.kind === 'threshold'
                          )?.disabled ?? false
                        }
                        creationDisabledMessageKey={
                          WALLET_PAYMENT_SETTING_LOCK_MESSAGE
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
                          paymentSettingTabs.find(
                            (tab) => tab.kind === 'scheduled'
                          )?.disabled ?? false
                        }
                        creationDisabledMessageKey={
                          WALLET_PAYMENT_SETTING_LOCK_MESSAGE
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
```

Also:

- Remove `showSubscriptionPanel`, `handleSubscriptionAvailabilityChange`, and `SubscriptionPlansCard` usage from `Wallet`.
- Import `TitledCard` if not already imported.
- Keep `AffiliateRewardsCard` below this grid.

- [ ] **Step 5: Add i18n keys**

Run:

```bash
cd web/default
bun run i18n:sync
```

Expected:

- New English-source keys exist in all locale JSON files.
- At minimum:
  - `Payment settings`
  - `Choose one active payment setting at a time`
  - `Cancel the current payment setting before choosing another one.`

- [ ] **Step 6: Run focused frontend tests**

Run:

```bash
cd web/default
bun test src/features/wallet/lib/payment-settings.test.ts src/features/wallet/components/auto-recharge-card.test.ts src/features/wallet/components/wallet-subscription-status-card.test.ts
```

Expected: PASS.

- [ ] **Step 7: Run type/build verification**

Run:

```bash
cd web/default
bun run build:check
```

Expected: PASS with successful TypeScript and Rsbuild build.

- [ ] **Step 8: Commit Task 4**

```bash
git add web/default/src/features/wallet/index.tsx web/default/src/features/wallet/lib/payment-settings.test.ts web/default/src/features/wallet/components/auto-recharge-card.test.ts web/default/src/features/wallet/components/wallet-subscription-status-card.test.ts web/default/src/i18n/locales
git commit -m "feat(wallet): unify payment setting layout"
```

---

## Final Verification

- [ ] Run the full focused wallet test set:

```bash
cd web/default
bun test src/features/wallet
```

Expected: PASS.

- [ ] Run build verification:

```bash
cd web/default
bun run build:check
```

Expected: PASS.

- [ ] Manually inspect personal wallet in desktop and mobile widths:
  - No active subscription, auto recharge presets visible
  - Active subscription visible
  - Active auto recharge visible
  - Active scheduled recharge visible
  - No payment settings visible

- [ ] Commit any final fixes:

```bash
git status --short
git add web/default/src/features/wallet/index.tsx web/default/src/features/wallet/lib/payment-settings.ts web/default/src/features/wallet/lib/payment-settings.test.ts web/default/src/features/wallet/components/auto-recharge-card.tsx web/default/src/features/wallet/components/auto-recharge-card.test.ts web/default/src/features/wallet/components/wallet-subscription-status-card.tsx web/default/src/features/wallet/components/wallet-subscription-status-card.test.ts web/default/src/i18n/locales
git commit -m "fix(wallet): polish payment setting layout"
```
