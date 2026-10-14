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
const LAST_SELECTED_ORGANIZATION_ID_KEY = 'organization:last-selected-id'

type OrganizationLike = {
  id: number
}

export function getLastSelectedOrganizationId() {
  if (typeof window === 'undefined') return ''

  try {
    return window.localStorage.getItem(LAST_SELECTED_ORGANIZATION_ID_KEY) ?? ''
  } catch {
    return ''
  }
}

export function saveLastSelectedOrganizationId(organizationId: string) {
  if (typeof window === 'undefined') return

  try {
    if (organizationId) {
      window.localStorage.setItem(
        LAST_SELECTED_ORGANIZATION_ID_KEY,
        organizationId
      )
    } else {
      window.localStorage.removeItem(LAST_SELECTED_ORGANIZATION_ID_KEY)
    }
  } catch {
    // Storage can be unavailable in restricted browser contexts.
  }
}

export function resolveAvailableOrganizationId(
  organizationId: string,
  organizations: OrganizationLike[]
) {
  if (
    organizationId &&
    organizations.some(
      (organization) => String(organization.id) === organizationId
    )
  ) {
    return organizationId
  }

  return organizations[0] ? String(organizations[0].id) : ''
}
