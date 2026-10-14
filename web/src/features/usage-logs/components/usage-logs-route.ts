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
 * fork: O3 — route access for usage-logs components that render under both
 * /usage-logs/$section and /organization/usage-logs/$section. Upstream binds
 * each component to the user route with getRouteApi, whose strict reads throw
 * on any other route; these non-strict reads work under either.
 */
import {
  useNavigate,
  useParams,
  useSearch,
  type UseNavigateResult,
} from '@tanstack/react-router'

import type { Route as UserUsageLogsRoute } from '@/routes/_authenticated/usage-logs/$section'

import type { UsageLogsScope } from '../types'
import { useUsageLogsContext } from './usage-logs-provider'

/** Both routes validate search with the same schema. */
export type UsageLogsSearch =
  (typeof UserUsageLogsRoute)['types']['fullSearchSchema']

const USAGE_LOGS_ROUTE = '/usage-logs/$section'
const ORGANIZATION_USAGE_LOGS_ROUTE = '/organization/usage-logs/$section'

/**
 * The section route for a scope. The organization route shares the user
 * route's params and search schema, so it is typed as the user route; the
 * cast keeps navigation type-checked before that route is registered.
 */
export function getUsageLogsRouteTo(
  logsScope: UsageLogsScope
): typeof USAGE_LOGS_ROUTE {
  return logsScope === 'organization'
    ? (ORGANIZATION_USAGE_LOGS_ROUTE as typeof USAGE_LOGS_ROUTE)
    : USAGE_LOGS_ROUTE
}

export function useUsageLogsRoute() {
  const { logsScope } = useUsageLogsContext()
  const params = useParams({ strict: false }) as { section?: string }
  const search = useSearch({ strict: false }) as UsageLogsSearch
  // Navigates relative to the current location (not the user route) and is
  // typed like the user route's navigate, which useTableUrlState expects.
  const navigate = useNavigate() as UseNavigateResult<typeof USAGE_LOGS_ROUTE>

  return {
    section: params.section,
    search,
    navigate,
    routeTo: getUsageLogsRouteTo(logsScope),
  }
}
