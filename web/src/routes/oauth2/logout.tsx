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
// fork: O13 — OIDC end-session page; public route, the page restores the session itself.
import { createFileRoute } from '@tanstack/react-router'

import { OidcLogoutPage } from '@/features/oidc/components/oidc-logout-page'

export const Route = createFileRoute('/oauth2/logout')({
  // Permissive on purpose: the page reads window.location.search itself.
  validateSearch: (search: Record<string, unknown>) => search,
  component: OidcLogoutPage,
})
