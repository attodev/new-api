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
// fork: O13 — covers same-origin redirect resolution for the OIDC pages.
import { afterEach, describe, expect, test, vi } from 'vitest'

import { replaceLocation } from '@/features/oidc/lib/location'

import {
  oidcPageRedirectHref,
  replaceWithOidcPage,
  resolveSameOriginRedirect,
} from './redirect-target'

vi.mock('@/features/oidc/lib/location', () => ({
  replaceLocation: vi.fn(),
}))

const ORIGIN = 'https://gw.example.com'
const resolve = (t: string | undefined) => resolveSameOriginRedirect(t, ORIGIN)

describe('resolveSameOriginRedirect', () => {
  test('accepts same-origin paths and returns the resolved value', () => {
    expect(resolve('/oauth2/authorize?state=1')).toBe(
      '/oauth2/authorize?state=1'
    )
    expect(resolve('/dashboard')).toBe('/dashboard')
    expect(resolve('/a/b?x=1#h')).toBe('/a/b?x=1#h')
  })

  test('normalizes dot segments in the returned value', () => {
    expect(resolve('/a/../b?y=1')).toBe('/b?y=1')
  })

  test('rejects protocol-relative and backslash targets', () => {
    expect(resolve('//evil.com?a')).toBe(null)
    expect(resolve('/\\evil.com?a')).toBe(null)
    expect(resolve('/\\/evil.com?a')).toBe(null)
  })

  test('rejects control characters the URL parser would strip', () => {
    expect(resolve('/\t/evil.com?a')).toBe(null)
    expect(resolve('/\n/evil.com?a')).toBe(null)
    expect(resolve('/\r/evil.com?a')).toBe(null)
    // percent-encoded form, after decoding as the router does
    expect(resolve(decodeURIComponent('/%09/evil.com?a'))).toBe(null)
  })

  test('rejects absolute and non-http schemes', () => {
    expect(resolve('javascript:alert(1)?x')).toBe(null)
    expect(resolve('https://evil.com/?a')).toBe(null)
    expect(resolve('https://gw.example.com/ok?a')).toBe(null)
    expect(resolve('data:text/html,x?y')).toBe(null)
  })

  test('never returns a value that could be read as protocol-relative', () => {
    // resolves same-origin but to "//x"; refused rather than returned bare
    expect(resolve('/..//x?y')).toBe(null)
  })

  test('rejects empty / non-string input', () => {
    expect(resolve(undefined)).toBe(null)
    expect(resolve('')).toBe(null)
    expect(resolve('dashboard')).toBe(null)
  })
})

describe('oidcPageRedirectHref', () => {
  test('returns the full same-origin URL for a query-bearing OIDC page', () => {
    expect(
      oidcPageRedirectHref(
        '/oauth2/authorize?client_id=openwebui&state=12345678901234567890123',
        ORIGIN
      )
    ).toBe(
      'https://gw.example.com/oauth2/authorize?client_id=openwebui&state=12345678901234567890123'
    )
    expect(oidcPageRedirectHref('/oauth2/logout?state=1', ORIGIN)).toBe(
      'https://gw.example.com/oauth2/logout?state=1'
    )
  })

  test('leaves every other target to the router', () => {
    expect(oidcPageRedirectHref('/pricing?view=grid', ORIGIN)).toBe(null)
    expect(oidcPageRedirectHref('/oauth2/authorize', ORIGIN)).toBe(null)
    expect(oidcPageRedirectHref('/dashboard', ORIGIN)).toBe(null)
    expect(oidcPageRedirectHref(undefined, ORIGIN)).toBe(null)
  })

  test('refuses OIDC-looking targets that do not resolve on this origin', () => {
    expect(oidcPageRedirectHref('//evil.com/oauth2/authorize?a', ORIGIN)).toBe(
      null
    )
    expect(
      oidcPageRedirectHref('/\t/evil.com/oauth2/authorize?a', ORIGIN)
    ).toBe(null)
    expect(oidcPageRedirectHref('/oauth2/../dashboard?x=1', ORIGIN)).toBe(null)
  })
})

describe('replaceWithOidcPage', () => {
  afterEach(() => {
    vi.mocked(replaceLocation).mockClear()
  })

  test('replaces the document for an OIDC page target', () => {
    expect(replaceWithOidcPage('/oauth2/authorize?state=x')).toBe(true)
    expect(replaceLocation).toHaveBeenCalledWith(
      `${window.location.origin}/oauth2/authorize?state=x`
    )
  })

  test('does nothing for other targets', () => {
    expect(replaceWithOidcPage('/pricing?view=grid')).toBe(false)
    expect(replaceWithOidcPage('/dashboard')).toBe(false)
    expect(replaceLocation).not.toHaveBeenCalled()
  })
})
