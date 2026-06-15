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

type UserLike = {
  id: number
}

export function getActiveOrganizationSubscriptionUserIds(
  records: OrganizationUserSubscriptionRecord[]
): Set<number> {
  return new Set(
    records
      .filter((record) => record.subscription.status === 'active')
      .map((record) => record.subscription.user_id)
  )
}

export function hasActiveOrganizationSubscription(
  userId: number,
  records: OrganizationUserSubscriptionRecord[]
): boolean {
  return getActiveOrganizationSubscriptionUserIds(records).has(userId)
}

export function shouldDisableQuotaForOrganizationSubscription(
  userId: number,
  records: OrganizationUserSubscriptionRecord[]
): boolean {
  return hasActiveOrganizationSubscription(userId, records)
}

export function getOrganizationQuotaControlState(
  userId: number,
  quota: number,
  records: OrganizationUserSubscriptionRecord[]
): {
  disabled: boolean
  displayQuota: number
  hasActivePlan: boolean
} {
  const hasActivePlan = hasActiveOrganizationSubscription(userId, records)
  return {
    disabled: hasActivePlan,
    displayQuota: hasActivePlan ? 0 : quota,
    hasActivePlan,
  }
}

export function getAssignableOrganizationSubscriptionUsers<T extends UserLike>(
  users: T[],
  records: OrganizationUserSubscriptionRecord[]
): T[] {
  const activeUserIds = getActiveOrganizationSubscriptionUserIds(records)
  return users.filter((user) => !activeUserIds.has(user.id))
}

export function getVisibleActiveOrganizationSubscriptionRecords(
  records: OrganizationUserSubscriptionRecord[]
): OrganizationUserSubscriptionRecord[] {
  return records.filter((record) => record.subscription.status === 'active')
}
