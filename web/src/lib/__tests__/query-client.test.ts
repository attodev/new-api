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
import type { QueryClient } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { createAppQueryClient } from '../query-client'

const httpError = (status: number) => ({ response: { status } })

function retryOf(client: QueryClient) {
  const retry = client.getDefaultOptions().queries?.retry
  if (typeof retry !== 'function') throw new Error('retry must be a function')
  return retry
}

describe('createAppQueryClient retry policy', () => {
  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('does not retry auth, permission or rate-limit failures', () => {
    vi.stubEnv('DEV', false)
    const retry = retryOf(createAppQueryClient())
    expect(retry(1, httpError(401) as never)).toBe(false)
    expect(retry(1, httpError(403) as never)).toBe(false)
    expect(retry(1, httpError(429) as never)).toBe(false)
  })

  it('still retries transient server failures, up to the cap', () => {
    vi.stubEnv('DEV', false)
    const retry = retryOf(createAppQueryClient())
    expect(retry(1, httpError(500) as never)).toBe(true)
    expect(retry(4, httpError(500) as never)).toBe(false)
  })
})
