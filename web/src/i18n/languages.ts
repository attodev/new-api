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
export const INTERFACE_LANGUAGE_OPTIONS = [
  { code: 'en', label: 'English' },
  { code: 'ko', label: '한국어' }, // fork: O9
] as const

export type InterfaceLanguageCode =
  (typeof INTERFACE_LANGUAGE_OPTIONS)[number]['code']

// fork: O9 — the fork keeps only English and Korean (team 66d421d13). `kr` is
// the legacy team code; anything else, including the removed upstream codes // fork: O9
// (zhCN, zhTW, fr, ru, ja, vi), is English. // fork: O9
const KOREAN_TAG = /^(kr|ko([-_].*)?)$/i // fork: O9
const isKorean = (value: string) => KOREAN_TAG.test(value.trim()) // fork: O9

export function normalizeInterfaceLanguage(value?: string | null): string {
  return value && isKorean(value) ? 'ko' : 'en' // fork: O9
}

/**
 * Map a browser-detected locale onto the interface language codes this project
 * uses with i18next. // fork: O9
 *
 * Korean tags (`ko-KR`, `ko_KR`, the legacy `kr`) become `ko`. Every other // fork: O9
 * value is returned unchanged; i18next's `supportedLngs` then falls back to // fork: O9
 * English. // fork: O9
 */
export function convertDetectedLanguage(value: string): string {
  return isKorean(value) ? 'ko' : value // fork: O9
}

/**
 * Convert an interface language code (the values i18next uses) into a valid // fork: O9
 * BCP-47 locale tag that the `Intl.*` APIs accept. // fork: O9
 *
 * Any locale derived from `i18n.language` / `i18n.resolvedLanguage` MUST be // fork: O9
 * run through this before it reaches an `Intl` constructor. Korean yields // fork: O9
 * `ko`, everything else `en`; an empty value falls back to `undefined`, which // fork: O9
 * makes `Intl` use the runtime default locale. // fork: O9
 */
export function toIntlLocale(value?: string | null): string | undefined {
  if (!value) return undefined
  return normalizeInterfaceLanguage(value) // fork: O9
}
