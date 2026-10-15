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
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import ko from '@/i18n/locales/ko.json' // fork: O9

import { importOrgUsers } from '../api'
import { OrgUsersImportDialog } from '../components/org-users-import-dialog'
import type { OrgImportResult } from '../types'

vi.mock('../api', () => ({ importOrgUsers: vi.fn() }))

const baseResult: OrgImportResult = {
  assigned: 0,
  created: 0,
  updated: 0,
  updated_usernames: [],
  removed: 0,
  removed_usernames: [],
  errors: [],
}

async function uploadAndReadResult(result: OrgImportResult) {
  vi.mocked(importOrgUsers).mockResolvedValue(result)
  render(
    <OrgUsersImportDialog
      open
      onOpenChange={() => undefined}
      onSuccess={() => undefined}
    />
  )
  await userEvent.upload(
    screen.getByLabelText(i18next.t('Select .xlsx file')),
    new File(['x'], 'users.xlsx')
  )
  await userEvent.click(
    screen.getByRole('button', { name: i18next.t('Upload') })
  )
  await screen.findByText(`${i18next.t('Errors')}:`)
}

function errorItems() {
  return screen.getAllByRole('listitem').map((item) => item.textContent)
}

beforeEach(() => {
  i18next.addResourceBundle('en', 'translation', en.translation)
  i18next.addResourceBundle('ko', 'translation', ko.translation)
  vi.mocked(importOrgUsers).mockReset()
})

afterEach(async () => {
  await act(() => i18next.changeLanguage('en'))
})

describe('organization import row errors', () => {
  test('renders translated row errors and falls back to plain errors', async () => {
    await uploadAndReadResult({
      ...baseResult,
      errors: [
        'line 2: username is empty',
        'line 3 (bad_role): invalid organization_role "x" (only member or admin can be imported)',
      ],
      error_details: [
        { line: 2, message_key: 'username is empty' },
        {
          line: 3,
          username: 'bad_role',
          message_key:
            'invalid organization_role {{role}} (only member or admin can be imported)',
          params: { role: '"x"' },
        },
        {
          line: 0,
          username: 'gone',
          message_key: 'failed to remove: {{error}}',
          params: { error: 'boom' },
        },
      ],
    })

    expect(errorItems()).toEqual([
      'Line 2: username is empty',
      'Line 3 (bad_role): invalid organization_role "x" (only member or admin can be imported)',
      'Remove members not in file (gone): failed to remove: boom',
    ])
  })

  test('renders the Korean sentence and translates a nested error key', async () => {
    await act(() => i18next.changeLanguage('ko'))
    await uploadAndReadResult({
      ...baseResult,
      errors: ['line 5 (member_21): invalid quota "9": quota exceeds ...'],
      error_details: [
        {
          line: 5,
          username: 'member_21',
          message_key: 'invalid quota {{quota}}: {{error}}',
          params: {
            quota: '"9"',
            error: 'quota exceeds the wallet limit 100',
            error_key: 'quota exceeds the wallet limit {{limit}}',
            error_params: { limit: 100 },
          },
        },
      ],
    })

    expect(errorItems()).toEqual([
      '5행 (member_21): 쿼터 "9" 값이 올바르지 않습니다: 쿼터가 지갑 한도 100을(를) 초과합니다',
    ])
  })

  test('falls back to the plain errors of an older response', async () => {
    await uploadAndReadResult({
      ...baseResult,
      errors: ['line 2: username is empty', 'line 2: username is empty'],
    })

    // Two identical rows both render.
    expect(errorItems()).toEqual([
      'line 2: username is empty',
      'line 2: username is empty',
    ])
  })

  test('keeps identical structured rows apart', async () => {
    await uploadAndReadResult({
      ...baseResult,
      errors: ['x', 'x'],
      error_details: [
        { line: 2, message_key: 'username is empty' },
        { line: 2, message_key: 'username is empty' },
      ],
    })

    expect(errorItems()).toEqual([
      'Line 2: username is empty',
      'Line 2: username is empty',
    ])
  })
})
