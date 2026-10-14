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
import { createFileRoute, redirect } from '@tanstack/react-router'
import z from 'zod'

import { canAccessOrganizationArea } from '@/features/organizations/lib/organization-access'
import { UsageLogs } from '@/features/usage-logs'
import {
  isUsageLogsSectionId,
  USAGE_LOGS_DEFAULT_SECTION,
} from '@/features/usage-logs/section-registry'
import { useAuthStore } from '@/stores/auth-store'

// Same search schema as /usage-logs/$section: the usage-logs components read
// this route's search through the user route's types (usage-logs-route.ts).
const logTypeValues = ['0', '1', '2', '3', '4', '5', '6', '7'] as const
const logTypeSearchSchema = z
  .preprocess(
    (value) => {
      if (value == null || value === '') return undefined
      return Array.isArray(value) ? value : [value]
    },
    z.array(z.enum(logTypeValues)).optional()
  )
  .catch([])

const usageLogsSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
  type: logTypeSearchSchema.optional(),
  filter: z.string().optional().catch(''),
  model: z.string().optional().catch(''),
  token: z.string().optional().catch(''),
  channel: z.string().optional().catch(''),
  group: z.string().optional().catch(''),
  username: z.string().optional().catch(''),
  requestId: z.string().optional().catch(''),
  upstreamRequestId: z.string().optional().catch(''),
  startTime: z.number().optional(),
  endTime: z.number().optional(),
})

export const Route = createFileRoute(
  '/_authenticated/organization/usage-logs/$section'
)({
  beforeLoad: ({ params, search }) => {
    const { auth } = useAuthStore.getState()

    if (!canAccessOrganizationArea(auth.user)) {
      throw redirect({ to: '/403' })
    }

    if (!isUsageLogsSectionId(params.section)) {
      throw redirect({
        to: '/organization/usage-logs/$section',
        params: { section: USAGE_LOGS_DEFAULT_SECTION },
      })
    }
    // type only applies to common; drop it from the URL for other sections
    const hasTypeSearch = Array.isArray(search?.type)
      ? search.type.length > 0
      : search?.type != null && search.type !== ''
    if (params.section !== 'common' && hasTypeSearch) {
      throw redirect({
        to: '/organization/usage-logs/$section',
        params: { section: params.section },
        search: { ...search, type: undefined },
        replace: true,
      })
    }
  },
  validateSearch: usageLogsSearchSchema,
  component: OrganizationUsageLogs,
})

function OrganizationUsageLogs() {
  return <UsageLogs logsScope='organization' />
}
