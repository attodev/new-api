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
export interface TossOptionUpdate {
  key: string
  value: string | number | boolean
}

export const TOSS_ATOMIC_OPTION_KEY_ORDER = [
  'TossEnabled',
  'TossBillingEnabled',
  'TossWalletAutoRechargeEnabled',
  'TossTestMode',
  'TossClientKey',
  'TossSecretKey',
  'TossTestClientKey',
  'TossTestSecretKey',
  'TossBillingClientKey',
  'TossBillingSecretKey',
  'TossBillingTestClientKey',
  'TossBillingTestSecretKey',
  'TossUnitPrice',
  'TossMinTopUp',
] as const

export const TOSS_ATOMIC_OPTION_KEYS = new Set<string>(
  TOSS_ATOMIC_OPTION_KEY_ORDER
)

export function buildCompleteTossRepairUpdates(
  values: Record<
    (typeof TOSS_ATOMIC_OPTION_KEY_ORDER)[number],
    string | number | boolean
  >
): TossOptionUpdate[] {
  return TOSS_ATOMIC_OPTION_KEY_ORDER.map((key) => ({
    key,
    value: values[key],
  }))
}

const TOSS_CREDENTIAL_OPTION_PAIRS = [
  ['TossClientKey', 'TossSecretKey'],
  ['TossTestClientKey', 'TossTestSecretKey'],
  ['TossBillingClientKey', 'TossBillingSecretKey'],
  ['TossBillingTestClientKey', 'TossBillingTestSecretKey'],
] as const

type TossCredentialOptionKey =
  (typeof TOSS_CREDENTIAL_OPTION_PAIRS)[number][number]

/**
 * A credential rotation must send the client and secret observed in the same
 * form snapshot. The server persists the pair in one transaction, preventing
 * an old secret-only browser request from being combined with a newer MID.
 */
export function completeTossCredentialOptionPairs(
  updates: TossOptionUpdate[],
  values: Record<TossCredentialOptionKey, string>
): TossOptionUpdate[] {
  const updatesByKey = new Map(updates.map((update) => [update.key, update]))

  for (const [clientKey, secretKey] of TOSS_CREDENTIAL_OPTION_PAIRS) {
    if (updatesByKey.has(clientKey) || updatesByKey.has(secretKey)) {
      updatesByKey.set(clientKey, {
        key: clientKey,
        value: values[clientKey],
      })
      updatesByKey.set(secretKey, {
        key: secretKey,
        value: values[secretKey],
      })
    }
  }

  return [...updatesByKey.values()]
}
