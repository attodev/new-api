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
// fork: O13 — Open WebUI site settings: option types/defaults, relay-status and
// signing-key API, and the ordered option save shared by the two sections.
import type { QueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { STATUS_QUERY_KEY, STATUS_STORAGE_KEY } from '@/lib/status-query'

import type { UpdateOptionRequest } from '../types'

/**
 * Non-secret `oauth2.*` (built-in OIDC provider) and `openwebui.*` (relay)
 * options read from GET /api/option/. The secrets (oauth2.client_secret,
 * openwebui.service_api_key, openwebui.jwt_secret,
 * openwebui.jwt_previous_secret) are deliberately absent: they are write-only
 * and the forms never prefill them.
 */
export type OpenWebUISiteSettings = {
  'oauth2.enabled': boolean
  'oauth2.client_id': string
  'oauth2.redirect_uri': string
  'oauth2.app_url': string
  'oauth2.post_logout_redirect_uri': string
  'oauth2.open_in_new_window': boolean
  'oauth2.replace_playground': boolean
  'openwebui.relay_enabled': boolean
  'openwebui.jwt_issuer': string
  'openwebui.jwt_leeway_seconds': number
  'openwebui.allow_unsigned_requests': boolean
}

export const OPENWEBUI_SITE_DEFAULTS: OpenWebUISiteSettings = {
  'oauth2.enabled': false,
  'oauth2.client_id': '',
  'oauth2.redirect_uri': '',
  'oauth2.app_url': '',
  'oauth2.post_logout_redirect_uri': '',
  'oauth2.open_in_new_window': false,
  'oauth2.replace_playground': false,
  'openwebui.relay_enabled': false,
  'openwebui.jwt_issuer': 'open-webui',
  'openwebui.jwt_leeway_seconds': 60,
  'openwebui.allow_unsigned_requests': true,
}

export type OpenWebUIOAuth2Defaults = Pick<
  OpenWebUISiteSettings,
  Extract<keyof OpenWebUISiteSettings, `oauth2.${string}`>
>

export type OpenWebUIRelayDefaults = Pick<
  OpenWebUISiteSettings,
  Extract<keyof OpenWebUISiteSettings, `openwebui.${string}`>
>

export type OpenWebUIRelayStatus = {
  relay_enabled: boolean
  service_key_configured: boolean
  jwt_secret_configured: boolean
  jwt_previous_secret_configured: boolean
  jwt_issuer: string
  jwt_leeway_seconds: number
  allow_unsigned_requests: boolean
}

export type OidcSigningKeyInfo = { kid: string; created_at: number }

export type OidcSigningKeyStatus = {
  current: OidcSigningKeyInfo
  previous: OidcSigningKeyInfo | null
}

type ApiResult<T> = { success: boolean; message?: string; data?: T }

export const OPENWEBUI_RELAY_STATUS_QUERY_KEY = ['openwebui-relay-status']
export const OIDC_SIGNING_KEY_STATUS_QUERY_KEY = ['oidc-signing-key-status']

export async function getOpenWebUIRelayStatus() {
  const res = await api.get<ApiResult<OpenWebUIRelayStatus>>(
    '/api/openwebui/relay-status'
  )
  return res.data
}

export async function getOidcSigningKeyStatus() {
  const res = await api.get<ApiResult<OidcSigningKeyStatus>>(
    '/api/oauth2/signing-key-status'
  )
  return res.data
}

export async function rotateOidcSigningKey() {
  const res = await api.post<ApiResult<OidcSigningKeyStatus>>(
    '/api/oauth2/rotate-signing-key'
  )
  return res.data
}

/** Same rule as the server's IsAbsoluteHTTPURL: http(s), a host, no fragment. */
export function isAbsoluteHttpUrl(raw: string): boolean {
  let url: URL
  try {
    url = new URL(raw.trim())
  } catch {
    return false
  }
  return (
    (url.protocol === 'http:' || url.protocol === 'https:') &&
    url.hostname !== '' &&
    !raw.includes('#')
  )
}

/**
 * Order option saves around an enable switch. The server validates enabling
 * against the values already stored, so the switch is saved after the other
 * fields when it turns on, and before them when it turns off (the feature is
 * never live with half-saved settings).
 */
export function orderAroundEnableKey(
  updates: UpdateOptionRequest[],
  enableKey: string
): UpdateOptionRequest[] {
  const toggle = updates.find((u) => u.key === enableKey)
  if (!toggle) return updates
  const rest = updates.filter((u) => u.key !== enableKey)
  return toggle.value === true ? [...rest, toggle] : [toggle, ...rest]
}

/**
 * Save options one by one, stopping at the first failure. useUpdateOption
 * already shows the (translated) server error, so this only reports whether
 * every save succeeded.
 */
export async function saveOptionsInOrder(
  save: (request: UpdateOptionRequest) => Promise<unknown>,
  updates: UpdateOptionRequest[]
): Promise<boolean> {
  for (const update of updates) {
    try {
      await save(update)
    } catch {
      return false
    }
  }
  return true
}

/**
 * /api/status derives oauth2_* (the Playground "Open in Chat App" button and
 * replace mode) from every oauth2.* option, so a save must drop both the
 * React Query entry and the localStorage snapshot readCachedStatus() serves,
 * as useUpdateOption does for its STATUS_RELATED_KEYS.
 */
export function refreshCachedStatus(queryClient: QueryClient): void {
  void queryClient.invalidateQueries({ queryKey: STATUS_QUERY_KEY })
  try {
    window.localStorage.removeItem(STATUS_STORAGE_KEY)
  } catch {
    /* Storage may be disabled. */
  }
}

/** Wrap an option save so each successful save refreshes the cached status. */
export function withStatusRefresh(
  save: (request: UpdateOptionRequest) => Promise<unknown>,
  queryClient: QueryClient
): (request: UpdateOptionRequest) => Promise<unknown> {
  return async (request) => {
    const result = await save(request)
    refreshCachedStatus(queryClient)
    return result
  }
}
