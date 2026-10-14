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
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

import { findLocaleIssues } from '../../../scripts/ko-locale.mjs'

// fork: O9 — merge gate for the Korean locale. After an upstream merge adds
// en.json keys, `bun run i18n:sync` fills ko.json with english placeholders;
// this test turns that gap red in `bun run test` until the keys are
// translated (or listed in docs/i18n/ko-skip.json via MANUAL_SKIP + seed).

const here = dirname(fileURLToPath(import.meta.url))

function readTranslation(file: string): Record<string, unknown> {
  const data = JSON.parse(readFileSync(file, 'utf8')) as {
    translation: Record<string, unknown>
  }
  return data.translation
}

describe('Korean locale coverage', () => {
  it('translates every en.json key or keeps it in the skip list', () => {
    const en = readTranslation(resolve(here, '../locales/en.json'))
    const ko = readTranslation(resolve(here, '../locales/ko.json'))
    const skip = JSON.parse(
      readFileSync(resolve(here, '../../../../docs/i18n/ko-skip.json'), 'utf8')
    ) as string[]

    const issues = findLocaleIssues(en, ko, new Set(skip)).map(
      ([key, problems]) => `${JSON.stringify(key)}: ${problems.join('; ')}`
    )
    expect(issues).toEqual([])
  })

  it('reports a missing key and an english placeholder', () => {
    const en = { Save: 'Save', Delete: 'Delete', OAuth: 'OAuth' }
    const ko = { Save: 'Save', OAuth: 'OAuth' }
    expect(findLocaleIssues(en, ko, new Set(['OAuth']))).toEqual([
      ['Save', ['untranslated (equals english)', 'no hangul']],
      ['Delete', ['missing']],
    ])
  })
})
