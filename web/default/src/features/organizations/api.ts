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
import { api } from '@/lib/api'
import type {
  ApiResponse,
  OrganizationUserUpdatePayload,
  OrganizationUsersPage,
} from './types'

export async function getOrganizationUsers(params: {
  page?: number
  size?: number
}): Promise<ApiResponse<OrganizationUsersPage>> {
  const search = new URLSearchParams()
  if (params.page) search.set('p', String(params.page))
  if (params.size) search.set('page_size', String(params.size))
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.get(`/api/organization/users${suffix}`)
  return res.data
}

export async function updateOrganizationUser(
  userId: number,
  payload: OrganizationUserUpdatePayload
): Promise<ApiResponse> {
  const res = await api.patch(`/api/organization/users/${userId}`, payload)
  return res.data
}
