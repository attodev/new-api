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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  isSameOriginPathWithQuery,
  resolveSameOriginRedirect,
} from './redirect-target'

const ORIGIN = 'https://gw.example.com'
const resolve = (t: string | undefined) => resolveSameOriginRedirect(t, ORIGIN)

describe('resolveSameOriginRedirect', () => {
  test('accepts same-origin paths and returns the resolved value', () => {
    assert.equal(
      resolve('/oauth2/authorize?state=1'),
      '/oauth2/authorize?state=1'
    )
    assert.equal(resolve('/dashboard'), '/dashboard')
    assert.equal(resolve('/a/b?x=1#h'), '/a/b?x=1#h')
  })

  test('normalizes dot segments in the returned value', () => {
    assert.equal(resolve('/a/../b?y=1'), '/b?y=1')
  })

  test('rejects protocol-relative and backslash targets', () => {
    assert.equal(resolve('//evil.com?a'), null)
    assert.equal(resolve('/\\evil.com?a'), null)
    assert.equal(resolve('/\\/evil.com?a'), null)
  })

  test('rejects control characters the URL parser would strip', () => {
    assert.equal(resolve('/\t/evil.com?a'), null)
    assert.equal(resolve('/\n/evil.com?a'), null)
    assert.equal(resolve('/\r/evil.com?a'), null)
    // percent-encoded form, after decoding as the router does
    assert.equal(resolve(decodeURIComponent('/%09/evil.com?a')), null)
  })

  test('rejects absolute and non-http schemes', () => {
    assert.equal(resolve('javascript:alert(1)?x'), null)
    assert.equal(resolve('https://evil.com/?a'), null)
    assert.equal(resolve('https://gw.example.com/ok?a'), null)
    assert.equal(resolve('data:text/html,x?y'), null)
  })

  test('never returns a value that could be read as protocol-relative', () => {
    // resolves same-origin but to "//x"; refused rather than returned bare
    assert.equal(resolve('/..//x?y'), null)
  })

  test('rejects empty / non-string input', () => {
    assert.equal(resolve(undefined), null)
    assert.equal(resolve(''), null)
    assert.equal(resolve('dashboard'), null)
  })
})

describe('isSameOriginPathWithQuery', () => {
  test('requires a safe target that contains a query', () => {
    assert.equal(
      isSameOriginPathWithQuery('/oauth2/authorize?state=1', ORIGIN),
      true
    )
    assert.equal(isSameOriginPathWithQuery('/dashboard', ORIGIN), false)
    assert.equal(isSameOriginPathWithQuery('/\t/evil.com?a', ORIGIN), false)
    assert.equal(isSameOriginPathWithQuery('//evil.com?a', ORIGIN), false)
  })
})
