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
// fork: O13 — same-origin halves of the OIDC authorize and end-session endpoints.
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
  /** OAuth error code: email_required, invalid_client, invalid_request, ... */
  error?: string
  message?: string
  message_key?: string
  data?: { redirect_url: string }
}

const AUTHORIZE_PARAM_NAMES = [
  'response_type',
  'client_id',
  'redirect_uri',
  'scope',
  'state',
  'nonce',
  'code_challenge',
  'code_challenge_method',
] as const satisfies readonly (keyof OidcAuthorizeParams)[]

/** The authorize parameters exactly as Open WebUI put them in the query. */
export function readAuthorizeParams(search: string): OidcAuthorizeParams {
  const query = new URLSearchParams(search)
  const params: OidcAuthorizeParams = {}
  for (const name of AUTHORIZE_PARAM_NAMES) {
    const value = query.get(name)
    if (value !== null) params[name] = value
  }
  return params
}

/**
 * POST /api/oauth2/authorize with the dashboard Bearer token. A 401 means
 * "no usable session" and is handled by the page (sign in, then come back),
 * so the client's refresh-and-redirect handling is turned off here: it would
 * send the browser to a bare /sign-in and lose the authorize request.
 */
export async function authorizeOidc(
  params: OidcAuthorizeParams
): Promise<{ status: number; body: OidcAuthorizeResponse | null }> {
  try {
    const res = await api.post<OidcAuthorizeResponse>(
      '/api/oauth2/authorize',
      params,
      { skipAuthRefresh: true, skipErrorHandler: true, skipBusinessError: true }
    )
    return { status: res.status, body: res.data }
  } catch (err) {
    const response = (
      err as { response?: { status?: number; data?: OidcAuthorizeResponse } }
    )?.response
    return { status: response?.status ?? 0, body: response?.data ?? null }
  }
}

export type OidcLogoutParams = {
  id_token_hint?: string
  post_logout_redirect_uri?: string
  state?: string
  /** Set only after the user clicked "Sign out" on the confirm screen. */
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

/** The end-session parameters exactly as Open WebUI put them in the query. */
export function readLogoutParams(search: string): OidcLogoutParams {
  const query = new URLSearchParams(search)
  const params: OidcLogoutParams = {}
  for (const name of [
    'id_token_hint',
    'post_logout_redirect_uri',
    'state',
  ] as const) {
    const value = query.get(name)
    if (value !== null) params[name] = value
  }
  return params
}

/**
 * POST /api/oauth2/logout. The server identifies the session by the Bearer
 * token alone (the refresh cookie is scoped to /api/user/auth and is not
 * sent here), so callers restore the session before calling this.
 */
export async function logoutOidc(
  params: OidcLogoutParams
): Promise<OidcLogoutResponse> {
  const res = await api.post<OidcLogoutResponse>('/api/oauth2/logout', params, {
    skipAuthRefresh: true,
    skipErrorHandler: true,
    skipBusinessError: true,
  })
  return res.data
}
