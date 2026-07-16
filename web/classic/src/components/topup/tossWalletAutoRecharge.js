/*
Copyright (C) 2025 QuantumNous

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

import { isTossBillingAuthSession } from './tossSubscriptionCheckout';

const PRESET_FINGERPRINT_PATTERN = /^[a-f0-9]{64}$/;
const USER_VISIBLE_POLICY_STATUSES = new Set(['pending', 'active']);

// `cancel_pending` is a provider-cleanup state. The backend clears active_key
// before entering it so a replacement policy can be configured, and explicitly
// moves that row out of user-visible use while reconciliation finishes.
export function isUserVisibleWalletAutoRechargePolicy(policy) {
  return USER_VISIBLE_POLICY_STATUSES.has(policy?.status);
}

export function hasValidWalletAutoRechargeFingerprint(preset) {
  return (
    typeof preset?.terms_fingerprint === 'string' &&
    PRESET_FINGERPRINT_PATTERN.test(preset.terms_fingerprint)
  );
}

export function walletAutoRechargePresetTerms(preset) {
  return Object.freeze({
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
  });
}

function isFiniteNumber(value) {
  return typeof value === 'number' && Number.isFinite(value);
}

function isPresetTerms(value) {
  return (
    value &&
    typeof value === 'object' &&
    Number.isSafeInteger(value.preset_id) &&
    value.preset_id > 0 &&
    (value.type === 'scheduled' || value.type === 'threshold') &&
    (value.target_scope === 'user' ||
      value.target_scope === 'organization' ||
      value.target_scope === 'all') &&
    isFiniteNumber(value.amount) &&
    value.amount > 0 &&
    isFiniteNumber(value.threshold_amount) &&
    Number.isSafeInteger(value.threshold_quota) &&
    (value.interval_unit === '' ||
      value.interval_unit === 'month' ||
      value.interval_unit === 'day' ||
      value.interval_unit === 'custom') &&
    Number.isSafeInteger(value.interval_value) &&
    Number.isSafeInteger(value.custom_seconds) &&
    typeof value.charge_immediately === 'boolean' &&
    typeof value.enabled === 'boolean'
  );
}

export function walletAutoRechargeSessionMatchesPreset(session, preset) {
  if (
    !isTossBillingAuthSession(session, 'wallet') ||
    !hasValidWalletAutoRechargeFingerprint(preset) ||
    session.preset_fingerprint !== preset.terms_fingerprint ||
    !isPresetTerms(session.policy)
  ) {
    return false;
  }

  const expected = walletAutoRechargePresetTerms(preset);
  return Object.keys(expected).every(
    (key) => session.policy[key] === expected[key],
  );
}
