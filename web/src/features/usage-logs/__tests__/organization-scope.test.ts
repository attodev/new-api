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

import { buildOrganizationLogsParams } from '../lib/utils'

describe('buildOrganizationLogsParams', () => {
  const params = { p: 1, page_size: 20, model_name: 'gpt-4o' }

  test('adds organization_id for the organization scope', () => {
    expect(buildOrganizationLogsParams(params, 'organization', 7)).toEqual({
      ...params,
      organization_id: 7,
    })
  })

  test('leaves user-scope params untouched', () => {
    const result = buildOrganizationLogsParams(params, 'user', 7)
    expect(result).toEqual(params)
    expect(result).not.toHaveProperty('organization_id')
  })

  test('omits organization_id until an organization is chosen', () => {
    expect(
      buildOrganizationLogsParams(params, 'organization', undefined)
    ).not.toHaveProperty('organization_id')
  })
})
