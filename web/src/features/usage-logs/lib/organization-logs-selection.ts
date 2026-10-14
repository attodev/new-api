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
/**
 * fork: O3 — which organization the organization-scope usage logs list.
 * Root picks one (remembered across pages); organization members are scoped
 * by their session and get no selection.
 */
import { useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useState } from 'react'

import { getOrganizations } from '@/features/organizations/api'
import {
  getLastSelectedOrganizationId,
  resolveAvailableOrganizationId,
  saveLastSelectedOrganizationId,
} from '@/features/organizations/lib/organization-selection'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import type { UsageLogsScope } from '../types'

export interface OrganizationLogsSelection {
  /** Root in the organization scope: queries wait for a chosen organization. */
  enabled: boolean
  organizations: { id: number; name: string }[]
  selectedOrganizationId: string
  organizationId?: number
  onChange: (organizationId: string) => void
}

export function useOrganizationLogsSelection(
  logsScope: UsageLogsScope
): OrganizationLogsSelection {
  const role = useAuthStore((state) => state.auth.user?.role ?? ROLE.GUEST)
  const enabled = logsScope === 'organization' && role >= ROLE.SUPER_ADMIN
  const [selectedOrganizationId, setSelectedOrganizationId] = useState(
    getLastSelectedOrganizationId
  )
  const { data } = useQuery({
    queryKey: ['organizations', 'usage-logs-selector'],
    queryFn: () => getOrganizations({ page: 1, size: 100 }),
    enabled,
  })
  const organizations = useMemo(() => data?.data?.items ?? [], [data])

  useEffect(() => {
    if (!enabled || organizations.length === 0) return
    const next = resolveAvailableOrganizationId(
      selectedOrganizationId,
      organizations
    )
    if (!next || next === selectedOrganizationId) return
    setSelectedOrganizationId(next)
    saveLastSelectedOrganizationId(next)
  }, [enabled, organizations, selectedOrganizationId])

  const onChange = useCallback((organizationId: string) => {
    setSelectedOrganizationId(organizationId)
    saveLastSelectedOrganizationId(organizationId)
  }, [])

  const parsedOrganizationId = Number(selectedOrganizationId)
  const isKnownOrganization = organizations.some(
    (organization) => organization.id === parsedOrganizationId
  )

  return {
    enabled,
    organizations,
    selectedOrganizationId,
    organizationId:
      enabled && isKnownOrganization ? parsedOrganizationId : undefined,
    onChange,
  }
}
