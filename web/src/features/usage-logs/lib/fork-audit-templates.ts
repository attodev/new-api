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

// fork: O3 — audit-log templates for the organization and top-up expiry actions.
// The backend stores only `action` + `params` in `other.op`; these English
// templates (also the i18n keys, see en.json/ko.json) are spread into
// AUDIT_TEMPLATES in format.ts. Each template interpolates only params that
// every call site records: organization.update and organization.user.update
// merge the changed fields into params, and organization.plan.update's title
// may be absent, so none of those are referenced.
export const FORK_AUDIT_TEMPLATES: Record<string, string> = {
  'organization.create':
    'Created organization “{{name}}” (ID {{organization_id}}) with owner {{owner_user_id}}',
  'organization.update': 'Updated organization {{organization_id}}',
  'organization.delete':
    'Deleted organization {{organization_id}} (owner {{owner_user_id}})',
  'organization.user.assign':
    'Assigned user {{user_id}} to organization {{organization_id}} as {{organization_role}}',
  'organization.user.update':
    'Updated member {{user_id}} of organization {{organization_id}}',
  'organization.user.remove':
    'Removed user {{user_id}} from organization {{organization_id}}',
  'organization.user.import':
    'Imported members into organization {{organization_id}} from Excel (assigned {{assigned}}, created {{created}}, updated {{updated}}, removed {{removed}}, errors {{errors}})',
  'organization.plan.create':
    'Created subscription plan “{{title}}” (ID {{plan_id}}) for organization {{organization_id}}',
  'organization.plan.update':
    'Updated subscription plan {{plan_id}} of organization {{organization_id}}',
  'organization.plan.disable':
    'Disabled subscription plan {{plan_id}} of organization {{organization_id}}',
  'organization.subscription.assign':
    'Assigned subscription plan {{plan_id}} of organization {{organization_id}} to user {{user_id}}',
  'organization.subscription.cancel':
    'Cancelled the active subscription of user {{user_id}} in organization {{organization_id}}',
  'organization.topup.expire':
    'Expired {{expired}} pending top-up orders of organization {{organization_id}}',
  'topup.expire':
    'Expired {{expired}} pending top-ups and {{expired_subscriptions}} subscription orders of user {{user_id}}',
}
