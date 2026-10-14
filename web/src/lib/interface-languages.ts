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
import { INTERFACE_LANGUAGE_OPTIONS } from '@/i18n/languages'
import { useSystemConfigStore } from '@/stores/system-config-store'

// fork: O8 — the admin option InterfaceLanguages limits which languages the
// language menu offers. Every locale stays loaded.

/**
 * Keep the options whose code is allowed, plus the user's current language so
 * a saved language that is no longer offered stays selectable. An empty or
 * missing allow list means every option.
 */
export function filterInterfaceLanguages<T extends { code: string }>(
  options: readonly T[],
  allowed: readonly string[] | undefined,
  current: string
): T[] {
  if (!allowed || allowed.length === 0) return [...options]
  return options.filter(
    (option) => allowed.includes(option.code) || option.code === current
  )
}

/** Read `interface_languages` from `/api/status`, keeping only strings. */
export function toInterfaceLanguageList(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === 'string')
}

/**
 * Read the InterfaceLanguages option value (comma-separated codes) into the
 * interface language codes, in the admin's order. Matching ignores case and
 * accepts the legacy `kr`; unknown codes and duplicates are dropped.
 */
export function parseInterfaceLanguageOption(value: string): string[] {
  const codes: string[] = []
  for (const part of value.split(',')) {
    const lower = part.trim().toLowerCase()
    const key = lower === 'kr' ? 'ko' : lower
    const match = INTERFACE_LANGUAGE_OPTIONS.find(
      (option) => option.code.toLowerCase() === key
    )
    if (match && !codes.includes(match.code)) codes.push(match.code)
  }
  return codes
}

/** The language menu options for the current user (see the file comment). */
export function useInterfaceLanguageOptions(current: string) {
  const allowed = useSystemConfigStore((s) => s.config.interfaceLanguages)
  return filterInterfaceLanguages(INTERFACE_LANGUAGE_OPTIONS, allowed, current)
}
