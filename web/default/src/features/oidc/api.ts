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

export type OidcAuthorizeParams = {
  response_type?: string
  client_id?: string
  redirect_uri?: string
  scope?: string
  state?: string
  nonce?: string
  code_challenge?: string
  code_challenge_method?: string
}

export type OidcAuthorizeResponse = {
  success: boolean
  error?: string
  message?: string
  data?: { redirect_url: string }
}

/**
 * Same-origin half of the OIDC authorize endpoint. The browser lands on
 * /oauth2/authorize from Open WebUI (a cross-site navigation that carries
 * no SameSite=Strict session cookie); this XHR is same-origin, so the
 * cookie is sent and the server can tell whether the user is logged in.
 * A 401 means "not logged in" and is handled by the page, so only the global
 * 401 toast is suppressed (the interceptor still resets the auth store).
 */
export async function authorizeOidc(
  params: OidcAuthorizeParams
): Promise<{ status: number; body: OidcAuthorizeResponse | null }> {
  try {
    const res = await api.post<OidcAuthorizeResponse>(
      '/api/oauth2/authorize',
      params,
      { skipErrorHandler: true, skipBusinessError: true }
    )
    return { status: res.status, body: res.data }
  } catch (err) {
    const status =
      (err as { response?: { status?: number } })?.response?.status ?? 0
    const body =
      (err as { response?: { data?: OidcAuthorizeResponse } })?.response
        ?.data ?? null
    return { status, body }
  }
}

export type OidcLogoutParams = {
  id_token_hint?: string
  post_logout_redirect_uri?: string
  state?: string
  /** The user confirmed on the page; needed when no valid hint is sent. */
  confirmed?: boolean
}

export type OidcLogoutResponse = {
  success: boolean
  data?: {
    /** Set only when the server ended the session. */
    redirect_url: string | null
    /** The server did not log out and wants the user to confirm. */
    needs_confirmation?: boolean
  }
}

export async function logoutOidc(
  params: OidcLogoutParams
): Promise<OidcLogoutResponse> {
  const res = await api.post<OidcLogoutResponse>('/api/oauth2/logout', params, {
    skipErrorHandler: true,
    skipBusinessError: true,
  })
  return res.data
}
