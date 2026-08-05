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
import type {
  WalletAutoRechargeBillingAuthSession,
  WalletAutoRechargePreset,
  WalletAutoRechargePresetTerms,
  WalletAutoRechargeRequest,
} from '../types'
import {
  getValidTossBillingAuthTradeNo,
  isValidTossBillingAuthSession,
} from './payment'

const PRESET_FINGERPRINT_PATTERN = /^[a-f0-9]{64}$/

export function hasValidWalletAutoRechargePresetFingerprint(
  preset: Pick<WalletAutoRechargePreset, 'terms_fingerprint'>
): boolean {
  return (
    typeof preset.terms_fingerprint === 'string' &&
    PRESET_FINGERPRINT_PATTERN.test(preset.terms_fingerprint)
  )
}

export function getWalletAutoRechargePresetTerms(
  preset: WalletAutoRechargePreset
): WalletAutoRechargePresetTerms {
  return {
    preset_id: preset.id,
    type: preset.type,
    target_scope: preset.target_scope,
    amount: preset.amount,
    threshold_amount: preset.threshold_amount ?? 0,
    threshold_quota: preset.threshold_quota ?? 0,
    interval_unit: preset.interval_unit ?? '',
    interval_value: preset.interval_value ?? 0,
    custom_seconds: preset.custom_seconds ?? 0,
    charge_immediately: preset.charge_immediately ?? false,
    enabled: preset.enabled,
  }
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function isWalletAutoRechargePresetTerms(
  value: unknown
): value is WalletAutoRechargePresetTerms {
  if (!value || typeof value !== 'object') return false
  const terms = value as Record<string, unknown>
  return (
    Number.isSafeInteger(terms.preset_id) &&
    (terms.preset_id as number) > 0 &&
    (terms.type === 'scheduled' || terms.type === 'threshold') &&
    (terms.target_scope === 'user' ||
      terms.target_scope === 'organization' ||
      terms.target_scope === 'all') &&
    isFiniteNumber(terms.amount) &&
    (terms.amount as number) > 0 &&
    isFiniteNumber(terms.threshold_amount) &&
    Number.isSafeInteger(terms.threshold_quota) &&
    (terms.interval_unit === '' ||
      terms.interval_unit === 'month' ||
      terms.interval_unit === 'day' ||
      terms.interval_unit === 'custom') &&
    Number.isSafeInteger(terms.interval_value) &&
    Number.isSafeInteger(terms.custom_seconds) &&
    typeof terms.charge_immediately === 'boolean' &&
    typeof terms.enabled === 'boolean'
  )
}

export function isWalletAutoRechargeBillingSessionMatchingRequest(
  value: unknown,
  request: WalletAutoRechargeRequest
): value is WalletAutoRechargeBillingAuthSession {
  if (!isValidTossBillingAuthSession(value, 'wallet')) return false
  const session = value as WalletAutoRechargeBillingAuthSession
  if (
    !PRESET_FINGERPRINT_PATTERN.test(session.preset_fingerprint) ||
    session.preset_fingerprint !== request.preset_fingerprint ||
    !isWalletAutoRechargePresetTerms(session.policy)
  ) {
    return false
  }

  const expected = request.expected_policy
  const actual = session.policy
  return (
    actual.preset_id === expected.preset_id &&
    actual.type === expected.type &&
    actual.target_scope === expected.target_scope &&
    actual.amount === expected.amount &&
    actual.threshold_amount === expected.threshold_amount &&
    actual.threshold_quota === expected.threshold_quota &&
    actual.interval_unit === expected.interval_unit &&
    actual.interval_value === expected.interval_value &&
    actual.custom_seconds === expected.custom_seconds &&
    actual.charge_immediately === expected.charge_immediately &&
    actual.enabled === expected.enabled
  )
}

export async function cancelRecoverableWalletAutoRechargeBillingSession(
  value: unknown,
  scope: 'user' | 'organization',
  cancel: (tradeNo: string, scope: 'user' | 'organization') => Promise<unknown>
): Promise<boolean> {
  const tradeNo = getValidTossBillingAuthTradeNo(value)
  if (!tradeNo) return false
  await cancel(tradeNo, scope)
  return true
}

export { getValidTossBillingAuthTradeNo }
