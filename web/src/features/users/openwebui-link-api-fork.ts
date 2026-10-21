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
// fork: O13 — admin API for the Open WebUI ↔ new-api user link. Linking and
// unlinking send a single-use X-Security-Proof (develop's step-up).
import type { AxiosRequestConfig } from 'axios'

import { api } from '@/lib/api'

import type { ApiResponse } from './types'

export type OpenWebUITokenStatus = {
  name: string
  exists: boolean
  status: number
}

export type OpenWebUILink = {
  id: number
  sub: string
  user_id: number
  email: string
  linked_by: 'auto' | 'admin'
  created_at: number
  tokens: OpenWebUITokenStatus[]
}

/** Binding type the unlink proof is bound to (`admin.user.binding.clear`). */
export const OPENWEBUI_BINDING_TYPE = 'openwebui'

const proofConfig = (proofToken: string): AxiosRequestConfig => ({
  headers: { 'X-Security-Proof': proofToken },
  singleUseAuthorization: true,
})

export async function getOpenWebUILink(
  userId: number
): Promise<ApiResponse<OpenWebUILink | null>> {
  const res = await api.get('/api/openwebui/links', {
    params: { user_id: userId },
  })
  return res.data
}

/** Needs an `admin.user.update` proof for `{ user_id }`. */
export async function createOpenWebUILink(
  userId: number,
  sub: string,
  proofToken: string
): Promise<ApiResponse<OpenWebUILink>> {
  const res = await api.post(
    '/api/openwebui/links',
    { user_id: userId, sub },
    proofConfig(proofToken)
  )
  return res.data
}

/** Needs an `admin.user.binding.clear` proof for `{ user_id, binding_type }`. */
export async function deleteOpenWebUILink(
  linkId: number,
  proofToken: string
): Promise<ApiResponse<null>> {
  const res = await api.delete(
    `/api/openwebui/links/${linkId}`,
    proofConfig(proofToken)
  )
  return res.data
}
