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

import {
  convertDetectedLanguage,
  INTERFACE_LANGUAGE_OPTIONS,
  normalizeInterfaceLanguage,
  toIntlLocale,
} from './languages'

describe('convertDetectedLanguage', () => {
  test('maps browser BCP-47 Chinese tags onto interface codes', () => {
    expect(convertDetectedLanguage('zh-TW')).toBe('zhTW')
    expect(convertDetectedLanguage('zh-HK')).toBe('zhTW')
    expect(convertDetectedLanguage('zh-MO')).toBe('zhTW')
    expect(convertDetectedLanguage('zh-Hant-TW')).toBe('zhTW')
    expect(convertDetectedLanguage('zh')).toBe('zhCN')
    expect(convertDetectedLanguage('zh-CN')).toBe('zhCN')
    expect(convertDetectedLanguage('zh-Hans')).toBe('zhCN')
  })

  test('keeps already-normalized interface codes stable (localStorage round-trip)', () => {
    // i18next caches `zhTW`/`zhCN` (the supportedLngs codes) to localStorage,
    // and the detector runs this converter on the cached value at every page
    // load — if `zhTW` does not survive the round-trip, a user who picked
    // Traditional Chinese is flipped to Simplified on the next load and the
    // cache is overwritten, making the flip permanent.
    expect(convertDetectedLanguage('zhTW')).toBe('zhTW')
    expect(convertDetectedLanguage('zhCN')).toBe('zhCN')
  })

  test('passes non-Chinese values through unchanged', () => {
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
    expect(normalizeInterfaceLanguage('zh-CN')).toBe('zhCN')
    expect(normalizeInterfaceLanguage('zh-TW')).toBe('zhTW')
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
