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
import { describe, expect, it } from 'vitest'

import {
  filterInterfaceLanguages,
  parseInterfaceLanguageOption,
  toInterfaceLanguageList,
} from './interface-languages'

const all = [
  { code: 'en', label: 'English' },
  { code: 'ko', label: '한국어' },
  { code: 'ja', label: '日本語' },
]

describe('filterInterfaceLanguages', () => {
  it('keeps only allowed codes', () => {
    expect(
      filterInterfaceLanguages(all, ['en', 'ko'], 'en').map((o) => o.code)
    ).toEqual(['en', 'ko'])
  })
  it('keeps a saved language that is no longer offered', () => {
    expect(
      filterInterfaceLanguages(all, ['en', 'ko'], 'ja').map((o) => o.code)
    ).toEqual(['en', 'ko', 'ja'])
  })
  it('falls back to every option when the list is empty', () => {
    expect(filterInterfaceLanguages(all, [], 'en')).toEqual(all)
    expect(filterInterfaceLanguages(all, undefined, 'en')).toEqual(all)
  })
})

describe('toInterfaceLanguageList', () => {
  it('keeps the string entries of an array', () => {
    expect(toInterfaceLanguageList(['en', 3, 'ko'])).toEqual(['en', 'ko'])
  })
  it('returns an empty list for anything else', () => {
    expect(toInterfaceLanguageList(undefined)).toEqual([])
    expect(toInterfaceLanguageList('en,ko')).toEqual([])
  })
})

describe('parseInterfaceLanguageOption', () => {
  it('maps the option value to interface codes in the admin order', () => {
    expect(parseInterfaceLanguageOption('kr,EN,xx,ko')).toEqual(['ko', 'en'])
    expect(parseInterfaceLanguageOption('')).toEqual([])
  })
  it('drops the removed interface languages', () => {
    expect(parseInterfaceLanguageOption('ja,zhCN,zhTW,fr,ru,vi,ko')).toEqual([
      'ko',
    ])
  })
})
