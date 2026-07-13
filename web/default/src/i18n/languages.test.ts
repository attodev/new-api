import { describe, expect, test } from 'bun:test'
import {
  INTERFACE_LANGUAGE_OPTIONS,
  normalizeInterfaceLanguage,
} from './languages'

describe('interface languages', () => {
  test('exposes only English and Korean in the UI language list', () => {
    expect(INTERFACE_LANGUAGE_OPTIONS.map((language) => language.code)).toEqual([
      'en',
      'kr',
    ])
  })

  test('normalizes Korean browser language variants to kr', () => {
    expect(normalizeInterfaceLanguage('ko')).toBe('kr')
    expect(normalizeInterfaceLanguage('ko-KR')).toBe('kr')
    expect(normalizeInterfaceLanguage('ko_KR')).toBe('kr')
    expect(normalizeInterfaceLanguage('kr')).toBe('kr')
  })

  test('normalizes supported English browser language variants', () => {
    expect(normalizeInterfaceLanguage('en-US')).toBe('en')
  })

  test('falls back unavailable languages to English', () => {
    expect(normalizeInterfaceLanguage('zh-CN')).toBe('en')
    expect(normalizeInterfaceLanguage('fr-CA')).toBe('en')
    expect(normalizeInterfaceLanguage('ja-JP')).toBe('en')
    expect(normalizeInterfaceLanguage('ru-RU')).toBe('en')
    expect(normalizeInterfaceLanguage('vi-VN')).toBe('en')
    expect(normalizeInterfaceLanguage('unknown')).toBe('en')
    expect(normalizeInterfaceLanguage('pt-BR')).toBe('en')
  })
})
