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
import { describe, expect, test } from 'vitest'

import type { OrganizationUserSubscriptionRecord } from '../../types'
import {
  getAssignableOrganizationSubscriptionUsers,
  getActiveOrganizationSubscriptionUserIds,
  getOrganizationQuotaControlState,
  getVisibleActiveOrganizationSubscriptionRecords,
  hasActiveOrganizationSubscription,
  shouldDisableQuotaForOrganizationSubscription,
} from '../organization-subscription-utils'

function subscriptionRecord(
  userId: number,
  status: string
): OrganizationUserSubscriptionRecord {
  return {
    subscription: {
      id: userId,
      organization_id: 1,
      user_id: userId,
      plan_id: 1,
      amount_total: 100,
      amount_used: 0,
      start_time: 1,
      end_time: 2,
      status,
      last_reset_time: 1,
      next_reset_time: 2,
      upgrade_group: '',
      prev_user_group: '',
      assigned_by_user_id: 1,
      created_at: 1,
      updated_at: 1,
    },
  }
}

describe('organization subscription utils', () => {
  test('detects only active organization plan assignments', () => {
    const records = [
      subscriptionRecord(11, 'active'),
      subscriptionRecord(12, 'cancelled'),
      subscriptionRecord(13, 'expired'),
    ]

    expect(getActiveOrganizationSubscriptionUserIds(records)).toEqual(
      new Set([11])
    )
    expect(hasActiveOrganizationSubscription(11, records)).toBe(true)
    expect(hasActiveOrganizationSubscription(12, records)).toBe(false)
    expect(hasActiveOrganizationSubscription(13, records)).toBe(false)
  })

  test('filters active plan users out of assignable plan users', () => {
    const records = [
      subscriptionRecord(11, 'active'),
      subscriptionRecord(12, 'cancelled'),
    ]
    const users = [
      { id: 11, username: 'active-user' },
      { id: 12, username: 'cancelled-user' },
      { id: 13, username: 'plain-user' },
    ]

    expect(
      getAssignableOrganizationSubscriptionUsers(users, records).map(
        (user) => user.id
      )
    ).toEqual([12, 13])
  })

  test('marks quota controls disabled only for active organization plan users', () => {
    const records = [
      subscriptionRecord(11, 'active'),
      subscriptionRecord(12, 'cancelled'),
    ]

    expect(shouldDisableQuotaForOrganizationSubscription(11, records)).toBe(
      true
    )
    expect(shouldDisableQuotaForOrganizationSubscription(12, records)).toBe(
      false
    )
    expect(shouldDisableQuotaForOrganizationSubscription(13, records)).toBe(
      false
    )
  })

  test('shows only active subscriptions in the assignment table after cancellation', () => {
    const records = [
      subscriptionRecord(11, 'active'),
      subscriptionRecord(12, 'cancelled'),
      subscriptionRecord(13, 'expired'),
    ]

    expect(
      getVisibleActiveOrganizationSubscriptionRecords(records).map(
        (record) => record.subscription.user_id
      )
    ).toEqual([11])
  })

  test('returns cancelled plan users to assignable users after cancellation', () => {
    const records = [
      subscriptionRecord(11, 'active'),
      subscriptionRecord(12, 'cancelled'),
    ]
    const users = [
      { id: 11, username: 'active-user' },
      { id: 12, username: 'cancelled-user' },
      { id: 13, username: 'plain-user' },
    ]

    expect(
      getAssignableOrganizationSubscriptionUsers(users, records).map(
        (user) => user.username
      )
    ).toEqual(['cancelled-user', 'plain-user'])
  })

  test('derives quota input disabled state and display balance for active plan users', () => {
    const records = [
      subscriptionRecord(11, 'active'),
      subscriptionRecord(12, 'cancelled'),
    ]

    expect(getOrganizationQuotaControlState(11, 500, records)).toEqual({
      disabled: true,
      displayQuota: 0,
      hasActivePlan: true,
    })
    expect(getOrganizationQuotaControlState(12, 500, records)).toEqual({
      disabled: false,
      displayQuota: 500,
      hasActivePlan: false,
    })
  })
})
