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

import { pickWalletView } from '../wallet-view'

describe('pickWalletView', () => {
  test('sends the organization owner to the organization wallet', () => {
    expect(
      pickWalletView({ organization_id: 7, organization_role: 'owner' })
    ).toBe('organization_owner')
  })

  test('sends other organization members to the member summary', () => {
    expect(
      pickWalletView({ organization_id: 7, organization_role: 'admin' })
    ).toBe('organization_member')
    expect(
      pickWalletView({ organization_id: 7, organization_role: 'member' })
    ).toBe('organization_member')
  })

  test('keeps users without an organization on the personal wallet', () => {
    expect(pickWalletView(null)).toBe('personal')
    expect(pickWalletView(undefined)).toBe('personal')
    expect(pickWalletView({})).toBe('personal')
    expect(
      pickWalletView({ organization_id: 0, organization_role: 'owner' })
    ).toBe('personal')
  })
})
