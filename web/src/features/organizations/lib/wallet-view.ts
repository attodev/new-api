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
import { hasOrganizationOwnerRole } from '@/lib/organization-roles'

export type WalletView =
  | 'organization_owner'
  | 'organization_member'
  | 'personal'

type WalletViewUser = {
  organization_id?: number
  organization_role?: string
} | null

/**
 * The /wallet route shows the organization wallet to the organization owner,
 * a read-only summary to the other organization members, and the personal
 * wallet to everyone else.
 */
export function pickWalletView(user: WalletViewUser | undefined): WalletView {
  if (Number(user?.organization_id ?? 0) <= 0) return 'personal'
  if (hasOrganizationOwnerRole(user?.organization_role)) {
    return 'organization_owner'
  }
  return 'organization_member'
}
