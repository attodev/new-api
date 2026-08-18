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
import { createFileRoute, redirect } from '@tanstack/react-router'
import { OpenWebUISettingsPage } from '@/features/system-settings/openwebui'
import {
  OPENWEBUI_DEFAULT_SECTION,
  OPENWEBUI_SECTION_IDS,
} from '@/features/system-settings/openwebui/section-registry.tsx'

export const Route = createFileRoute(
  '/_authenticated/system-settings/openwebui/$section'
)({
  beforeLoad: ({ params }) => {
    const validSections = OPENWEBUI_SECTION_IDS as unknown as string[]
    if (!validSections.includes(params.section)) {
      throw redirect({
        to: '/system-settings/openwebui/$section',
        params: { section: OPENWEBUI_DEFAULT_SECTION },
      })
    }
  },
  component: OpenWebUISettingsPage,
})
