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
import { BookOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { buttonVariants } from '@/components/ui/button'

// fork: O11 (team 9b4ee5213, a3e606bc9) — quick start guide PDF download.
// English UIs get the English guide; every other language gets the Korean one.
const QUICKSTART_PDF_KO = '/AlRouter Quick Start Guide_v1.0.pdf'
const QUICKSTART_PDF_EN = '/AlRouter Quick Start Guide_EN_v1.0.pdf'

export function QuickstartPdfLink() {
  const { t, i18n } = useTranslation()
  const isEnglish = (i18n.language ?? '').toLowerCase().startsWith('en')
  const href = encodeURI(isEnglish ? QUICKSTART_PDF_EN : QUICKSTART_PDF_KO)

  return (
    <div className='flex justify-end pt-2'>
      <a
        href={href}
        target='_blank'
        rel='noopener noreferrer'
        className={buttonVariants({ size: 'sm', variant: 'outline' })}
      >
        <BookOpen data-icon='inline-start' />
        {t('Quick Start Guide(PDF)')}
      </a>
    </div>
  )
}
