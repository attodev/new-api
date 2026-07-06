import { describe, expect, test } from 'bun:test'
import {
  INTERFACE_LANGUAGE_OPTIONS,
  normalizeInterfaceLanguage,
} from './languages'

describe('interface languages', () => {
  test('exposes all supported interface languages in the UI language list', () => {
    expect(INTERFACE_LANGUAGE_OPTIONS.map((language) => language.code)).toEqual([
      'en',
      'zh',
      'fr',
      'ja',
      'kr',
      'ru',
      'vi',
    ])
  })

  test('normalizes Korean browser language variants to kr', () => {
    expect(normalizeInterfaceLanguage('ko')).toBe('kr')
    expect(normalizeInterfaceLanguage('ko-KR')).toBe('kr')
    expect(normalizeInterfaceLanguage('ko_KR')).toBe('kr')
    expect(normalizeInterfaceLanguage('kr')).toBe('kr')
  })

  test('normalizes supported browser language variants', () => {
    expect(normalizeInterfaceLanguage('zh-CN')).toBe('zh')
    expect(normalizeInterfaceLanguage('fr-CA')).toBe('fr')
    expect(normalizeInterfaceLanguage('ja-JP')).toBe('ja')
    expect(normalizeInterfaceLanguage('ru-RU')).toBe('ru')
    expect(normalizeInterfaceLanguage('vi-VN')).toBe('vi')
  })

  test('falls back unknown languages to English', () => {
    expect(normalizeInterfaceLanguage('unknown')).toBe('en')
    expect(normalizeInterfaceLanguage('pt-BR')).toBe('en')
  })
})
