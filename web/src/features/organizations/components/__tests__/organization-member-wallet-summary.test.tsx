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

import { getOrganizationMemberWalletBalanceDisplay } from '../../lib/member-wallet-balance'
import type { OrganizationUserSubscriptionRecord } from '../../types'

function translate(key: string) {
  return key
}

function record(status: string): OrganizationUserSubscriptionRecord {
  return {
    subscription: {
      id: 1,
      organization_id: 1,
      user_id: 2,
      plan_id: 3,
      amount_total: 100,
      amount_used: 20,
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

describe('organization member wallet balance display', () => {
  test('replaces current balance copy for active organization plan users', () => {
    expect(
      getOrganizationMemberWalletBalanceDisplay(record('active'), translate)
    ).toEqual({
      value: 'Organization plan in use',
      description: 'Balance is managed by the assigned organization plan',
    })
  })

  test('keeps regular balance display when there is no active organization plan', () => {
    expect(
      getOrganizationMemberWalletBalanceDisplay(record('cancelled'), translate)
    ).toBeUndefined()
    expect(
      getOrganizationMemberWalletBalanceDisplay(null, translate)
    ).toBeUndefined()
  })
})
