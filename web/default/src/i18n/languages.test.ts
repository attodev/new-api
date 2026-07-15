import { describe, expect, test } from 'bun:test'
import {
  INTERFACE_LANGUAGE_OPTIONS,
  normalizeInterfaceLanguage,
} from './languages'

describe('interface languages', () => {
  test('exposes Korean and English in the UI language list', () => {
    expect(INTERFACE_LANGUAGE_OPTIONS.map((language) => language.code)).toEqual(
      ['en', 'kr']
    )
  })

  test('normalizes Korean browser language variants to kr', () => {
    expect(normalizeInterfaceLanguage('ko')).toBe('kr')
    expect(normalizeInterfaceLanguage('ko-KR')).toBe('kr')
    expect(normalizeInterfaceLanguage('ko_KR')).toBe('kr')
    expect(normalizeInterfaceLanguage('kr')).toBe('kr')
  })

  test('falls back unknown languages to English', () => {
    expect(normalizeInterfaceLanguage('unknown')).toBe('en')
    expect(normalizeInterfaceLanguage('pt-BR')).toBe('en')
    expect(normalizeInterfaceLanguage('zh-CN')).toBe('en')
  })
})
