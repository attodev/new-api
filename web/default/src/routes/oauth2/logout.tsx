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
import { useEffect } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { AuthLayout } from '@/features/auth/auth-layout'
import { logoutOidc, type OidcLogoutParams } from '@/features/oidc/api'

/**
 * OIDC end_session_endpoint as a page: the same-origin API call below
 * carries the session cookie that the cross-site GET from Open WebUI did
 * not, so the new-api session really ends. The server decides where to
 * send the browser next (registered post-logout URI or /sign-in).
 */
function OidcLogoutPage() {
  const { t } = useTranslation()

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      let target = '/sign-in'
      try {
        // Raw query, not Route.useSearch(): the router JSON-parses values.
        const qs = new URLSearchParams(window.location.search)
        const params: OidcLogoutParams = {
          id_token_hint: qs.get('id_token_hint') ?? undefined,
          post_logout_redirect_uri:
            qs.get('post_logout_redirect_uri') ?? undefined,
          state: qs.get('state') ?? undefined,
        }
        const res = await logoutOidc(params)
        if (res?.data?.redirect_url) target = res.data.redirect_url
      } catch {
        /* fall back to /sign-in */
      }
      if (cancelled) return
      useAuthStore.getState().auth.reset()
      window.location.replace(target)
    })()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <AuthLayout>
      <div className='mx-auto flex w-full max-w-sm flex-col items-center gap-4 text-center'>
        <Loader2 className='text-muted-foreground size-6 animate-spin' />
        <p className='text-muted-foreground text-sm'>
          {t('Signing you out...')}
        </p>
      </div>
    </AuthLayout>
  )
}

export const Route = createFileRoute('/oauth2/logout')({
  // Permissive on purpose: the page reads window.location.search itself.
  validateSearch: (search: Record<string, unknown>) => search,
  component: OidcLogoutPage,
})
