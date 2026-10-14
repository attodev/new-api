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
import test from 'node:test'

import { validateEntry, isSkippable, isKeptLiteral } from './ko-locale.mjs'

test('accepts a faithful translation', () => {
  assert.deepEqual(
    validateEntry('Top up {{amount}} now', '지금 {{amount}} 충전', {
      skip: false,
    }),
    []
  )
})

test('rejects a changed interpolation set', () => {
  const v = validateEntry('{{count}} items', '{{개수}}개 항목', { skip: false })
  assert.ok(v.some((x) => x.includes('interpolation')))
})

test('rejects a missing Trans tag', () => {
  const v = validateEntry('Read the <0>docs</0>', '문서를 읽으세요', {
    skip: false,
  })
  assert.ok(v.some((x) => x.includes('tag')))
})

test('rejects an untranslated english value unless skipped', () => {
  assert.ok(
    validateEntry('Save group settings', 'Save group settings', { skip: false })
      .length > 0
  )
  assert.deepEqual(
    validateEntry('https://example.com', 'https://example.com', { skip: true }),
    []
  )
})

test('rejects leading/trailing whitespace drift', () => {
  assert.ok(validateEntry(' Prefix', '접두어', { skip: false }).length > 0)
})

test('accepts a korean value that contains ascii words', () => {
  assert.deepEqual(validateEntry('API key', 'API 키', { skip: false }), [])
})

test('rejects a value that is not a string', () => {
  assert.ok(validateEntry('Save', 42, { skip: false }).length > 0)
})

test('rejects newline drift', () => {
  assert.ok(
    validateEntry('Line one\nLine two', '첫째 줄 둘째 줄', { skip: false })
      .length > 0
  )
})

test('skips numbers, json and urls but not interpolated sentences', () => {
  assert.equal(isSkippable('10000'), true)
  assert.equal(isSkippable('{"a": "b"}'), true)
  assert.equal(isSkippable('https://example.com'), true)
  assert.equal(isSkippable('s'), true)
  assert.equal(isSkippable('{{count}} items'), false)
  assert.equal(isSkippable('Save group settings'), false)
})

test('keeps a value english only when team and zh both kept it', () => {
  assert.equal(isKeptLiteral('OpenAI', 'OpenAI', 'OpenAI'), true)
  assert.equal(isKeptLiteral('Baidu', 'Baidu', '百度'), false)
  assert.equal(isKeptLiteral('New key', undefined, 'New key'), false)
  assert.equal(isKeptLiteral('Save', '저장', 'Save'), false)
})
