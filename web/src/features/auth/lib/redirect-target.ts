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
// fork: O13 — same-origin post-login redirect resolution for the OIDC pages.
import { replaceLocation } from '@/features/oidc/lib/location'

/**
 * Resolve a post-login redirect target against this origin and return the
 * normalized `pathname + search + hash`, or null when the target is not a
 * plain same-origin absolute path. Resolution (not string inspection) is the
 * check: the URL parser strips tabs/newlines and treats `\` like `/`, so
 * heuristics on the raw string can be bypassed (e.g. "/\t/evil.com").
 *
 * Callers MUST navigate to the returned value, never to the raw input.
 */
export function resolveSameOriginRedirect(
  target: string | undefined | null,
  origin: string = window.location.origin
): string | null {
  if (typeof target !== 'string' || !target.startsWith('/')) return null
  // oxlint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(target)) return null

  let url: URL
  try {
    url = new URL(target, origin)
  } catch {
    return null
  }
  if (url.origin !== origin) return null
  // A resolved path starting with "//" would be protocol-relative if used
  // as a bare location, so refuse it outright.
  if (url.pathname.startsWith('//')) return null
  return url.pathname + url.search + url.hash
}

const OIDC_PAGE_PREFIX = '/oauth2/'

/**
 * The full same-origin URL to load for a query-bearing OIDC page target
 * (`/oauth2/authorize?...`, `/oauth2/logout?...`), or null for any other
 * target, which the router navigates to as before.
 *
 * The OIDC pages are entered from Open WebUI by a document load and read the
 * raw query from `window.location`. Returning to them after sign-in with a
 * document load too keeps that one entry path: the page restores the session
 * the same way whether the user signed in just now or long ago.
 */
export function oidcPageRedirectHref(
  target: string | undefined | null,
  origin: string = window.location.origin
): string | null {
  const resolved = resolveSameOriginRedirect(target, origin)
  if (!resolved?.startsWith(OIDC_PAGE_PREFIX) || !resolved.includes('?')) {
    return null
  }
  return origin + resolved
}

/** Load an OIDC page target as a document. Returns false for other targets. */
export function replaceWithOidcPage(
  target: string | undefined | null
): boolean {
  const href = oidcPageRedirectHref(target)
  if (!href) return false
  replaceLocation(href)
  return true
}
