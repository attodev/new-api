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

// fork: O11 (team 3757c8244) — the dashboard SPA shell carries a placeholder
// that main_chatbot_fork.go replaces with the deferred widget script only when
// CHATBOT_WIDGET_URL is set.
const indexHtml = readFileSync(
  path.resolve(import.meta.dirname, '../../../index.html'),
  'utf8'
)

describe('chatbot widget placeholder in the SPA shell', () => {
  it('puts the placeholder on its own line after #root in the body', () => {
    const body = indexHtml.slice(
      indexHtml.indexOf('id="root"'),
      indexHtml.indexOf('</body>')
    )
    expect(body).toMatch(/\n\s*<!--chatbot-->\n/)
    expect(indexHtml.match(/<!--chatbot-->/g)).toHaveLength(1)
  })

  it('does not hard-code the chatbot host', () => {
    expect(indexHtml).not.toContain('chatbot.alrouter.ai')
    expect(indexHtml).not.toMatch(/widget\.js/)
  })
})
