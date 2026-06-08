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
