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
import { act, cleanup, render, screen } from '@testing-library/react'
import i18n from 'i18next'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'

import ko from '@/i18n/locales/ko.json'

import { QuickstartPdfLink } from '../quickstart-pdf-link-fork'

const KO_PDF = '/AlRouter%20Quick%20Start%20Guide_v1.0.pdf'
const EN_PDF = '/AlRouter%20Quick%20Start%20Guide_EN_v1.0.pdf'

beforeAll(() => {
  i18n.addResourceBundle('ko', 'translation', ko.translation, true, true)
})

afterEach(async () => {
  cleanup()
  await act(async () => {
    await i18n.changeLanguage('en')
  })
})

async function renderIn(language: string) {
  await act(async () => {
    await i18n.changeLanguage(language)
  })
  render(<QuickstartPdfLink />)
}

describe('quick start guide PDF link', () => {
  it('links the Korean guide for ko', async () => {
    await renderIn('ko')
    const link = screen.getByRole('link', { name: '빠른 시작 가이드(PDF)' })
    expect(link).toHaveAttribute('href', KO_PDF)
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('links the English guide for en and en-US', async () => {
    await renderIn('en')
    expect(
      screen.getByRole('link', { name: 'Quick Start Guide(PDF)' })
    ).toHaveAttribute('href', EN_PDF)
    cleanup()
    await renderIn('en-US')
    expect(
      screen.getByRole('link', { name: 'Quick Start Guide(PDF)' })
    ).toHaveAttribute('href', EN_PDF)
  })

  it('falls back to the Korean guide for other languages', async () => {
    await renderIn('ja')
    expect(screen.getByRole('link')).toHaveAttribute('href', KO_PDF)
  })
})
