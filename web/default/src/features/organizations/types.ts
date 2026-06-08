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
export type OrganizationRole = 'member' | 'admin' | 'owner'

export interface OrganizationUser {
  id: number
  username: string
  display_name: string
  role: number
  status: number
  quota: number
  used_quota: number
  group: string
  organization_id: number
  organization_role: OrganizationRole
  remark?: string
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
  status?: number
  quota?: number
  remark?: string
}

export interface AssignOrganizationUserPayload {
  organization_role: OrganizationRole
}

export interface OrganizationUpdatePayload {
  description?: string
  quota?: number
  status?: number
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
