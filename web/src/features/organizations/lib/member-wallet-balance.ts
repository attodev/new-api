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
import type { OrganizationUserSubscriptionRecord } from '../types'

type OrganizationWalletTranslate = (key: string) => string

/**
 * Members on an active organization plan spend the plan limit, not their own
 * balance, so the wallet's balance figure is replaced with a notice.
 */
export function getOrganizationMemberWalletBalanceDisplay(
  subscription: OrganizationUserSubscriptionRecord | null,
  t: OrganizationWalletTranslate
) {
  if (subscription?.subscription?.status !== 'active') return undefined

  return {
    value: t('Organization plan in use'),
    description: t('Balance is managed by the assigned organization plan'),
  }
}
