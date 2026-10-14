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
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'

import {
  validateEntry,
  isSkippable,
  isKeptLiteral,
  repairWhitespace,
  seedBlockedReason,
} from './ko-locale.mjs'

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

test('lets a sentence fragment carry no hangul but not stay english', () => {
  const en = 'Supports `-thinking`, `-thinking-'
  assert.deepEqual(
    validateEntry(en, '`-thinking`, `-thinking-', {
      skip: false,
      fragment: true,
    }),
    []
  )
  assert.ok(
    validateEntry(en, '`-thinking`, `-thinking-', { skip: false }).length > 0
  )
  assert.ok(validateEntry(en, en, { skip: false, fragment: true }).length > 0)
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

test('rejects a changed set of named tags', () => {
  const en = 'Click <strong>here</strong> to continue'
  assert.deepEqual(
    validateEntry(en, '계속하려면 <strong>여기</strong>를 클릭하세요', {
      skip: false,
    }),
    []
  )
  assert.ok(
    validateEntry(en, '계속하려면 여기를 클릭하세요', { skip: false }).some(
      (x) => x.includes('tag')
    )
  )
  assert.ok(
    validateEntry(en, '계속하려면 <b>여기</b>를 클릭하세요', {
      skip: false,
    }).some((x) => x.includes('tag'))
  )
  assert.ok(
    validateEntry('<ServerAddress>', '<서버주소>', { skip: false }).some((x) =>
      x.includes('tag')
    )
  )
  assert.ok(
    validateEntry('One<p>Two', '하나둘', { skip: false }).some((x) =>
      x.includes('tag')
    )
  )
  assert.ok(
    validateEntry('<think>', '생각', { skip: false }).some((x) =>
      x.includes('tag')
    )
  )
})

test('rejects a changed printf-style placeholder', () => {
  assert.deepEqual(
    validateEntry('Hello %s, you have %d items', '%s님, 항목이 %d개 있습니다', {
      skip: false,
    }),
    []
  )
  assert.ok(
    validateEntry('Hello %s', '안녕하세요', { skip: false }).some((x) =>
      x.includes('placeholder')
    )
  )
  assert.ok(
    validateEntry('%d items', '%s개 항목', { skip: false }).some((x) =>
      x.includes('placeholder')
    )
  )
  assert.deepEqual(validateEntry('50% off', '50% 할인', { skip: false }), [])
})

test('whitespace parity covers tabs, repeated spaces and the newline count', () => {
  assert.ok(
    validateEntry('\tIndented', ' 들여쓰기', { skip: false }).length > 0
  )
  assert.ok(validateEntry('Trailing  ', '끝 ', { skip: false }).length > 0)
  assert.deepEqual(validateEntry('Trailing  ', '끝  ', { skip: false }), [])
  assert.ok(
    validateEntry('a\nb\nc', '가\n나다', { skip: false }).some((x) =>
      x.includes('newline')
    )
  )
  assert.deepEqual(validateEntry('a\nb\nc', '가\n나\n다', { skip: false }), [])
})

test('auto-skips interpolation-only keys and identifiers without lowercase', () => {
  for (const key of [
    '{{method}} {{route}}',
    '{{start}}–{{end}}',
    'AZURE_OPENAI_ENDPOINT',
    'CPU',
    'HTTP/1.1',
    'SSL/TLS',
  ]) {
    assert.equal(isSkippable(key), true, key)
  }
  for (const key of ['Top P', 'vLLM', '{{value}}ms', '_copy', '，and ']) {
    assert.equal(isSkippable(key), true, key)
  }
  for (const key of [
    'N/A',
    'e.g. 401, 403',
    '1 Hour',
    'Save',
    '{{count}} items',
    'Throughput short',
  ]) {
    assert.equal(isSkippable(key), false, key)
  }
})

test('repairs whitespace drift by copying the english edges', () => {
  assert.equal(repairWhitespace('| Based on', ' | 기반 '), '| 기반')
  assert.equal(repairWhitespace('Reason:', '이유: '), '이유:')
  assert.equal(
    repairWhitespace('Can access. ', '접근할 수 있습니다.'),
    '접근할 수 있습니다. '
  )
  assert.equal(repairWhitespace(' Prefix', '접두어'), ' 접두어')
})

test('seed refuses to overwrite existing output unless forced', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'ko-seed-'))
  try {
    const ko = path.join(dir, 'ko.json')
    const batches = path.join(dir, 'ko-batches.json')
    assert.equal(seedBlockedReason([ko, batches], false), null)
    fs.writeFileSync(batches, '{}')
    assert.match(seedBlockedReason([ko, batches], false), /ko-batches\.json/)
    assert.match(seedBlockedReason([ko, batches], false), /--force/)
    assert.equal(seedBlockedReason([ko, batches], true), null)
    fs.writeFileSync(ko, '{}')
    assert.match(seedBlockedReason([ko, batches], false), /ko\.json/)
  } finally {
    fs.rmSync(dir, { recursive: true, force: true })
  }
})
