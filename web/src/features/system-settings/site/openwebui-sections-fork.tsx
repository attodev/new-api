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
// fork: O13 — the two Open WebUI sections of Site & Branding. Only non-secret
// options are passed down; the secrets are write-only and never prefilled.
import type { SiteSettings } from '../types'
import { OpenWebUIOAuth2Section } from './openwebui-integration-section'
import { OpenWebUIRelaySection } from './openwebui-relay-section'

export const OPENWEBUI_SITE_SECTIONS = [
  {
    id: 'openwebui-integration',
    titleKey: 'OpenWebUI Integration',
    build: (settings: SiteSettings) => (
      <OpenWebUIOAuth2Section
        defaultValues={{
          'oauth2.enabled': settings['oauth2.enabled'],
          'oauth2.client_id': settings['oauth2.client_id'],
          'oauth2.redirect_uri': settings['oauth2.redirect_uri'],
          'oauth2.app_url': settings['oauth2.app_url'],
          'oauth2.post_logout_redirect_uri':
            settings['oauth2.post_logout_redirect_uri'],
          'oauth2.open_in_new_window': settings['oauth2.open_in_new_window'],
          'oauth2.replace_playground': settings['oauth2.replace_playground'],
        }}
      />
    ),
  },
  {
    id: 'openwebui-relay',
    titleKey: 'OpenWebUI Relay',
    build: (settings: SiteSettings) => (
      <OpenWebUIRelaySection
        defaultValues={{
          'openwebui.relay_enabled': settings['openwebui.relay_enabled'],
          'openwebui.jwt_issuer': settings['openwebui.jwt_issuer'],
          'openwebui.jwt_leeway_seconds':
            settings['openwebui.jwt_leeway_seconds'],
          'openwebui.allow_unsigned_requests':
            settings['openwebui.allow_unsigned_requests'],
        }}
      />
    ),
  },
] as const
