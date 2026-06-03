# Organization Management Design

## Context

The project currently uses `User.Role` for global access levels:

- `RoleCommonUser = 1`
- `RoleAdminUser = 10`
- `RoleRootUser = 100`

The existing `User.Group` field is not an organizational/team field. It is already used for model access, channel availability, pricing ratios, top-up ratios, token group selection, subscription upgrades, logs, and performance metrics. Reusing `Group` for team management would mix billing/model semantics with organization membership.

## Goals

Add a new organization domain for team-like user management without changing the meaning of `Group` or granting organization managers global admin permissions.

Initial behavior:

- Global root users (`RoleRootUser`) create and manage organizations.
- Users can belong to one organization.
- Organization roles are `member`, `admin`, and `owner`.
- Organization admins can manage limited operational fields for users in their own organization.
- Organization owners can additionally manage membership and organization roles within their own organization.

## Non-Goals

This first version does not replace the global admin system, does not redesign permissions into a full RBAC model, and does not allow users to self-create organizations. It also does not let organization admins manage channels, model pricing, payment settings, global `User.Role`, `User.Group`, passwords, or global admins/root users.

## Data Model

Add an `Organization` model backed by an `organizations` table:

- `id`
- `name`
- `description`
- `owner_user_id`
- `status`
- `created_at`
- `updated_at`

Extend `User` with:

- `organization_id`
- `organization_role`

The initial organization roles are:

- `member`
- `admin`
- `owner`

Users are limited to one organization in this version. The organization model is intentionally separate from `Group`, so pricing/model access behavior remains unchanged.

## Authorization

Global `User.Role` and organization roles remain separate.

Organization admins and owners may still have `RoleCommonUser`. They must not pass existing `AdminAuth()` checks unless they also have a global admin role. Organization APIs should use dedicated helpers or middleware that check:

- authenticated user id
- global root/admin override where appropriate
- organization membership
- organization role
- target user organization
- target user global role

Global root users are never manageable through organization admin flows. Global admin users are also excluded from organization admin target operations in the initial version.

## API Shape

Use organization-specific API routes instead of widening existing admin routes.

Global root organization management under `/api/organizations`:

- create organization
- update organization metadata
- enable/disable organization
- assign users to organizations
- set organization roles

Current-user organization management under `/api/organization`:

- list users in the current user's organization
- view user details in the current user's organization
- update limited fields for users in the current user's organization
- for owners only, assign/remove users within their organization and update organization roles

This split keeps global root organization administration separate from current organization self-management.

## Allowed User Operations

Organization admins can initially:

- list users in their organization
- view user details in their organization
- enable or disable non-global-admin users in their organization
- adjust quota for non-global-admin users in their organization
- update notes or remarks for non-global-admin users in their organization
- view organization-scoped usage/log data where existing log queries can be safely scoped

Organization owners can additionally:

- add users to their organization
- remove users from their organization
- change organization roles within their organization

Organization admins and owners cannot:

- change global `User.Role`
- change `User.Group`
- change or reset passwords
- manage root users
- manage global admin users through organization flows
- manage channels, model pricing, payment settings, or system settings

## Frontend

The default frontend should display organization concepts separately from user groups. UI labels should use "Organization" in English and "조직" in Korean-facing copy. Organization admin screens should not rely on `role >= ROLE.ADMIN`; they should check explicit organization permissions from the authenticated user profile or a dedicated endpoint.

Existing admin screens should remain protected by the current global admin checks.

## Migrations

Database changes must be compatible with SQLite, MySQL 5.7.8+, and PostgreSQL 9.6+.

Use GORM migrations where possible. If raw SQL is required, follow the project's cross-database conventions and avoid database-specific types. Store `organization_role` as a short string with constants for `member`, `admin`, and `owner`.

## Testing

Backend tests should cover:

- role validation for organization roles
- organization admin cannot manage users outside the organization
- organization admin cannot manage global admins or root users
- organization owner can change organization roles within the organization
- global root can create and manage organizations
- existing global admin behavior is unchanged

Frontend tests or focused verification should cover:

- organization admin UI is visible to organization admins
- global admin UI remains hidden from organization-only admins
- user management actions are limited to the organization scope

## Future Expansion

This design leaves room to move toward broader organization administration later:

- organization-level settings
- organization-scoped API keys
- organization billing or quota budgets
- organization invitations
- multi-organization membership
- full permission/RBAC model
