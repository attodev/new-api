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
import type { OrganizationRole } from '@/lib/organization-roles'

export type { OrganizationRole }

export interface OrganizationUser {
  id: number
  username: string
  display_name: string
  role: number
  /** Platform status (root-only); shown read-only, never sent by org admins. */
  status: number
  /** Organization-scoped status: 1 active, 2 suspended (no relay). */
  member_status: number
  quota: number
  used_quota: number
  group: string
  organization_id: number
  organization_role: OrganizationRole
  /** Organization-scoped note; the platform remark is root-only. */
  organization_remark?: string
}

/** GET /api/organization/users/lookup: identity only, no quota or remark. */
export interface OrganizationMemberLookup {
  id: number
  username: string
  display_name: string
  organization_id: number
}

export interface OrganizationUsersPage {
  page: number
  page_size: number
  total: number
  items: OrganizationUser[]
}

export interface Organization {
  id: number
  name: string
  description: string
  status: number
  owner_user_id: number
  quota: number
  used_quota: number
}

export interface OrganizationsPage {
  page: number
  page_size: number
  total: number
  items: Organization[]
}

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  message_key?: string
  data?: T
}

export type OrganizationUsersResponse = ApiResponse<OrganizationUsersPage>

export interface CreateOrganizationPayload {
  name: string
  description?: string
  owner_user_id: number
  quota?: number
}

export interface OrganizationUserUpdatePayload {
  member_status?: number
  quota?: number
  organization_remark?: string
}

export interface AssignOrganizationUserPayload {
  organization_role: Exclude<OrganizationRole, 'owner'>
}

export interface OrganizationUpdatePayload {
  name?: string
  description?: string
  quota?: number
  status?: number
}

/**
 * One rejected import row: `message_key` is the English template (a key of the
 * locale files) and `params` fill it. When the row failed on a nested message,
 * `params.error_key`/`params.error_params` carry that message's own key.
 * Line 0 is a failure of the remove-absent step.
 */
export interface OrgImportRowError {
  line: number
  username?: string
  message_key: string
  params?: Record<string, unknown>
}

export interface OrgImportResult {
  assigned: number
  created: number
  updated: number
  updated_usernames: string[]
  removed: number
  removed_usernames: string[]
  /** English text of each rejected row; `error_details` is the translatable form. */
  errors: string[]
  error_details?: OrgImportRowError[]
}

export type OrganizationDashboardRangePreset = 'today' | '7d' | '30d' | 'custom'

export interface OrganizationDashboardOrganization {
  id: number
  name: string
  quota: number
  used_quota: number
}

export interface OrganizationDashboardSummary {
  period_quota: number
  period_requests: number
  member_count: number
  active_member_count: number
}

export interface OrganizationDashboardDailyUsage {
  date: string
  quota: number
  requests: number
}

export interface OrganizationDashboardTopUser {
  user_id: number
  username: string
  display_name: string
  quota: number
  used_quota: number
  period_quota: number
  period_requests: number
}

export interface OrganizationDashboardUserDailyUsage {
  date: string
  user_id: number
  username: string
  display_name: string
  quota: number
  requests: number
}

export interface OrganizationDashboardTopModel {
  model: string
  quota: number
  requests: number
}

export interface OrganizationDashboardModelDailyUsage {
  date: string
  model: string
  quota: number
  requests: number
}

export interface OrganizationDashboardData {
  organization: OrganizationDashboardOrganization
  summary: OrganizationDashboardSummary
  daily_usage: OrganizationDashboardDailyUsage[]
  top_users: OrganizationDashboardTopUser[]
  top_models: OrganizationDashboardTopModel[]
  model_usage: OrganizationDashboardModelDailyUsage[]
  user_usage: OrganizationDashboardUserDailyUsage[]
}

export interface OrganizationDashboardParams {
  start_timestamp?: number | null
  end_timestamp?: number | null
  preset?: OrganizationDashboardRangePreset
  organization_id?: number | null
}

export type OrganizationSubscriptionDurationUnit =
  | 'year'
  | 'month'
  | 'day'
  | 'hour'
  | 'custom'

export type OrganizationSubscriptionResetPeriod =
  | 'never'
  | 'daily'
  | 'weekly'
  | 'monthly'
  | 'custom'

export interface OrganizationSubscriptionPlan {
  id: number
  organization_id: number
  title: string
  subtitle: string
  duration_unit: OrganizationSubscriptionDurationUnit
  duration_value: number
  custom_seconds: number
  enabled: boolean
  sort_order: number
  upgrade_group: string
  total_amount: number
  quota_reset_period: OrganizationSubscriptionResetPeriod
  quota_reset_custom_seconds: number
  created_at: number
  updated_at: number
}

export interface OrganizationUserSubscription {
  id: number
  organization_id: number
  user_id: number
  plan_id: number
  amount_total: number
  amount_used: number
  start_time: number
  end_time: number
  status: string
  last_reset_time: number
  next_reset_time: number
  upgrade_group: string
  prev_user_group: string
  assigned_by_user_id: number
  created_at: number
  updated_at: number
}

export interface OrganizationUserSubscriptionRecord {
  subscription: OrganizationUserSubscription
  user?: OrganizationUser
  plan?: OrganizationSubscriptionPlan
}

export interface OrganizationSubscriptionPlanPayload {
  title: string
  subtitle?: string
  duration_unit: OrganizationSubscriptionDurationUnit
  duration_value: number
  custom_seconds?: number
  sort_order?: number
  total_amount: number
}
