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
// Types for scripts/ko-locale.mjs (fork: O9), imported by the Korean locale
// merge gate src/i18n/__tests__/ko-coverage.test.ts.

export type LocaleIssue = [key: string, issues: string[]]

export function validateEntry(
  en: string,
  ko: unknown,
  options: { skip: boolean; fragment?: boolean }
): string[]

export function findLocaleIssues(
  en: Record<string, unknown>,
  ko: Record<string, unknown>,
  skip: Set<string>,
  keys?: string[]
): LocaleIssue[]
