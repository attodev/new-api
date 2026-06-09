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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { OrganizationUserSubscriptionRecord } from '../types'
import {
  getActiveOrganizationSubscriptionUserIds,
  hasActiveOrganizationSubscription,
} from './organization-subscription-utils'

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

    assert.deepEqual(
      getActiveOrganizationSubscriptionUserIds(records),
      new Set([11])
    )
    assert.equal(hasActiveOrganizationSubscription(11, records), true)
    assert.equal(hasActiveOrganizationSubscription(12, records), false)
    assert.equal(hasActiveOrganizationSubscription(13, records), false)
  })
})
