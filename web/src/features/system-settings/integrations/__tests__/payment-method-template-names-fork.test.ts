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
import { readFileSync } from 'node:fs'
import path from 'node:path'

import { describe, expect, it } from 'vitest'

// fork: O11 — the payment-method template defaults (team f34c341c2) must not
// ship upstream's Chinese names; admins see these verbatim in the editor.
describe('payment method template default names (fork)', () => {
  const source = readFileSync(
    path.resolve(import.meta.dirname, '../payment-methods-visual-editor.tsx'),
    'utf8'
  )

  it('uses English default names for the Epay templates', () => {
    expect(source).toContain("name: 'Alipay'")
    expect(source).toContain("name: 'WeChat Pay'")
    expect(source).toContain("name: 'Custom 1'")
  })

  it('leaves no Chinese template names behind', () => {
    expect(source).not.toMatch(/支付宝|微信|自定义/)
  })
})
