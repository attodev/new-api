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
  AmountRequest,
  AmountResponse,
  ApiResponse as WalletApiResponse,
  BillingHistoryResponse,
  CreemPaymentRequest,
  CreemPaymentResponse,
  PayPalPaymentRequest,
  PayPalPaymentResponse,
  PaymentRequest,
  PaymentResponse,
  StripePaymentResponse,
  WaffoPancakePaymentRequest,
  WaffoPancakePaymentResponse,
  WaffoPaymentRequest,
  WaffoPaymentResponse,
} from '@/features/wallet/types'
import type {
  ApiResponse,
  AssignOrganizationUserPayload,
  CreateOrganizationPayload,
  Organization,
  OrganizationDashboardData,
  OrganizationDashboardParams,
  OrgImportResult,
  OrganizationSubscriptionPlan,
  OrganizationSubscriptionPlanPayload,
  OrganizationUserSubscription,
  OrganizationUserSubscriptionRecord,
  OrganizationsPage,
  OrganizationUpdatePayload,
  OrganizationUserUpdatePayload,
  OrganizationUsersPage,
} from './types'

export async function createOrganization(
  payload: CreateOrganizationPayload
): Promise<ApiResponse<Organization>> {
  const res = await api.post('/api/organizations/', payload)
  return res.data
}

export async function getOrganizations(params: {
  page?: number
  size?: number
}): Promise<ApiResponse<OrganizationsPage>> {
  const search = new URLSearchParams()
  if (params.page) search.set('p', String(params.page))
  if (params.size) search.set('page_size', String(params.size))
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.get(`/api/organizations/${suffix}`)
  return res.data
}

export async function updateOrganization(
  organizationId: number,
  payload: OrganizationUpdatePayload
): Promise<ApiResponse> {
  const res = await api.patch(`/api/organizations/${organizationId}`, payload)
  return res.data
}

export async function deleteOrganization(organizationId: number): Promise<void> {
  await api.delete(`/api/organizations/${organizationId}`)
}

export async function exportOrgUsers(organizationId?: number): Promise<void> {
  const params = organizationId ? `?organization_id=${organizationId}` : ''
  const res = await api.get(`/api/organization/users/export${params}`, {
    responseType: 'blob',
  })
  const url = URL.createObjectURL(new Blob([res.data as BlobPart]))
  const a = document.createElement('a')
  a.href = url
  a.download = `org-users-${Date.now()}.xlsx`
  a.click()
  URL.revokeObjectURL(url)
}

