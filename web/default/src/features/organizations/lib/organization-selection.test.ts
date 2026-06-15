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
import assert from 'node:assert/strict'
import { afterEach, describe, test } from 'node:test'
import {
  getLastSelectedOrganizationId,
  resolveAvailableOrganizationId,
  saveLastSelectedOrganizationId,
} from './organization-selection'

function installWindowWithStorage(storage: Storage) {
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: {
      localStorage: storage,
    },
  })
}

function createMemoryStorage(): Storage {
  const data = new Map<string, string>()
  return {
    get length() {
      return data.size
    },
    clear() {
      data.clear()
    },
    getItem(key: string) {
      return data.get(key) ?? null
    },
    key(index: number) {
      return Array.from(data.keys())[index] ?? null
    },
    removeItem(key: string) {
      data.delete(key)
    },
    setItem(key: string, value: string) {
      data.set(key, value)
    },
  }
}

afterEach(() => {
  Reflect.deleteProperty(globalThis, 'window')
})

describe('organization selection persistence', () => {
  test('saves, reads, and clears the last selected organization id', () => {
    installWindowWithStorage(createMemoryStorage())

    saveLastSelectedOrganizationId('42')
    assert.equal(getLastSelectedOrganizationId(), '42')

    saveLastSelectedOrganizationId('')
    assert.equal(getLastSelectedOrganizationId(), '')
  })

  test('falls back to the first available organization when saved id is unavailable', () => {
    assert.equal(
      resolveAvailableOrganizationId('2', [{ id: 1 }, { id: 2 }]),
      '2'
    )
    assert.equal(
      resolveAvailableOrganizationId('9', [{ id: 1 }, { id: 2 }]),
      '1'
    )
    assert.equal(resolveAvailableOrganizationId('', []), '')
  })

  test('returns an empty id when storage is unavailable', () => {
    assert.equal(getLastSelectedOrganizationId(), '')
    saveLastSelectedOrganizationId('10')
    assert.equal(getLastSelectedOrganizationId(), '')
  })
})
