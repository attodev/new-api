export const ORGANIZATION_ROLE = {
  MEMBER: 'member',
  ADMIN: 'admin',
  OWNER: 'owner',
} as const

export type OrganizationRole =
  (typeof ORGANIZATION_ROLE)[keyof typeof ORGANIZATION_ROLE]

export function hasOrganizationAdminRole(role?: string): boolean {
  return role === ORGANIZATION_ROLE.ADMIN || role === ORGANIZATION_ROLE.OWNER
}

export function hasOrganizationOwnerRole(role?: string): boolean {
  return role === ORGANIZATION_ROLE.OWNER
}
