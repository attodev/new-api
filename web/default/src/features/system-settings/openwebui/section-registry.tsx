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
import type { OpenWebUISettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { OpenWebUIOAuth2Section } from './oauth2-section'

const OPENWEBUI_SECTIONS = [
  {
    id: 'oauth2',
    titleKey: 'OpenWebUI Integration',
    build: (settings: OpenWebUISettings) => (
      <OpenWebUIOAuth2Section
        defaultValues={{
          'oauth2.enabled': settings['oauth2.enabled'],
          'oauth2.client_id': settings['oauth2.client_id'],
          'oauth2.client_secret': settings['oauth2.client_secret'],
          'oauth2.redirect_uri': settings['oauth2.redirect_uri'],
        }}
      />
    ),
  },
] as const

export type OpenWebUISectionId = (typeof OPENWEBUI_SECTIONS)[number]['id']

const openWebUIRegistry = createSectionRegistry<
  OpenWebUISectionId,
  OpenWebUISettings
>({
  sections: OPENWEBUI_SECTIONS,
  defaultSection: 'oauth2',
  basePath: '/system-settings/openwebui',
  urlStyle: 'path',
})

export const OPENWEBUI_SECTION_IDS = openWebUIRegistry.sectionIds
export const OPENWEBUI_DEFAULT_SECTION = openWebUIRegistry.defaultSection
export const getOpenWebUISectionNavItems = openWebUIRegistry.getSectionNavItems
export const getOpenWebUISectionContent = openWebUIRegistry.getSectionContent
export const getOpenWebUISectionMeta = openWebUIRegistry.getSectionMeta
