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
import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { hasOrganizationOwnerRole } from '@/lib/organization-roles'
import { OrganizationMemberWalletSummary } from '@/features/organizations/components/organization-member-wallet-summary'
import { OrganizationWallet } from '@/features/organizations/components/organization-wallet'
import { Wallet } from '@/features/wallet'

const walletSearchSchema = z.object({
  show_history: z.boolean().optional(),
})

export const Route = createFileRoute('/_authenticated/wallet/')({
  component: RouteComponent,
  validateSearch: walletSearchSchema,
})

function RouteComponent() {
  const { show_history } = Route.useSearch()
  const user = useAuthStore((s) => s.auth.user)
  const organizationId = Number(user?.organization_id ?? 0)

  if (organizationId > 0) {
    if (hasOrganizationOwnerRole(user?.organization_role)) {
      return <OrganizationWallet />
    }
    return <OrganizationMemberWalletSummary />
  }

  return <Wallet initialShowHistory={show_history} />
}