export async function importOrgUsers(
  file: File,
  organizationId?: number
): Promise<OrgImportResult> {
  const params = organizationId ? `?organization_id=${organizationId}` : ''
  const form = new FormData()
  form.append('file', file)
  const res = await api.post(`/api/organization/users/import${params}`, form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return res.data.data as OrgImportResult
}

export async function getOrganizationProfile(): Promise<
  ApiResponse<Organization>
> {
  const res = await api.get('/api/organization/')
  return res.data
}

export async function getOrganizationWallet(): Promise<
  ApiResponse<Organization>
> {
  const res = await api.get('/api/organization/wallet')
  return res.data
}

export async function getOrganizationDashboard(
  params: OrganizationDashboardParams
): Promise<ApiResponse<OrganizationDashboardData>> {
  const search = new URLSearchParams()
  if (params.start_timestamp !== undefined && params.start_timestamp !== null) {
    search.set('start_timestamp', String(params.start_timestamp))
  }
  if (params.end_timestamp !== undefined && params.end_timestamp !== null) {
    search.set('end_timestamp', String(params.end_timestamp))
  }
  if (params.preset) {
    search.set('preset', params.preset)
  }
  if (params.organization_id !== undefined && params.organization_id !== null) {
    search.set('organization_id', String(params.organization_id))
  }
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.get(`/api/organization/dashboard${suffix}`)
  return res.data
}

export async function getOrganizationBillingHistory(
  page: number,
  pageSize: number,
  keyword?: string
): Promise<WalletApiResponse<BillingHistoryResponse>> {
  const params = new URLSearchParams({
    p: page.toString(),
    page_size: pageSize.toString(),
  })
  if (keyword) {
    params.append('keyword', keyword)
  }
  const res = await api.get(`/api/organization/topup/self?${params.toString()}`)
  return res.data
}

export async function calculateOrganizationAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/organization/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function calculateOrganizationStripeAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/organization/stripe/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function calculateOrganizationPayPalAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/organization/paypal/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function calculateOrganizationWaffoPancakeAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/organization/waffo-pancake/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function requestOrganizationPayment(
  request: PaymentRequest
): Promise<PaymentResponse> {
  const res = await api.post('/api/organization/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return {
    ...res.data,
    url: res.data.url || (res as unknown as { url?: string }).url,
  }
}

export async function requestOrganizationStripePayment(
  request: PaymentRequest
): Promise<StripePaymentResponse> {
  const res = await api.post('/api/organization/stripe/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function requestOrganizationPayPalPayment(
  request: PayPalPaymentRequest
): Promise<PayPalPaymentResponse> {
  const res = await api.post('/api/organization/paypal/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function requestOrganizationCreemPayment(
  request: CreemPaymentRequest
): Promise<CreemPaymentResponse> {
  const res = await api.post('/api/organization/creem/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function requestOrganizationWaffoPayment(
  request: WaffoPaymentRequest
): Promise<WaffoPaymentResponse> {
  const res = await api.post('/api/organization/waffo/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function requestOrganizationWaffoPancakePayment(
  request: WaffoPancakePaymentRequest
): Promise<WaffoPancakePaymentResponse> {
  const res = await api.post('/api/organization/waffo-pancake/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function getOrganizationUsers(params: {
  page?: number
  size?: number
  organization_id?: number | null
}): Promise<ApiResponse<OrganizationUsersPage>> {
  const search = new URLSearchParams()
  if (params.page) search.set('p', String(params.page))
  if (params.size) search.set('page_size', String(params.size))
  appendOrganizationId(search, params.organization_id)
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.get(`/api/organization/users${suffix}`)
  return res.data
}

export async function getAssignableOrganizationUsers(params: {
  page?: number
  size?: number
}): Promise<ApiResponse<OrganizationUsersPage>> {
  const search = new URLSearchParams()
  if (params.page) search.set('p', String(params.page))
  if (params.size) search.set('page_size', String(params.size))
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.get(`/api/organization/assignable-users${suffix}`)
  return res.data
}

export async function updateOrganizationUser(
  userId: number,
  payload: OrganizationUserUpdatePayload
): Promise<ApiResponse> {
  const res = await api.patch(`/api/organization/users/${userId}`, payload)
  return res.data
}

export async function assignOrganizationUser(
  userId: number,
  payload: AssignOrganizationUserPayload
): Promise<ApiResponse> {
  const res = await api.put(
    `/api/organization/users/${userId}/membership`,
    payload
  )
  return res.data
}

function appendOrganizationId(
  search: URLSearchParams,
  organizationId?: number | null
) {
  if (organizationId !== undefined && organizationId !== null) {
    search.set('organization_id', String(organizationId))
  }
}

export async function getOrganizationSubscriptionPlans(params: {
  organization_id?: number | null
} = {}): Promise<ApiResponse<OrganizationSubscriptionPlan[]>> {
  const search = new URLSearchParams()
  appendOrganizationId(search, params.organization_id)
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.get(`/api/organization/subscription/plans${suffix}`)
  return res.data
}

export async function createOrganizationSubscriptionPlan(
  payload: OrganizationSubscriptionPlanPayload,
  organizationId?: number | null
): Promise<ApiResponse<OrganizationSubscriptionPlan>> {
  const search = new URLSearchParams()
  appendOrganizationId(search, organizationId)
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.post(
    `/api/organization/subscription/plans${suffix}`,
    payload
  )
  return res.data
}

export async function updateOrganizationSubscriptionPlan(
  planId: number,
  payload: OrganizationSubscriptionPlanPayload,
  organizationId?: number | null
): Promise<ApiResponse> {
  const search = new URLSearchParams()
  appendOrganizationId(search, organizationId)
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.patch(
    `/api/organization/subscription/plans/${planId}${suffix}`,
    payload
  )
  return res.data
}

export async function disableOrganizationSubscriptionPlan(
  planId: number,
  organizationId?: number | null
): Promise<ApiResponse> {
  const search = new URLSearchParams()
  appendOrganizationId(search, organizationId)
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.delete(
    `/api/organization/subscription/plans/${planId}${suffix}`
  )
  return res.data
}

export async function getOrganizationUserSubscriptions(params: {
  organization_id?: number | null
} = {}): Promise<ApiResponse<OrganizationUserSubscriptionRecord[]>> {
  const search = new URLSearchParams()
  appendOrganizationId(search, params.organization_id)
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.get(`/api/organization/subscription/users${suffix}`)
  return res.data
}

export async function assignOrganizationUserSubscription(
  userId: number,
  planId: number,
  organizationId?: number | null
): Promise<ApiResponse<OrganizationUserSubscription>> {
  const search = new URLSearchParams()
  appendOrganizationId(search, organizationId)
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.put(
    `/api/organization/subscription/users/${userId}${suffix}`,
    { plan_id: planId }
  )
  return res.data
}

export async function cancelOrganizationUserSubscription(
  userId: number,
  organizationId?: number | null
): Promise<ApiResponse> {
  const search = new URLSearchParams()
  appendOrganizationId(search, organizationId)
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.delete(
    `/api/organization/subscription/users/${userId}${suffix}`
  )
  return res.data
}

export async function getMyOrganizationSubscription(): Promise<
  ApiResponse<OrganizationUserSubscriptionRecord | null>
> {
  const res = await api.get('/api/organization/subscription/self')
  return res.data
}
