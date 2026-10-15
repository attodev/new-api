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

// fork: O11 (team 3757c8244) — the dashboard SPA shell loads the chatbot widget.
const indexHtml = readFileSync(
  path.resolve(import.meta.dirname, '../../../index.html'),
  'utf8'
)

describe('chatbot widget in the SPA shell', () => {
  it('loads the widget script from the chatbot host after #root in the body', () => {
    const head = indexHtml.slice(
      indexHtml.indexOf('id="root"'),
      indexHtml.indexOf('</body>')
    )
    const script = head.match(
      /<script\s+src="https:\/\/chatbot\.alrouter\.ai\/widget\.js"\s+data-api-base="https:\/\/chatbot\.alrouter\.ai"\s*><\/script>/
    )
    expect(script).not.toBeNull()
  })

  it('loads the widget exactly once', () => {
    expect(indexHtml.match(/chatbot\.alrouter\.ai\/widget\.js/g)).toHaveLength(
      1
    )
  })
})
