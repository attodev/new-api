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
 * A post-login redirect target that carries its own query string
 * (e.g. `/oauth2/authorize?client_id=...`). TanStack Router treats a `to`
 * containing `?` as part of the path, so such targets are followed with a
 * full same-origin document navigation instead. Only absolute paths on this
 * origin qualify (`//host` and backslash tricks are rejected).
 */
export function isSameOriginPathWithQuery(target?: string | null): boolean {
  return (
    typeof target === 'string' &&
    target.startsWith('/') &&
    !target.startsWith('//') &&
    !target.includes('\\') &&
    target.includes('?')
  )
}
