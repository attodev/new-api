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
import { OrganizationWallet } from '@/features/organizations/components/organization-wallet'

const organizationWalletSearchSchema = z.object({
  wallet_auto_recharge: z.enum(['success', 'failed']).optional(),
})

export const Route = createFileRoute('/_authenticated/organization/wallet')({
  component: OrganizationWalletRoute,
  validateSearch: organizationWalletSearchSchema,
})

function OrganizationWalletRoute() {
  const { wallet_auto_recharge } = Route.useSearch()
  const { t } = useTranslation()

  useEffect(() => {
    if (wallet_auto_recharge === 'success') {
      toast.success(t('Setting updated successfully'))
    } else if (wallet_auto_recharge === 'failed') {
      toast.error(t('Payment request failed'))
    } else {
      return
    }
    window.history.replaceState({}, '', window.location.pathname)
  }, [t, wallet_auto_recharge])

  return <OrganizationWallet />
}
