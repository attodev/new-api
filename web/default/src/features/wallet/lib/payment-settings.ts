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
