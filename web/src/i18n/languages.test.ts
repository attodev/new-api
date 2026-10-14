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
import { describe, expect, test } from 'vitest'

import { resources } from './config'
import {
  convertDetectedLanguage,
  INTERFACE_LANGUAGE_OPTIONS,
  normalizeInterfaceLanguage,
  toIntlLocale,
} from './languages'

describe('convertDetectedLanguage', () => {
  test('passes non-Korean values through unchanged', () => {
    expect(convertDetectedLanguage('en')).toBe('en')
    expect(convertDetectedLanguage('fr-FR')).toBe('fr-FR')
    expect(convertDetectedLanguage('ja')).toBe('ja')
  })
})

// fork: O9 — Korean (ko) and the legacy team code kr.
describe('Korean interface language', () => {
  test('maps Korean BCP-47 tags onto ko', () => {
    expect(convertDetectedLanguage('ko')).toBe('ko')
    expect(convertDetectedLanguage('ko-KR')).toBe('ko')
    expect(convertDetectedLanguage('ko_KR')).toBe('ko')
    expect(convertDetectedLanguage('kr')).toBe('ko') // legacy team code
    // ISO 639-3 codes that merely start with `ko` (Konkani, Kosraean) are not Korean
    expect(convertDetectedLanguage('kok')).toBe('kok')
    expect(convertDetectedLanguage('kos')).toBe('kos')
  })
  test('normalizes saved Korean codes, including the legacy kr', () => {
    expect(normalizeInterfaceLanguage('ko')).toBe('ko')
    expect(normalizeInterfaceLanguage('ko-KR')).toBe('ko')
    expect(normalizeInterfaceLanguage('kr')).toBe('ko')
    expect(normalizeInterfaceLanguage('pt-BR')).toBe('en')
    expect(normalizeInterfaceLanguage('kok')).toBe('en')
  })
  test('normalizes a saved user language before it reaches i18next (login)', () => {
    // use-auth-redirect passes the saved value through normalizeInterfaceLanguage
    expect(normalizeInterfaceLanguage('kr')).toBe('ko')
    expect(normalizeInterfaceLanguage('zh-CN')).toBe('en')
    expect(normalizeInterfaceLanguage('zh-TW')).toBe('en')
    expect(normalizeInterfaceLanguage(undefined)).toBe('en')
  })
  test('lists Korean as an interface language', () => {
    expect(INTERFACE_LANGUAGE_OPTIONS.map((o) => o.code)).toContain('ko')
  })
  test('yields a valid Intl locale for ko', () => {
    expect(toIntlLocale('ko')).toBe('ko')
    expect(() => new Intl.NumberFormat(toIntlLocale('ko'))).not.toThrow()
  })
})

// fork: O9 — the fork keeps only English and Korean (team 66d421d13).
describe('English and Korean only', () => {
  test('offers only English and Korean', () => {
    expect(INTERFACE_LANGUAGE_OPTIONS.map((o) => o.code)).toEqual(['en', 'ko'])
  })
  test('falls back to English for removed languages', () => {
    for (const saved of ['zhCN', 'zhTW', 'zh-CN', 'ja', 'fr', 'ru', 'vi']) {
      expect(normalizeInterfaceLanguage(saved)).toBe('en')
    }
    expect(normalizeInterfaceLanguage('kr')).toBe('ko')
  })
  test('maps only Korean browser tags', () => {
    expect(convertDetectedLanguage('ko-KR')).toBe('ko')
    expect(convertDetectedLanguage('zh-CN')).toBe('zh-CN') // not supported → i18next falls back to en
    expect(convertDetectedLanguage('zhTW')).toBe('zhTW')
  })
  test('loads only the English and Korean resources', () => {
    expect(Object.keys(resources).sort()).toEqual(['en', 'ko'])
  })
  test('yields en for every Intl locale other than Korean', () => {
    expect(toIntlLocale('en')).toBe('en')
    expect(toIntlLocale('zhCN')).toBe('en')
    expect(toIntlLocale('ja')).toBe('en')
    expect(toIntlLocale('ko-KR')).toBe('ko')
  })
})
