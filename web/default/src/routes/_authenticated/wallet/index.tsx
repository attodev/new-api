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
import { useEffect } from 'react'
import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { hasOrganizationOwnerRole } from '@/lib/organization-roles'
import { OrganizationMemberWalletSummary } from '@/features/organizations/components/organization-member-wallet-summary'
import { OrganizationWallet } from '@/features/organizations/components/organization-wallet'
import { Wallet } from '@/features/wallet'

const walletSearchSchema = z.object({
  show_history: z.boolean().optional(),
  toss_error_code: z.string().optional(),
  toss_error_message: z.string().optional(),
  toss_order_id: z.string().optional(),
  wallet_auto_recharge: z.enum(['success', 'failed']).optional(),
})

export const Route = createFileRoute('/_authenticated/wallet/')({
  component: RouteComponent,
  validateSearch: walletSearchSchema,
})

function RouteComponent() {
  const {
    show_history,
    toss_error_code,
    toss_error_message,
    wallet_auto_recharge,
  } = Route.useSearch()
  const { t } = useTranslation()
  const user = useAuthStore((s) => s.auth.user)
  const organizationId = Number(user?.organization_id ?? 0)

  useEffect(() => {
    const detail = (toss_error_message || toss_error_code || '').trim()
    let handled = false
    if (detail) {
      if (
        toss_error_code === 'PAY_PROCESS_CANCELED' ||
        toss_error_code === 'USER_CANCEL'
      ) {
        toast.info(t('Cancelled'))
      } else {
        toast.error(`${t('Payment request failed')}: ${detail}`)
      }
      handled = true
    }
    if (wallet_auto_recharge === 'success') {
      toast.success(t('Setting updated successfully'))
      handled = true
    } else if (wallet_auto_recharge === 'failed' && !detail) {
      toast.error(t('Payment request failed'))
      handled = true
    }
    if (!handled) return
    window.history.replaceState({}, '', window.location.pathname)
  }, [t, toss_error_code, toss_error_message, wallet_auto_recharge])

  if (organizationId > 0) {
    if (hasOrganizationOwnerRole(user?.organization_role)) {
      return <OrganizationWallet initialShowHistory={show_history} />
    }
    return <OrganizationMemberWalletSummary />
  }

  return <Wallet initialShowHistory={show_history} />
}
