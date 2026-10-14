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
import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'

import { OrganizationMemberWalletSummary } from '@/features/organizations/components/organization-member-wallet-summary' // fork: O3
import { OrganizationWallet } from '@/features/organizations/components/organization-wallet' // fork: O3
import { pickWalletView } from '@/features/organizations/lib/wallet-view' // fork: O3
import { Wallet } from '@/features/wallet'
import { useAuthStore } from '@/stores/auth-store' // fork: O3

const walletSearchSchema = z.object({
  show_history: z.boolean().optional(),
})

export const Route = createFileRoute('/_authenticated/wallet/')({
  component: RouteComponent,
  validateSearch: walletSearchSchema,
})

function RouteComponent() {
  const { show_history } = Route.useSearch()
  // fork: O3 — organization owner/member wallets (organizations/lib/wallet-view.ts)
  const view = pickWalletView(useAuthStore((s) => s.auth.user))
  if (view === 'organization_owner') {
    return <OrganizationWallet initialShowHistory={show_history} />
  }
  if (view === 'organization_member') {
    return <OrganizationMemberWalletSummary />
  }
  // fork: O3 end
  return <Wallet initialShowHistory={show_history} />
}
