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
import { useQuery } from '@tanstack/react-query'

import { getSelf } from '@/lib/api'
import type { AuthUser } from '@/stores/auth-store'

/**
 * Current user from the server, falling back to `fallback` (the login-time
 * snapshot in the auth store) until the fetch lands or when it fails.
 *
 * The auth store is only written at login, so balance and usage read straight
 * from it go stale. staleTime 0 re-fetches on every mount.
 * fork: O11 (team 490d215a4)
 */
export function useLiveSelf(fallback: AuthUser | null | undefined) {
  const selfQuery = useQuery({
    queryKey: ['user', 'self'],
    queryFn: async () => {
      const result = await getSelf()
      return result.success && result.data ? (result.data as AuthUser) : null
    },
    staleTime: 0,
  })

  return selfQuery.data ?? fallback
}
