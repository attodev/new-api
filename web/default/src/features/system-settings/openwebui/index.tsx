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
import { SettingsPage } from '../components/settings-page'
import type { OpenWebUISettings } from '../types'
import {
  OPENWEBUI_DEFAULT_SECTION,
  getOpenWebUISectionContent,
  getOpenWebUISectionMeta,
} from './section-registry.tsx'

const defaultOpenWebUISettings: OpenWebUISettings = {
  'oauth2.enabled': false,
  'oauth2.client_id': '',
  'oauth2.client_secret': '',
  'oauth2.redirect_uri': '',
}

export function OpenWebUISettingsPage() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/openwebui/$section'
      defaultSettings={defaultOpenWebUISettings}
      defaultSection={OPENWEBUI_DEFAULT_SECTION}
      getSectionContent={getOpenWebUISectionContent}
      getSectionMeta={getOpenWebUISectionMeta}
    />
  )
}
