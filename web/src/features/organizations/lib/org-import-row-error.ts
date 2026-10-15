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
import type { TFunction } from 'i18next'

import type { OrgImportResult, OrgImportRowError } from '../types'

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object'
}

/**
 * The translated sentence of one rejected import row: "Line 3 (user): text".
 * A failure of the remove-absent step (line 0) leads with that option's label.
 * `message_key` and a nested `error_key` are keys of the locale files; params
 * are display-ready values, so they are inserted unescaped (React escapes).
 */
export function formatOrgImportRowError(
  t: TFunction,
  row: OrgImportRowError
): string {
  const options = { interpolation: { escapeValue: false } }
  const params: Record<string, unknown> = { ...row.params }
  if (typeof params.error_key === 'string' && params.error_key) {
    params.error = t(params.error_key, {
      ...(isRecord(params.error_params) ? params.error_params : {}),
      ...options,
    })
  }
  const label =
    row.line > 0
      ? t('Line {{line}}', { line: row.line, ...options })
      : t('Remove members not in file')
  const who = row.username ? ` (${row.username})` : ''
  return `${label}${who}: ${t(row.message_key, { ...params, ...options })}`
}

/**
 * The rows of an import result to list, translated. An older server only sends
 * the English `errors`. `key` is unique even when two rows read the same.
 */
export function listOrgImportErrors(
  t: TFunction,
  result: Pick<OrgImportResult, 'errors' | 'error_details'>
): { key: string; message: string }[] {
  const messages = result.error_details
    ? result.error_details.map((row) => formatOrgImportRowError(t, row))
    : result.errors
  const seen = new Map<string, number>()
  return messages.map((message) => {
    const count = (seen.get(message) ?? 0) + 1
    seen.set(message, count)
    return { key: `${count}:${message}`, message }
  })
}
